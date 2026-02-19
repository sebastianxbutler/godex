package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"godex/pkg/harness"
)

func TestExecJSONEmitter_MapsCoreEvents(t *testing.T) {
	var out bytes.Buffer
	emitter := newExecJSONEmitter(&out, "")

	events := []harness.Event{
		harness.NewTextEvent("hello"),
		harness.NewToolCallEvent("call_1", "read_file", `{"path":"README.md"}`),
		harness.NewUsageEvent(12, 7),
		harness.NewDoneEvent(),
	}
	for _, ev := range events {
		if err := emitter.Emit(ev); err != nil {
			t.Fatalf("emit event: %v", err)
		}
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 6 {
		t.Fatalf("expected 6 JSONL lines, got %d: %q", len(lines), out.String())
	}

	var got []map[string]any
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %d invalid json: %v", i, err)
		}
		got = append(got, m)
	}

	assertEventType(t, got[0], "response.content_part.added")
	assertEventType(t, got[1], "response.output_text.delta")
	assertEventType(t, got[2], "response.output_item.added")
	assertEventType(t, got[3], "response.function_call_arguments.delta")
	assertEventType(t, got[4], "response.output_item.done")
	assertEventType(t, got[5], "response.completed")

	response, ok := got[5]["response"].(map[string]any)
	if !ok {
		t.Fatalf("response.completed missing response object: %#v", got[5])
	}
	usage, ok := response["usage"].(map[string]any)
	if !ok {
		t.Fatalf("response.completed missing usage: %#v", got[5])
	}
	if usage["input_tokens"] != float64(12) || usage["output_tokens"] != float64(7) {
		t.Fatalf("unexpected usage values: %#v", usage)
	}
}

func TestExecJSONEmitter_ErrorAndDoneWithoutUsage(t *testing.T) {
	var out bytes.Buffer
	emitter := newExecJSONEmitter(&out, "")

	if err := emitter.Emit(harness.NewErrorEvent("boom")); err != nil {
		t.Fatalf("emit error: %v", err)
	}
	if err := emitter.Emit(harness.NewDoneEvent()); err != nil {
		t.Fatalf("emit done: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), out.String())
	}

	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("first line invalid json: %v", err)
	}
	assertEventType(t, first, "error")
	if first["message"] != "boom" {
		t.Fatalf("expected error message boom, got %#v", first["message"])
	}

	var second map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("second line invalid json: %v", err)
	}
	assertEventType(t, second, "response.completed")
}

func assertEventType(t *testing.T, ev map[string]any, want string) {
	t.Helper()
	if ev["type"] != want {
		t.Fatalf("event type = %#v, want %q", ev["type"], want)
	}
}
