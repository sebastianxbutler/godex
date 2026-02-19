package proxy

import (
	"encoding/json"
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

func TestBuildSystemAndInput_SkipsFailedEmptyToolCallHistoryPair(t *testing.T) {
	items := []OpenAIItem{
		{Type: "message", Role: "user", Content: "Run ls"},
		{Type: "function_call", CallID: "call_bad_exec", Name: "exec", Arguments: "{}"},
		{Type: "function_call_output", CallID: "call_bad_exec", Output: "Validation failed for tool \"exec\": command is required"},
		{Type: "message", Role: "assistant", Content: "Retrying..."},
	}

	input, _, err := buildSystemAndInput("test-session", items, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect: user message + assistant message only; bad call pair skipped.
	if len(input) != 2 {
		t.Fatalf("expected 2 items after skipping bad call pair, got %d", len(input))
	}
	if input[0].Type != "message" || input[0].Role != "user" {
		t.Fatalf("unexpected first item: %#v", input[0])
	}
	if input[1].Type != "message" || input[1].Role != "assistant" {
		t.Fatalf("unexpected second item: %#v", input[1])
	}
}

func TestBuildSystemAndInput_EmptyArgsCallNotSkippedWithoutValidationFailure(t *testing.T) {
	items := []OpenAIItem{
		{Type: "message", Role: "user", Content: "Status?"},
		{Type: "function_call", CallID: "call_status", Name: "session_status", Arguments: "{}"},
		{Type: "function_call_output", CallID: "call_status", Output: "ok"},
	}

	input, _, err := buildSystemAndInput("test-session", items, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect all 3 preserved (valid {} args for tools that take no required input).
	if len(input) != 3 {
		t.Fatalf("expected 3 items, got %d", len(input))
	}
	if input[1].Type != "function_call" || input[1].CallID != "call_status" {
		t.Fatalf("missing function_call item: %#v", input)
	}
}

func TestBuildSystemAndInput_AssistantContentType(t *testing.T) {
	// Verify that assistant messages use output_text, not input_text
	// Codex rejects input_text for assistant role

	items := []OpenAIItem{
		{Type: "message", Role: "user", Content: "Hello"},
		{Type: "message", Role: "assistant", Content: "Hi there!"},
		{Type: "message", Role: "user", Content: "How are you?"},
	}

	input, _, err := buildSystemAndInput("test-session", items, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(input) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(input))
	}

	// User message should use input_text
	if input[0].Content[0].Type != "input_text" {
		t.Errorf("user message should use input_text, got %s", input[0].Content[0].Type)
	}

	// Assistant message should use output_text
	if input[1].Content[0].Type != "output_text" {
		t.Errorf("assistant message should use output_text, got %s", input[1].Content[0].Type)
	}

	// Second user message should use input_text
	if input[2].Content[0].Type != "input_text" {
		t.Errorf("user message should use input_text, got %s", input[2].Content[0].Type)
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

func TestMapTools_FunctionStrictDefaultsTrue(t *testing.T) {
	tools := []OpenAITool{{
		Type: "function",
		Name: "exec",
		Parameters: json.RawMessage(`{
			"type":"object",
			"required":["command"],
			"properties":{"command":{"type":"string"}}
		}`),
	}}
	got := mapTools(tools)
	if len(got) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(got))
	}
	if !got[0].Strict {
		t.Fatalf("expected strict=true by default")
	}
}

func TestMapTools_FunctionStrictFalseHintStillNormalizesToStrict(t *testing.T) {
	disabled := false
	tools := []OpenAITool{{
		Type: "function",
		Name: "exec",
		Parameters: json.RawMessage(`{
			"type":"object",
			"required":["command"],
			"properties":{"command":{"type":"string"}}
		}`),
		Strict: &disabled,
	}}
	got := mapTools(tools)
	if len(got) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(got))
	}
	if !got[0].Strict {
		t.Fatalf("expected strict=true even when client hints strict=false")
	}
}

func TestMapTools_StrictAddsRootAdditionalPropertiesFalse(t *testing.T) {
	tools := []OpenAITool{{
		Type: "function",
		Name: "read",
		Parameters: json.RawMessage(`{
			"type":"object",
			"required":["path"],
			"properties":{"path":{"type":"string"}}
		}`),
	}}
	got := mapTools(tools)
	if len(got) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(got))
	}
	if !got[0].Strict {
		t.Fatalf("expected strict=true")
	}
	var schema map[string]any
	if err := json.Unmarshal(got[0].Parameters, &schema); err != nil {
		t.Fatalf("invalid mapped schema: %v", err)
	}
	if ap, ok := schema["additionalProperties"].(bool); !ok || ap {
		t.Fatalf("expected additionalProperties=false, got %#v", schema["additionalProperties"])
	}
}

func TestMapTools_StrictInfersObjectType(t *testing.T) {
	tools := []OpenAITool{{
		Type: "function",
		Name: "read",
		Parameters: json.RawMessage(`{
			"required":["path"],
			"properties":{"path":{"type":"string"}}
		}`),
	}}
	got := mapTools(tools)
	if len(got) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(got))
	}
	if !got[0].Strict {
		t.Fatalf("expected strict=true")
	}
	var schema map[string]any
	if err := json.Unmarshal(got[0].Parameters, &schema); err != nil {
		t.Fatalf("invalid mapped schema: %v", err)
	}
	if typ, _ := schema["type"].(string); typ != "object" {
		t.Fatalf("expected type=object, got %#v", schema["type"])
	}
	if ap, ok := schema["additionalProperties"].(bool); !ok || ap {
		t.Fatalf("expected additionalProperties=false, got %#v", schema["additionalProperties"])
	}
}

func TestMapTools_StrictNormalizesRequiredAndOptional(t *testing.T) {
	tools := []OpenAITool{{
		Type: "function",
		Name: "read",
		Parameters: json.RawMessage(`{
			"type":"object",
			"required":["path"],
			"properties":{
				"path":{"type":"string"},
				"offset":{"type":"number"},
				"limit":{"type":"number"}
			}
		}`),
	}}
	got := mapTools(tools)
	if len(got) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(got))
	}
	var schema map[string]any
	if err := json.Unmarshal(got[0].Parameters, &schema); err != nil {
		t.Fatalf("invalid mapped schema: %v", err)
	}
	reqRaw, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("required missing or wrong type: %#v", schema["required"])
	}
	reqSet := map[string]bool{}
	for _, v := range reqRaw {
		if s, ok := v.(string); ok {
			reqSet[s] = true
		}
	}
	for _, k := range []string{"path", "offset", "limit"} {
		if !reqSet[k] {
			t.Fatalf("expected %q in required, got %#v", k, reqRaw)
		}
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing: %#v", schema["properties"])
	}
	offset, ok := props["offset"].(map[string]any)
	if !ok {
		t.Fatalf("offset schema missing")
	}
	types, ok := offset["type"].([]any)
	if !ok || len(types) != 2 {
		t.Fatalf("expected nullable offset type, got %#v", offset["type"])
	}
}

func TestMapTools_StrictNormalizesNestedObjectInUnion(t *testing.T) {
	tools := []OpenAITool{{
		Type: "function",
		Name: "exec",
		Parameters: json.RawMessage(`{
			"type":"object",
			"required":["command"],
			"properties":{
				"command":{"type":"string"},
				"env":{
					"type":["object","null"],
					"properties":{"FOO":{"type":"string"}}
				}
			}
		}`),
	}}
	got := mapTools(tools)
	if len(got) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(got))
	}
	var schema map[string]any
	if err := json.Unmarshal(got[0].Parameters, &schema); err != nil {
		t.Fatalf("invalid mapped schema: %v", err)
	}
	props := schema["properties"].(map[string]any)
	env := props["env"].(map[string]any)
	if ap, ok := env["additionalProperties"].(bool); !ok || ap {
		t.Fatalf("expected nested additionalProperties=false, got %#v", env["additionalProperties"])
	}
}
