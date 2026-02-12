package proxy

import (
	"net/http"
	"time"

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
	if !s.requireAuth(w, r) {
		return
	}
	var req OpenAIChatRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Model == "" {
		req.Model = s.cfg.Model
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

	cl := s.clientForSession(sessionKey)
	if !req.Stream {
		result, err := cl.StreamAndCollect(r.Context(), codexReq)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		s.cache.SaveToolCalls(sessionKey, toolCallsFromResult(result))
		resp := chatResponseFromResult(req.Model, result)
		writeJSON(w, http.StatusOK, resp)
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

	err = cl.StreamResponses(r.Context(), codexReq, func(ev sse.Event) error {
		collector.Observe(ev.Value)
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
