package sse

import (
	"strings"
	"testing"
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
