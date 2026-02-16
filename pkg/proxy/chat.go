package proxy

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"godex/pkg/backend"
	"godex/pkg/client"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

type chatCallInfo struct {
	index int
	id    string
	name  string
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req OpenAIChatRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	modelEntry, ok := s.resolveModel(req.Model)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("model %q not available", req.Model))
		return
	}
	req.Model = modelEntry.ID
	key, ok := s.requireAuthOrPayment(w, r, req.Model)
	if !ok {
		return
	}
	if ok, reason := s.allowRequest(w, r, key); !ok {
		if reason == "tokens" {
			_ = s.issuePaymentChallenge(w, r, "topup", key.ID, req.Model)
		}
		return
	}
	sessionKey := s.sessionKey(req.User, r)
	items := make([]OpenAIItem, 0, len(req.Messages))
	for _, msg := range req.Messages {
		items = append(items, OpenAIItem{Type: "message", Role: msg.Role, Content: msg.Content})
	}
	input, system, err := buildSystemAndInput(sessionKey, items, s.cache)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	instructions := mergeInstructions("", system)
	instructions = s.resolveInstructions(sessionKey, instructions)
	tools := mapChatTools(req.Tools)
	toolChoice, tools := resolveToolChoice(req.ToolChoice, tools)

	codexReq := protocol.ResponsesRequest{
		Model:             req.Model,
		Instructions:      instructions,
		Input:             input,
		Tools:             tools,
		ToolChoice:        toolChoice,
		ParallelToolCalls: false,
		Store:             false,
		Stream:            true,
		Include:           []string{},
		PromptCacheKey:    sessionKey,
	}

	// Try router-based backend first, fall back to legacy client
	be := s.backendForModel(req.Model)
	if be == nil {
		// Fall back to legacy Codex client
		cl := s.clientForSessionWithBaseURL(sessionKey, modelEntry.BaseURL)
		be = &legacyClientBackend{client: cl}
	}

	if !req.Stream {
		result, err := be.StreamAndCollect(r.Context(), codexReq)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		s.cache.SaveToolCalls(sessionKey, toolCallsFromBackendResult(result))
		resp := chatResponseFromBackendResult(req.Model, result)
		writeJSON(w, http.StatusOK, resp)
		s.recordUsage(r, key, http.StatusOK, nil)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errNoFlusher)
		return
	}

	created := time.Now().Unix()
	chunkID := newResponseID("chatcmpl")
	collector := sse.NewCollector()
	callInfo := map[string]chatCallInfo{}
	itemToCall := map[string]string{}
	sentRole := false
	sawTool := false

	var usage *protocol.Usage
	err = be.StreamResponses(r.Context(), codexReq, func(ev sse.Event) error {
		collector.Observe(ev.Value)
		if ev.Value.Response != nil && ev.Value.Response.Usage != nil {
			usage = ev.Value.Response.Usage
		}
		switch ev.Value.Type {
		case "response.output_text.delta":
			if ev.Value.Delta == "" {
				return nil
			}
			chunk := OpenAIChatStreamChunk{
				ID:      chunkID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   req.Model,
				Choices: []OpenAIChatDeltaChoice{{
					Index: 0,
					Delta: OpenAIChatDelta{Content: ev.Value.Delta},
				}},
			}
			if !sentRole {
				chunk.Choices[0].Delta.Role = "assistant"
				sentRole = true
			}
			return writeSSE(w, flusher, chunk)
		case "response.content_part.added":
			if ev.Value.Part == nil || ev.Value.Part.Type != "output_text" || ev.Value.Part.Text == "" {
				return nil
			}
			chunk := OpenAIChatStreamChunk{
				ID:      chunkID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   req.Model,
				Choices: []OpenAIChatDeltaChoice{{
					Index: 0,
					Delta: OpenAIChatDelta{Content: ev.Value.Part.Text},
				}},
			}
			if !sentRole {
				chunk.Choices[0].Delta.Role = "assistant"
				sentRole = true
			}
			return writeSSE(w, flusher, chunk)
		case "response.output_item.added":
			if ev.Value.Item == nil || ev.Value.Item.Type != "function_call" {
				return nil
			}
			sawTool = true
			callID := ev.Value.Item.CallID
			if callID == "" {
				callID = ev.Value.Item.ID
			}
			info, ok := callInfo[callID]
			if !ok {
				info = chatCallInfo{index: len(callInfo), id: callID, name: ev.Value.Item.Name}
				callInfo[callID] = info
			}
			if ev.Value.Item.ID != "" {
				itemToCall[ev.Value.Item.ID] = callID
			}
			chunk := OpenAIChatStreamChunk{
				ID:      chunkID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   req.Model,
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
			return writeSSE(w, flusher, chunk)
		case "response.function_call_arguments.delta":
			callID := itemToCall[ev.Value.ItemID]
			if callID == "" {
				return nil
			}
			info, ok := callInfo[callID]
			if !ok {
				return nil
			}
			chunk := OpenAIChatStreamChunk{
				ID:      chunkID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   req.Model,
				Choices: []OpenAIChatDeltaChoice{{
					Index: 0,
					Delta: OpenAIChatDelta{ToolCalls: []OpenAIChatToolCallDelta{{
						Index: info.index,
						ID:    info.id,
						Type:  "function",
						Function: &OpenAIChatToolFuncDelta{
							Arguments: ev.Value.Delta,
						},
					}}},
				}},
			}
			return writeSSE(w, flusher, chunk)
		}
		return nil
	})

	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	calls := toolCallsFromCollector(callInfo, collector)
	s.cache.SaveToolCalls(sessionKey, calls)
	finish := "stop"
	if sawTool {
		finish = "tool_calls"
	}
	finalChunk := OpenAIChatStreamChunk{
		ID:      chunkID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   req.Model,
		Choices: []OpenAIChatDeltaChoice{{
			Index:        0,
			Delta:        OpenAIChatDelta{},
			FinishReason: &finish,
		}},
	}
	_ = writeSSE(w, flusher, finalChunk)
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
	s.recordUsage(r, key, http.StatusOK, usage)
}

func chatResponseFromResult(model string, result client.StreamResult) OpenAIChatResponse {
	resp := OpenAIChatResponse{
		ID:      newResponseID("chatcmpl"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []OpenAIChatChoice{{
			Index: 0,
			Message: OpenAIChatMessage{
				Role:    "assistant",
				Content: result.Text,
			},
			FinishReason: "stop",
		}},
	}
	if len(result.ToolCalls) > 0 {
		calls := make([]OpenAIChatToolCall, 0, len(result.ToolCalls))
		for _, call := range result.ToolCalls {
			calls = append(calls, OpenAIChatToolCall{
				ID:   call.CallID,
				Type: "function",
				Function: OpenAIChatToolFunction{
					Name:      call.Name,
					Arguments: call.Arguments,
				},
			})
		}
		resp.Choices[0].Message.ToolCalls = calls
		resp.Choices[0].Message.Content = ""
		resp.Choices[0].FinishReason = "tool_calls"
	}
	return resp
}

func toolCallsFromCollector(callInfo map[string]chatCallInfo, collector *sse.Collector) map[string]ToolCall {
	out := map[string]ToolCall{}
	if collector == nil {
		return out
	}
	args := collector.AllFunctionArgs()
	for callID, info := range callInfo {
		out[callID] = ToolCall{Name: info.name, Arguments: args[callID]}
	}
	return out
}

// chatResponseFromBackendResult converts a backend.StreamResult to OpenAI chat response.
func chatResponseFromBackendResult(model string, result backend.StreamResult) OpenAIChatResponse {
	resp := OpenAIChatResponse{
		ID:      newResponseID("chatcmpl"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []OpenAIChatChoice{{
			Index: 0,
			Message: OpenAIChatMessage{
				Role:    "assistant",
				Content: result.Text,
			},
			FinishReason: "stop",
		}},
	}
	if len(result.ToolCalls) > 0 {
		calls := make([]OpenAIChatToolCall, 0, len(result.ToolCalls))
		for _, call := range result.ToolCalls {
			calls = append(calls, OpenAIChatToolCall{
				ID:   call.CallID,
				Type: "function",
				Function: OpenAIChatToolFunction{
					Name:      call.Name,
					Arguments: call.Arguments,
				},
			})
		}
		resp.Choices[0].Message.ToolCalls = calls
		resp.Choices[0].Message.Content = ""
		resp.Choices[0].FinishReason = "tool_calls"
	}
	return resp
}

// toolCallsFromBackendResult converts backend tool calls to the cache format.
func toolCallsFromBackendResult(result backend.StreamResult) map[string]ToolCall {
	calls := map[string]ToolCall{}
	for _, call := range result.ToolCalls {
		calls[call.CallID] = ToolCall{Name: call.Name, Arguments: call.Arguments}
	}
	return calls
}

// legacyClientBackend wraps the old client.Client to implement backend.Backend.
type legacyClientBackend struct {
	client *client.Client
}

func (l *legacyClientBackend) Name() string {
	return "codex-legacy"
}

func (l *legacyClientBackend) StreamResponses(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error {
	return l.client.StreamResponses(ctx, req, onEvent)
}

func (l *legacyClientBackend) StreamAndCollect(ctx context.Context, req protocol.ResponsesRequest) (backend.StreamResult, error) {
	result, err := l.client.StreamAndCollect(ctx, req)
	if err != nil {
		return backend.StreamResult{}, err
	}
	// Convert client.StreamResult to backend.StreamResult
	toolCalls := make([]backend.ToolCall, len(result.ToolCalls))
	for i, tc := range result.ToolCalls {
		toolCalls[i] = backend.ToolCall{
			CallID:    tc.CallID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
		}
	}
	return backend.StreamResult{
		Text:      result.Text,
		ToolCalls: toolCalls,
	}, nil
}

func (l *legacyClientBackend) ListModels(ctx context.Context) ([]backend.ModelInfo, error) {
	// Legacy client doesn't support model listing
	return nil, nil
}
