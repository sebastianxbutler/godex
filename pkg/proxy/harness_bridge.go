// Package proxy: harness_bridge translates between harness.Event and the
// SSE/JSON wire formats consumed by proxy clients. This preserves backward
// compatibility with existing API consumers while routing through harnesses.
package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
) error {
	responseID := newResponseID("resp")
	// itemIndex tracks output item indices for SSE
	itemIndex := 0
	// callIndex maps callID to item index
	callIndex := map[string]int{}
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
	if err := writeSSE(w, flusher, created); err != nil {
		return err
	}

	// Track whether we've started a text output item
	textItemStarted := false

	err := h.StreamTurn(ctx, turn, func(ev harness.Event) error {
		switch ev.Kind {
		case harness.EventText:
			if ev.Text == nil || ev.Text.Delta == "" {
				return nil
			}
			// Start text output item if needed
			if !textItemStarted {
				textItemStarted = true
				addedEvt := map[string]any{
					"type":       "response.output_item.added",
					"output_index": itemIndex,
					"item": map[string]any{
						"id":   fmt.Sprintf("msg_%d", itemIndex),
						"type": "message",
						"role": "assistant",
						"content": []any{},
					},
				}
				if err := writeSSE(w, flusher, addedEvt); err != nil {
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
				if err := writeSSE(w, flusher, partEvt); err != nil {
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
			return writeSSE(w, flusher, delta)

		case harness.EventToolCall:
			if ev.ToolCall == nil {
				return nil
			}
			tc := ev.ToolCall
			// If we had a text item, close it and advance
			if textItemStarted {
				itemIndex++
				textItemStarted = false
			}
			idx := itemIndex
			callIndex[tc.CallID] = idx
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
				},
			}
			if err := writeSSE(w, flusher, addedEvt); err != nil {
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
				if err := writeSSE(w, flusher, argsDelta); err != nil {
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
			if err := writeSSE(w, flusher, argsDone); err != nil {
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
			return writeSSE(w, flusher, itemDone)

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
				return writeSSE(w, flusher, errEvt)
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
				if err := writeSSE(w, flusher, textDone); err != nil {
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
			return writeSSE(w, flusher, completed)

		case harness.EventThinking:
			// Thinking events don't have a direct SSE equivalent in the Codex format.
			// We could emit them as a custom event type, but for backward compat we skip.

		case harness.EventPlanUpdate:
			// Plan updates are Codex-specific. We could re-emit them as tool calls
			// but the harness already handles this internally. Skip for SSE.
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
) {
	result, err := h.StreamAndCollect(ctx, turn)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	// Build tool calls cache
	calls := map[string]ToolCall{}
	for _, tc := range result.ToolCalls {
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
		resp.Output = append(resp.Output, OpenAIRespItem{
			Type:      "function_call",
			Name:      tc.Name,
			CallID:    tc.CallID,
			Arguments: tc.Arguments,
		})
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
) error {
	chunkID := newResponseID("chatcmpl")
	created := time.Now().Unix()
	sentRole := false
	sawTool := false
	callInfoMap := map[string]chatCallInfo{}
	toolCalls := map[string]ToolCall{}
	var usage *protocol.Usage

	err := h.StreamTurn(ctx, turn, func(ev harness.Event) error {
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
			return writeSSE(w, flusher, chunk)

		case harness.EventToolCall:
			if ev.ToolCall == nil {
				return nil
			}
			tc := ev.ToolCall
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
