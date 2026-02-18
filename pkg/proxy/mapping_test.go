package proxy

import (
	"testing"
)

func TestBuildSystemAndInput_OrphanedToolResult(t *testing.T) {
	// This test verifies that orphaned function_call_output items
	// (tool results without matching function_calls) are skipped gracefully
	// instead of causing a hard error.
	//
	// This happens when:
	// 1. A tool call is aborted mid-stream
	// 2. OpenClaw performs transcript repair and inserts a synthetic error result
	// 3. The session continues through godex with the orphaned result in history

	items := []OpenAIItem{
		{Type: "message", Role: "user", Content: "Hello"},
		// Orphaned tool result - no matching function_call in history
		{Type: "function_call_output", CallID: "toolu_orphaned_123", Output: "[error: aborted]"},
		{Type: "message", Role: "user", Content: "Continue please"},
	}

	// No cache, so the orphaned result can't be recovered
	input, system, err := buildSystemAndInput("test-session", items, nil)

	// Should NOT error - orphaned results should be skipped
	if err != nil {
		t.Fatalf("expected no error for orphaned tool result, got: %v", err)
	}

	// System should be empty (no system messages in input)
	if system != "" {
		t.Errorf("expected empty system, got: %s", system)
	}

	// Should have 2 messages (the user messages), orphaned tool result skipped
	if len(input) != 2 {
		t.Errorf("expected 2 input items, got %d", len(input))
	}

	// Verify the messages are correct
	if len(input) >= 2 {
		if input[0].Role != "user" {
			t.Errorf("expected first input role 'user', got '%s'", input[0].Role)
		}
		if input[1].Role != "user" {
			t.Errorf("expected second input role 'user', got '%s'", input[1].Role)
		}
	}
}

func TestBuildSystemAndInput_ValidToolResult(t *testing.T) {
	// Verify that valid tool call + result pairs still work correctly

	items := []OpenAIItem{
		{Type: "message", Role: "user", Content: "List files"},
		{Type: "function_call", CallID: "call_123", Name: "exec", Arguments: `{"command":"ls"}`},
		{Type: "function_call_output", CallID: "call_123", Output: "file1\nfile2"},
		{Type: "message", Role: "assistant", Content: "Here are the files"},
	}

	input, _, err := buildSystemAndInput("test-session", items, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have: user message, function_call, function_call_output, assistant message
	if len(input) != 4 {
		t.Errorf("expected 4 input items, got %d", len(input))
	}
}

func TestBuildSystemAndInput_MissingCallID(t *testing.T) {
	// Verify that function_call_output without call_id still errors
	// (this is a malformed request, not an orphaned result)

	items := []OpenAIItem{
		{Type: "message", Role: "user", Content: "Hello"},
		{Type: "function_call_output", CallID: "", Output: "result"},
	}

	_, _, err := buildSystemAndInput("test-session", items, nil)

	if err == nil {
		t.Fatal("expected error for missing call_id")
	}

	if err.Error() != "function_call_output missing call_id" {
		t.Errorf("unexpected error message: %v", err)
	}
}
