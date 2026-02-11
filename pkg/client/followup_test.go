package client

import (
	"testing"

	"godex/pkg/protocol"
)

func TestBuildToolFollowupInputs(t *testing.T) {
	calls := []ToolCall{{CallID: "call_1", Name: "add", Arguments: "{\"a\":2}"}}
	outputs := map[string]string{"call_1": "5"}
	items := BuildToolFollowupInputs(calls, outputs)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Type != "function_call" || items[1].Type != "function_call_output" {
		t.Fatalf("unexpected item types: %+v", items)
	}
	if items[1].Output != "5" {
		t.Fatalf("unexpected output: %s", items[1].Output)
	}
	_ = protocol.ResponseInputItem{}
}
