package backend

import (
	"context"
	"fmt"

	"godex/pkg/protocol"
	"godex/pkg/sse"
)

// MockBackend is a test backend that returns predefined responses.
type MockBackend struct {
	BackendName string
	Response    string
	ToolCalls   []ToolCall
	Err         error
	// StreamEvents allows custom streaming behavior
	StreamEvents []sse.Event
}

var _ Backend = (*MockBackend)(nil)

func (m *MockBackend) Name() string {
	if m.BackendName == "" {
		return "mock"
	}
	return m.BackendName
}

func (m *MockBackend) StreamResponses(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error {
	if m.Err != nil {
		return m.Err
	}

	// If custom events provided, use them
	if len(m.StreamEvents) > 0 {
		for _, ev := range m.StreamEvents {
			if err := onEvent(ev); err != nil {
				return err
			}
		}
		return nil
	}

	// Default: emit text delta + done
	if m.Response != "" {
		ev := sse.Event{
			Value: protocol.StreamEvent{
				Type:  "response.output_text.delta",
				Delta: m.Response,
			},
		}
		if err := onEvent(ev); err != nil {
			return err
		}
	}

	// Emit tool calls if any
	for _, tc := range m.ToolCalls {
		ev := sse.Event{
			Value: protocol.StreamEvent{
				Type: "response.output_item.added",
				Item: &protocol.OutputItem{
					ID:     fmt.Sprintf("item_%s", tc.CallID),
					Type:   "function_call",
					Name:   tc.Name,
					CallID: tc.CallID,
				},
			},
		}
		if err := onEvent(ev); err != nil {
			return err
		}
		// Emit arguments
		ev = sse.Event{
			Value: protocol.StreamEvent{
				Type:   "response.function_call_arguments.delta",
				ItemID: fmt.Sprintf("item_%s", tc.CallID),
				Delta:  tc.Arguments,
			},
		}
		if err := onEvent(ev); err != nil {
			return err
		}
	}

	// Done event
	ev := sse.Event{
		Value: protocol.StreamEvent{
			Type: "response.done",
			Response: &protocol.ResponseRef{
				Usage: &protocol.Usage{
					InputTokens:  10,
					OutputTokens: 5,
				},
			},
		},
	}
	return onEvent(ev)
}

func (m *MockBackend) StreamAndCollect(ctx context.Context, req protocol.ResponsesRequest) (StreamResult, error) {
	if m.Err != nil {
		return StreamResult{}, m.Err
	}

	return StreamResult{
		Text:      m.Response,
		ToolCalls: m.ToolCalls,
		Usage: &protocol.Usage{
			InputTokens:  10,
			OutputTokens: 5,
		},
	}, nil
}

// ListModels returns mock model info.
func (m *MockBackend) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return []ModelInfo{
		{ID: "mock-model", DisplayName: "Mock Model"},
	}, nil
}
