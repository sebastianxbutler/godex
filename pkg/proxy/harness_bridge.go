// Package proxy: harness_bridge translates between harness.Event and the
// SSE/JSON wire formats consumed by proxy clients.
package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"godex/pkg/harness"
	"godex/pkg/protocol"
	"godex/pkg/router"
)

// harnessResponsesStream handles a streaming /v1/responses request via harness.
// It translates harness.Event back to the Codex-format SSE that clients expect.
func (s *Server) harnessResponsesStream(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	h harness.Harness,
	turn *harness.Turn,
	model string,
	key *KeyRecord,
	start time.Time,
	auditReq json.RawMessage,
	sessionKey string,
	requestID string,
) error {
	responseID := newResponseID("resp")
	// itemIndex tracks output item indices for SSE
	itemIndex := 0
	// Track tool calls for cache
	toolCalls := map[string]ToolCall{}
	var outputText string
	var usage *protocol.Usage

	// Emit response.created
	created := map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id":     responseID,
			"object": "response",
			"status": "in_progress",
			"model":  model,
		},
	}
	emitSSE := func(phase string, payload any) error {
		s.tracePayload(requestID, "proxy_openclaw", "out", "/v1/responses", phase, payload)
		return writeSSE(w, flusher, payload)
	}
	if err := emitSSE("sse.response.created", created); err != nil {
		return err
	}

	// Track whether we've started a text output item
	textItemStarted := false

	err := h.StreamTurn(ctx, turn, func(ev harness.Event) error {
		if rawEv, err := json.Marshal(ev); err == nil {
			s.tracePayload(requestID, "proxy_harness", "in", "/v1/responses", "harness.event", json.RawMessage(rawEv))
		}
		switch ev.Kind {
		case harness.EventText:
			if ev.Text == nil || ev.Text.Delta == "" {
				return nil
			}
			// Start text output item if needed
			if !textItemStarted {
				textItemStarted = true
				addedEvt := map[string]any{
					"type":         "response.output_item.added",
					"output_index": itemIndex,
					"item": map[string]any{
						"id":      fmt.Sprintf("msg_%d", itemIndex),
						"type":    "message",
						"role":    "assistant",
						"content": []any{},
					},
				}
				if err := emitSSE("sse.response.output_item.added.message", addedEvt); err != nil {
					return err
				}
				// Content part added
				partEvt := map[string]any{
					"type":          "response.content_part.added",
					"output_index":  itemIndex,
					"content_index": 0,
					"part": map[string]any{
						"type": "output_text",
						"text": "",
					},
				}
				if err := emitSSE("sse.response.content_part.added", partEvt); err != nil {
					return err
				}
			}
			outputText += ev.Text.Delta
			delta := map[string]any{
				"type":          "response.output_text.delta",
				"output_index":  itemIndex,
				"content_index": 0,
				"delta":         ev.Text.Delta,
			}
			return emitSSE("sse.response.output_text.delta", delta)

		case harness.EventToolCall:
			if ev.ToolCall == nil {
				return nil
			}
			tc := ev.ToolCall
			normalizeExecToolCall(turn, tc)
			if tc.Name == "exec" {
				log.Printf("[INFO] emitting exec tool call stream call_id=%s args=%s", tc.CallID, tc.Arguments)
			}
			// If we had a text item, close it and advance
			if textItemStarted {
				itemIndex++
				textItemStarted = false
			}
			idx := itemIndex
			toolCalls[tc.CallID] = ToolCall{Name: tc.Name, Arguments: tc.Arguments}
			itemIndex++

			// Emit output_item.added for function_call
			addedEvt := map[string]any{
				"type":         "response.output_item.added",
				"output_index": idx,
				"item": map[string]any{
					"id":      tc.CallID,
					"type":    "function_call",
					"call_id": tc.CallID,
					"name":    tc.Name,
					// Include arguments on added for clients that execute tool calls
					// immediately on output_item.added without waiting for done.
					"arguments": tc.Arguments,
				},
			}
			if err := emitSSE("sse.response.output_item.added", addedEvt); err != nil {
				return err
			}

			// Emit arguments delta
			if tc.Arguments != "" {
				argsDelta := map[string]any{
					"type":         "response.function_call_arguments.delta",
					"output_index": idx,
					"item_id":      tc.CallID,
					"delta":        tc.Arguments,
				}
				if err := emitSSE("sse.response.function_call_arguments.delta", argsDelta); err != nil {
					return err
				}
			}

			// Emit arguments done
			argsDone := map[string]any{
				"type":         "response.function_call_arguments.done",
				"output_index": idx,
				"item_id":      tc.CallID,
				"item": map[string]any{
					"id":        tc.CallID,
					"type":      "function_call",
					"call_id":   tc.CallID,
					"name":      tc.Name,
					"arguments": tc.Arguments,
				},
			}
			if err := emitSSE("sse.response.function_call_arguments.done", argsDone); err != nil {
				return err
			}

			// Emit output_item.done
			itemDone := map[string]any{
				"type":         "response.output_item.done",
				"output_index": idx,
				"item": map[string]any{
					"id":        tc.CallID,
					"type":      "function_call",
					"call_id":   tc.CallID,
					"name":      tc.Name,
					"arguments": tc.Arguments,
				},
			}
			return emitSSE("sse.response.output_item.done", itemDone)

		case harness.EventUsage:
			if ev.Usage != nil {
				usage = &protocol.Usage{
					InputTokens:  ev.Usage.InputTokens,
					OutputTokens: ev.Usage.OutputTokens,
				}
			}

		case harness.EventError:
			if ev.Error != nil {
				errEvt := map[string]any{
					"type":    "error",
					"message": ev.Error.Message,
				}
				return emitSSE("sse.error", errEvt)
			}

		case harness.EventDone:
			// Finalize text output item if open
			if textItemStarted {
				textDone := map[string]any{
					"type":          "response.output_text.done",
					"output_index":  itemIndex,
					"content_index": 0,
					"text":          outputText,
				}
				if err := emitSSE("sse.response.output_text.done", textDone); err != nil {
					return err
				}
			}

			// Emit response.completed
			completed := map[string]any{
				"type": "response.completed",
				"response": map[string]any{
					"id":     responseID,
					"object": "response",
					"status": "completed",
					"model":  model,
				},
			}
			if usage != nil {
				completed["response"].(map[string]any)["usage"] = map[string]any{
					"input_tokens":  usage.InputTokens,
					"output_tokens": usage.OutputTokens,
				}
			}
			return emitSSE("sse.response.completed", completed)

		case harness.EventThinking:
			// Thinking events don't currently have an OpenAI wire equivalent.

		case harness.EventPlanUpdate:
			// Plan updates are harness-internal and are not emitted over proxy SSE.
		}
		return nil
	})

	if err != nil {
		return err
	}

	// Cache tool calls
	s.cache.SaveToolCalls(sessionKey, toolCalls)

	// Record usage
	s.recordUsage(nil, key, http.StatusOK, usage)

	// Audit log
	if s.audit != nil {
		var toolNames []string
		for _, tc := range toolCalls {
			toolNames = append(toolNames, tc.Name)
		}
		entry := AuditEntry{
			KeyID:         key.ID,
			KeyLabel:      key.Label,
			Method:        "POST",
			Path:          "/v1/responses",
			Model:         model,
			Status:        http.StatusOK,
			ElapsedMs:     time.Since(start).Milliseconds(),
			HasToolCalls:  len(toolCalls) > 0,
			ToolCallNames: toolNames,
			OutputText:    outputText,
		}
		if usage != nil {
			entry.TokensIn = usage.InputTokens
			entry.TokensOut = usage.OutputTokens
		}
		entry.Request = auditReq
		s.audit.Log(entry)
	}

	return nil
}

func repairEmptyExecArgs(turn *harness.Turn) (string, bool) {
	cmd, ok := inferCommandFromMessages(turn.Messages)
	if !ok {
		return "", false
	}
	args := map[string]string{"command": cmd}
	raw, err := json.Marshal(args)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func normalizeExecToolCall(turn *harness.Turn, tc *harness.ToolCallEvent) {
	if tc == nil || tc.Name != "exec" {
		return
	}
	tc.Arguments = sanitizeExecArgs(tc.Arguments)
	if needsExecArgRepair(tc.Arguments) {
		if repaired, ok := repairEmptyExecArgs(turn); ok {
			log.Printf("[INFO] repaired empty/invalid exec args call_id=%s args=%s", tc.CallID, repaired)
			tc.Arguments = repaired
		} else {
			log.Printf("[WARN] unable to infer exec args for call_id=%s original=%q", tc.CallID, tc.Arguments)
		}
	}
}

func sanitizeExecArgs(args string) string {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return args
	}

	sanitized := map[string]any{}
	switch v := parsed["command"].(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			sanitized["command"] = v
		}
	}
	switch v := parsed["workdir"].(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			sanitized["workdir"] = v
		}
	}
	switch v := parsed["yieldMs"].(type) {
	case float64:
		sanitized["yieldMs"] = int(v)
	case int:
		sanitized["yieldMs"] = v
	}

	out, err := json.Marshal(sanitized)
	if err != nil {
		return args
	}
	return string(out)
}

func needsExecArgRepair(args string) bool {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" || trimmed == "{}" {
		return true
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return true
	}
	if len(parsed) == 0 {
		return true
	}
	cmdRaw, ok := parsed["command"]
	if !ok {
		return true
	}
	cmd, ok := cmdRaw.(string)
	return !ok || strings.TrimSpace(cmd) == ""
}

func inferCommandFromMessages(messages []harness.Message) (string, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		if cmd, ok := extractBacktickCommand(msg.Content); ok {
			return cmd, true
		}
		if cmd, ok := extractQuotedCommand(msg.Content); ok {
			return cmd, true
		}
		if mentionsLsCommand(msg.Content) {
			return "ls", true
		}
	}
	return "", false
}

var backtickCmdRE = regexp.MustCompile("`([^`\\n]+)`")
var quotedCmdRE = regexp.MustCompile(`"([^"\n]+)"`)

func extractBacktickCommand(s string) (string, bool) {
	matches := backtickCmdRE.FindAllStringSubmatch(s, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(matches[i][1])
		if candidate != "" {
			return candidate, true
		}
	}
	return "", false
}

func extractQuotedCommand(s string) (string, bool) {
	lower := strings.ToLower(s)
	if !strings.Contains(lower, "command") {
		return "", false
	}
	matches := quotedCmdRE.FindAllStringSubmatch(s, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(matches[i][1])
		if candidate != "" {
			return candidate, true
		}
	}
	return "", false
}

func mentionsLsCommand(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "run ls") ||
		strings.Contains(lower, "\"ls\" command") ||
		strings.Contains(lower, "try running ls")
}

// harnessResponsesNonStream handles a non-streaming /v1/responses request via harness.
func (s *Server) harnessResponsesNonStream(
	ctx context.Context,
	w http.ResponseWriter,
	h harness.Harness,
	turn *harness.Turn,
	model string,
	key *KeyRecord,
	start time.Time,
	auditReq json.RawMessage,
	sessionKey string,
	requestID string,
) {
	result, err := h.StreamAndCollect(ctx, turn)
	if err != nil {
		s.traceMessage(requestID, "proxy_harness", "in", "/v1/responses", "stream_and_collect_error", err.Error())
		writeError(w, http.StatusBadGateway, err)
		return
	}

	// Build tool calls cache
	calls := map[string]ToolCall{}
	for _, tc := range result.ToolCalls {
		local := tc
		normalizeExecToolCall(turn, &local)
		tc = local
		if tc.Name == "exec" {
			log.Printf("[INFO] emitting exec tool call nonstream call_id=%s args=%s", tc.CallID, tc.Arguments)
		}
		calls[tc.CallID] = ToolCall{Name: tc.Name, Arguments: tc.Arguments}
	}
	s.cache.SaveToolCalls(sessionKey, calls)

	// Build response
	resp := OpenAIResponsesResponse{
		ID:     newResponseID("resp"),
		Object: "response",
		Model:  model,
		Output: []OpenAIRespItem{},
	}
	if result.FinalText != "" {
		resp.Output = append(resp.Output, OpenAIRespItem{
			Type: "message",
			Role: "assistant",
			Content: []OpenAIRespContent{{
				Type: "output_text",
				Text: result.FinalText,
			}},
		})
	}
	for _, tc := range result.ToolCalls {
		local := tc
		normalizeExecToolCall(turn, &local)
		tc = local
		resp.Output = append(resp.Output, OpenAIRespItem{
			Type:      "function_call",
			Name:      tc.Name,
			CallID:    tc.CallID,
			Arguments: tc.Arguments,
		})
	}
	if rawResp, err := json.Marshal(resp); err == nil {
		s.tracePayload(requestID, "proxy_openclaw", "out", "/v1/responses", "json.response", json.RawMessage(rawResp))
	}

	writeJSON(w, http.StatusOK, resp)
	s.recordUsage(nil, key, http.StatusOK, nil)

	// Audit
	if s.audit != nil {
		var toolNames []string
		for _, tc := range result.ToolCalls {
			toolNames = append(toolNames, tc.Name)
		}
		entry := AuditEntry{
			KeyID:         key.ID,
			KeyLabel:      key.Label,
			Method:        "POST",
			Path:          "/v1/responses",
			Model:         model,
			Status:        http.StatusOK,
			ElapsedMs:     time.Since(start).Milliseconds(),
			HasToolCalls:  len(result.ToolCalls) > 0,
			ToolCallNames: toolNames,
			OutputText:    result.FinalText,
		}
		if result.Usage != nil {
			entry.TokensIn = result.Usage.InputTokens
			entry.TokensOut = result.Usage.OutputTokens
		}
		entry.Request = auditReq
		s.audit.Log(entry)
	}
}

// harnessChatStream handles a streaming /v1/chat/completions request via harness.
func (s *Server) harnessChatStream(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	h harness.Harness,
	turn *harness.Turn,
	model string,
	key *KeyRecord,
	start time.Time,
	sessionKey string,
	requestID string,
) error {
	chunkID := newResponseID("chatcmpl")
	created := time.Now().Unix()
	sentRole := false
	sawTool := false
	callInfoMap := map[string]chatCallInfo{}
	toolCalls := map[string]ToolCall{}
	var usage *protocol.Usage

	err := h.StreamTurn(ctx, turn, func(ev harness.Event) error {
		if rawEv, err := json.Marshal(ev); err == nil {
			s.tracePayload(requestID, "proxy_harness", "in", "/v1/chat/completions", "harness.event", json.RawMessage(rawEv))
		}
		switch ev.Kind {
		case harness.EventText:
			if ev.Text == nil || ev.Text.Delta == "" {
				return nil
			}
			chunk := OpenAIChatStreamChunk{
				ID:      chunkID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   model,
				Choices: []OpenAIChatDeltaChoice{{
					Index: 0,
					Delta: OpenAIChatDelta{Content: ev.Text.Delta},
				}},
			}
			if !sentRole {
				chunk.Choices[0].Delta.Role = "assistant"
				sentRole = true
			}
			s.tracePayload(requestID, "proxy_openclaw", "out", "/v1/chat/completions", "sse.chat.delta", chunk)
			return writeSSE(w, flusher, chunk)

		case harness.EventToolCall:
			if ev.ToolCall == nil {
				return nil
			}
			tc := ev.ToolCall
			normalizeExecToolCall(turn, tc)
			if tc.Name == "exec" {
				log.Printf("[INFO] emitting exec tool call chat-stream call_id=%s args=%s", tc.CallID, tc.Arguments)
			}
			sawTool = true
			info, ok := callInfoMap[tc.CallID]
			if !ok {
				info = chatCallInfo{index: len(callInfoMap), id: tc.CallID, name: tc.Name}
				callInfoMap[tc.CallID] = info
			}
			toolCalls[tc.CallID] = ToolCall{Name: tc.Name, Arguments: tc.Arguments}

			// Emit tool call start
			startChunk := OpenAIChatStreamChunk{
				ID:      chunkID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   model,
				Choices: []OpenAIChatDeltaChoice{{
					Index: 0,
					Delta: OpenAIChatDelta{ToolCalls: []OpenAIChatToolCallDelta{{
						Index: info.index,
						ID:    info.id,
						Type:  "function",
						Function: &OpenAIChatToolFuncDelta{
							Name: info.name,
						},
					}}},
				}},
			}
			if err := writeSSE(w, flusher, startChunk); err != nil {
				return err
			}
			s.tracePayload(requestID, "proxy_openclaw", "out", "/v1/chat/completions", "sse.chat.tool_start", startChunk)

			// Emit arguments
			if tc.Arguments != "" {
				argsChunk := OpenAIChatStreamChunk{
					ID:      chunkID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []OpenAIChatDeltaChoice{{
						Index: 0,
						Delta: OpenAIChatDelta{ToolCalls: []OpenAIChatToolCallDelta{{
							Index: info.index,
							ID:    info.id,
							Type:  "function",
							Function: &OpenAIChatToolFuncDelta{
								Arguments: tc.Arguments,
							},
						}}},
					}},
				}
				s.tracePayload(requestID, "proxy_openclaw", "out", "/v1/chat/completions", "sse.chat.tool_args", argsChunk)
				return writeSSE(w, flusher, argsChunk)
			}
			return nil

		case harness.EventUsage:
			if ev.Usage != nil {
				usage = &protocol.Usage{
					InputTokens:  ev.Usage.InputTokens,
					OutputTokens: ev.Usage.OutputTokens,
				}
			}

		case harness.EventDone:
			// Will send final chunk after StreamTurn returns
		}
		return nil
	})

	if err != nil {
		return err
	}

	s.cache.SaveToolCalls(sessionKey, toolCalls)

	finish := "stop"
	if sawTool {
		finish = "tool_calls"
	}
	finalChunk := OpenAIChatStreamChunk{
		ID:      chunkID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []OpenAIChatDeltaChoice{{
			Index:        0,
			Delta:        OpenAIChatDelta{},
			FinishReason: &finish,
		}},
	}
	_ = writeSSE(w, flusher, finalChunk)
	s.tracePayload(requestID, "proxy_openclaw", "out", "/v1/chat/completions", "sse.chat.final", finalChunk)
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()

	s.recordUsage(nil, key, http.StatusOK, usage)
	harnessName := h.Name()
	s.recordMetric(harnessName, model, start, "ok", "", usage)

	return nil
}

// buildTurnFromResponses converts a proxy ResponsesRequest into a harness.Turn.
func buildTurnFromResponses(model, instructions string, input []protocol.ResponseInputItem, tools []protocol.ToolSpec, reasoning any) *harness.Turn {
	turn := &harness.Turn{
		Model:        model,
		Instructions: instructions,
	}

	// Convert input items to messages
	for _, item := range input {
		switch item.Type {
		case "message":
			text := ""
			for _, part := range item.Content {
				text += part.Text
			}
			turn.Messages = append(turn.Messages, harness.Message{
				Role:    item.Role,
				Content: text,
			})
		case "function_call":
			turn.Messages = append(turn.Messages, harness.Message{
				Role:    "assistant",
				Content: item.Arguments,
				Name:    item.Name,
				ToolID:  item.CallID,
			})
		case "function_call_output":
			turn.Messages = append(turn.Messages, harness.Message{
				Role:    "tool",
				Content: item.Output,
				ToolID:  item.CallID,
			})
		}
	}

	// Convert tools
	for _, t := range tools {
		if t.Type != "function" {
			continue
		}
		var params map[string]any
		if t.Parameters != nil {
			_ = json.Unmarshal(t.Parameters, &params)
		}
		turn.Tools = append(turn.Tools, harness.ToolSpec{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		})
	}

	return turn
}

// buildTurnFromChat converts a chat completions request into a harness.Turn.
func buildTurnFromChat(model, instructions string, input []protocol.ResponseInputItem, tools []protocol.ToolSpec) *harness.Turn {
	return buildTurnFromResponses(model, instructions, input, tools, nil)
}

// harnessForModel returns the harness for a model from the harness router.
// Returns nil if no harness router is configured or no match found.
func (s *Server) harnessForModel(model string) harness.Harness {
	if s.harnessRouter == nil {
		return nil
	}
	expanded := s.harnessRouter.ExpandAlias(model)
	return s.harnessRouter.HarnessFor(expanded)
}

// harnessModelInfo is analogous to backend.ModelInfo for the harness system.
type harnessModelInfo struct {
	ID          string
	DisplayName string
}

// getCachedHarnessModels returns models from harness router with caching.
func (s *Server) getCachedHarnessModels(ctx context.Context) []harnessModelInfo {
	if s.harnessRouter == nil {
		return nil
	}
	// Use the same cache mechanism; harness models integrate with backend models
	models := s.harnessRouter.AllModels(ctx)
	result := make([]harnessModelInfo, len(models))
	for i, m := range models {
		result[i] = harnessModelInfo{ID: m.ID, DisplayName: m.Name}
	}
	return result
}

// Ensure the Server has a harnessRouter field. This is set in the updated Run() or externally.
// We add it via the SetHarnessRouter method.

// SetHarnessRouter sets the harness router on the server. Used by main.go wiring.
func (s *Server) SetHarnessRouter(r *router.Router) {
	s.harnessRouter = r
}
