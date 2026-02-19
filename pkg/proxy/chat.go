package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"godex/pkg/harness"
)

type chatCallInfo struct {
	index int
	id    string
	name  string
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := newResponseID("pxreq")
	var req OpenAIChatRequest
	if err := readJSON(r, &req); err != nil {
		s.traceMessage(requestID, "proxy", "in", "/v1/chat/completions", "openclaw_request_decode_error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if rawReq, err := json.Marshal(req); err == nil {
		s.tracePayload(requestID, "proxy", "in", "/v1/chat/completions", "openclaw_request", json.RawMessage(rawReq))
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
	items := make([]OpenAIItem, 0, len(req.Messages)*2) // May expand due to tool_calls
	for _, msg := range req.Messages {
		switch msg.Role {
		case "tool":
			// OpenAI tool result → Codex function_call_output
			output := extractText(msg.Content)
			items = append(items, OpenAIItem{
				Type:   "function_call_output",
				CallID: msg.ToolCallID,
				Output: output,
			})
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				// Assistant with tool_calls → Codex function_call items
				for _, tc := range msg.ToolCalls {
					items = append(items, OpenAIItem{
						Type:      "function_call",
						CallID:    tc.ID,
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					})
				}
			} else {
				// Regular assistant message
				items = append(items, OpenAIItem{Type: "message", Role: msg.Role, Content: msg.Content})
			}
		default:
			// user, system, developer - pass through as messages
			items = append(items, OpenAIItem{Type: "message", Role: msg.Role, Content: msg.Content})
		}
	}
	input, system, err := buildSystemAndInput(sessionKey, items, s.cache)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	instructions := mergeInstructions("", system)
	instructions = s.resolveInstructions(sessionKey, instructions)
	tools := mapChatTools(req.Tools)
	_, tools = resolveToolChoice(req.ToolChoice, tools)

	// Try harness-based routing first
	if h := s.harnessForModel(req.Model); h != nil {
		turn := buildTurnFromChat(req.Model, instructions, input, tools)
		if rawTurn, err := json.Marshal(turn); err == nil {
			s.tracePayload(requestID, "proxy_harness", "out", "/v1/chat/completions", "harness_turn", json.RawMessage(rawTurn))
		}
		if !req.Stream {
			result, err := h.StreamAndCollect(requestContext(r), turn)
			if err != nil {
				s.traceMessage(requestID, "proxy_harness", "in", "/v1/chat/completions", "stream_and_collect_error", err.Error())
				writeError(w, http.StatusBadGateway, err)
				return
			}
			calls := map[string]ToolCall{}
			for _, tc := range result.ToolCalls {
				calls[tc.CallID] = ToolCall{Name: tc.Name, Arguments: tc.Arguments}
			}
			s.cache.SaveToolCalls(sessionKey, calls)
			resp := harnessResultToChatResponse(req.Model, result)
			if rawResp, err := json.Marshal(resp); err == nil {
				s.tracePayload(requestID, "proxy_openclaw", "out", "/v1/chat/completions", "json.response", json.RawMessage(rawResp))
			}
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
		if err := s.harnessChatStream(requestContext(r), w, flusher, h, turn, req.Model, key, start, sessionKey, requestID); err != nil {
			s.traceMessage(requestID, "proxy", "out", "/v1/chat/completions", "stream_error", err.Error())
			_ = writeSSE(w, flusher, map[string]any{
				"type":    "error",
				"message": err.Error(),
			})
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			flusher.Flush()
			return
		}
		return
	}
	writeError(w, http.StatusBadRequest, fmt.Errorf("model %q not available", req.Model))
}

// harnessResultToChatResponse converts a harness.TurnResult to OpenAI chat response.
func harnessResultToChatResponse(model string, result *harness.TurnResult) OpenAIChatResponse {
	resp := OpenAIChatResponse{
		ID:      newResponseID("chatcmpl"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []OpenAIChatChoice{{
			Index: 0,
			Message: OpenAIChatMessage{
				Role:    "assistant",
				Content: result.FinalText,
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

// (toolCallsFromResult is defined in server.go)
