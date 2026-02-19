package sse

import (
	"strings"
	"testing"

	"godex/pkg/protocol"
)

func TestParseStreamAndCollector(t *testing.T) {
	stream := strings.Join([]string{
		"data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"item_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"add\",\"arguments\":\"\"}}",
		"",
		"data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"item_1\",\"delta\":\"{\\\"a\\\":2\"}",
		"",
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}",
		"",
	}, "\n")

	collector := NewCollector()
	count := 0
	err := ParseStream(strings.NewReader(stream), func(ev Event) error {
		count++
		collector.Observe(ev.Value)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("event count mismatch: got %d", count)
	}
	if got := collector.FunctionArgs("call_1"); got != "{\"a\":2" {
		t.Fatalf("arguments mismatch: got %q", got)
	}
	if got := collector.OutputText(); got != "hello" {
		t.Fatalf("text mismatch: got %q", got)
	}
}

func TestCollector_DeltaBeforeOutputItemAdded(t *testing.T) {
	stream := strings.Join([]string{
		"data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"item_1\",\"delta\":\"{\\\"command\\\":\\\"ls\\\"}\"}",
		"",
		"data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"item_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"exec\"}}",
		"",
	}, "\n")

	collector := NewCollector()
	err := ParseStream(strings.NewReader(stream), func(ev Event) error {
		collector.Observe(ev.Value)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := collector.CallIDForItem("item_1"); got != "call_1" {
		t.Fatalf("call id mismatch: got %q", got)
	}
	if got := collector.FunctionName("call_1"); got != "exec" {
		t.Fatalf("name mismatch: got %q", got)
	}
	if got := collector.FunctionArgs("call_1"); got != "{\"command\":\"ls\"}" {
		t.Fatalf("arguments mismatch: got %q", got)
	}
}

func TestCollector_DeltaWithCallID(t *testing.T) {
	stream := strings.Join([]string{
		"data: {\"type\":\"response.function_call_arguments.delta\",\"call_id\":\"call_2\",\"delta\":\"{\\\"command\\\":\\\"ls\\\"}\"}",
		"",
		"data: {\"type\":\"response.function_call_arguments.done\",\"call_id\":\"call_2\",\"name\":\"exec\"}",
		"",
	}, "\n")

	collector := NewCollector()
	err := ParseStream(strings.NewReader(stream), func(ev Event) error {
		collector.Observe(ev.Value)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := collector.FunctionName("call_2"); got != "exec" {
		t.Fatalf("name mismatch: got %q", got)
	}
	if got := collector.FunctionArgs("call_2"); got != "{\"command\":\"ls\"}" {
		t.Fatalf("arguments mismatch: got %q", got)
	}
}

func TestCollector_SnapshotArgsDoNotDuplicateDeltas(t *testing.T) {
	collector := NewCollector()
	collector.Observe(protocol.StreamEvent{
		Type:   "response.function_call_arguments.delta",
		CallID: "call_3",
		Delta:  `{"command":"ls"}`,
	})
	collector.Observe(protocol.StreamEvent{
		Type: "response.function_call_arguments.done",
		Item: &protocol.OutputItem{
			CallID:    "call_3",
			Name:      "exec",
			Arguments: `{"command":"ls"}{"command":"ls"}`,
		},
	})
	if got := collector.FunctionArgs("call_3"); got != `{"command":"ls"}` {
		t.Fatalf("arguments mismatch: got %q", got)
	}
}

func TestCollector_MarkToolCallEmitted(t *testing.T) {
	collector := NewCollector()
	if !collector.MarkToolCallEmitted("call_a") {
		t.Fatal("expected first call to emit")
	}
	if collector.MarkToolCallEmitted("call_a") {
		t.Fatal("expected duplicate call to be suppressed")
	}
}
