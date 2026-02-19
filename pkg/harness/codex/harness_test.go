package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"godex/pkg/harness"
)

func TestNewMock_Defaults(t *testing.T) {
	mock := NewMock()
	if mock.Name() != "codex" {
		t.Errorf("expected name 'codex', got %q", mock.Name())
	}
	models, err := mock.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "gpt-5.2-codex" {
		t.Errorf("unexpected models: %v", models)
	}
}

func TestWithApplyPatchFlow(t *testing.T) {
	mock := NewMock(WithApplyPatchFlow("main.go", "@@ func main():\n- old\n+ new"))

	ctx := context.Background()
	turn := &harness.Turn{Model: "test", Messages: []harness.Message{{Role: "user", Content: "fix it"}}}

	// First turn: should get preamble + tool call
	var events []harness.Event
	err := mock.StreamTurn(ctx, turn, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Kind != harness.EventPreamble {
		t.Errorf("expected preamble, got %s", events[0].Kind)
	}
	if events[1].Kind != harness.EventToolCall {
		t.Errorf("expected tool_call, got %s", events[1].Kind)
	}
	if events[1].ToolCall.Name != "apply_patch" {
		t.Errorf("expected apply_patch, got %s", events[1].ToolCall.Name)
	}

	// Second turn: should get text + usage
	events = events[:0]
	err = mock.StreamTurn(ctx, turn, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Kind != harness.EventText {
		t.Errorf("expected text, got %s", events[0].Kind)
	}
}

func TestWithPlanFlow(t *testing.T) {
	steps := []harness.PlanEvent{
		{Title: "Read code", Status: "completed"},
		{Title: "Write tests", Status: "in_progress"},
		{Title: "Refactor", Status: "pending"},
	}
	mock := NewMock(WithPlanFlow(steps))

	ctx := context.Background()
	turn := &harness.Turn{Model: "test"}

	var events []harness.Event
	err := mock.StreamTurn(ctx, turn, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// preamble + 3 plan events
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	if events[0].Kind != harness.EventPreamble {
		t.Errorf("expected preamble first")
	}
	for i := 1; i <= 3; i++ {
		if events[i].Kind != harness.EventPlanUpdate {
			t.Errorf("event %d: expected plan_update, got %s", i, events[i].Kind)
		}
	}
	if events[1].Plan.Title != "Read code" {
		t.Errorf("expected 'Read code', got %q", events[1].Plan.Title)
	}
}

func TestWithTextResponse(t *testing.T) {
	mock := NewMock(WithTextResponse("Hello, world!"))

	ctx := context.Background()
	result, err := mock.StreamAndCollect(ctx, &harness.Turn{Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", result.FinalText)
	}
	if result.Usage == nil {
		t.Error("expected usage info")
	}
}

func TestEmitPlanEvents(t *testing.T) {
	h := &Harness{}
	args := `{"steps":[{"title":"Step 1","status":"pending"},{"title":"Step 2","status":"in_progress"}]}`

	var events []harness.Event
	err := h.emitPlanEvents(args, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Plan.Title != "Step 1" || events[0].Plan.Status != "pending" {
		t.Errorf("unexpected first step: %+v", events[0].Plan)
	}
	if events[1].Plan.StepIndex != 1 {
		t.Errorf("expected step index 1, got %d", events[1].Plan.StepIndex)
	}
}

func TestEmitPlanEvents_InvalidJSON(t *testing.T) {
	h := &Harness{}
	var events []harness.Event
	err := h.emitPlanEvents("not json", func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Should fall back to emitting as tool call
	if len(events) != 1 || events[0].Kind != harness.EventToolCall {
		t.Errorf("expected fallback tool call event, got %v", events)
	}
}

func TestBuildRequest(t *testing.T) {
	h := &Harness{defaultModel: "gpt-5.2-codex"}
	turn := &harness.Turn{
		Messages: []harness.Message{
			{Role: "user", Content: "Hello"},
		},
		Reasoning: &harness.ReasoningConfig{Effort: "high", Summaries: true},
	}

	req, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "gpt-5.2-codex" {
		t.Errorf("expected default model, got %s", req.Model)
	}
	if !req.Stream {
		t.Error("expected stream to be true")
	}
	if len(req.Input) != 1 || req.Input[0].Role != "user" {
		t.Errorf("unexpected input: %+v", req.Input)
	}
	if req.Reasoning == nil || req.Reasoning.Effort != "high" {
		t.Error("expected reasoning config")
	}
	if req.Reasoning.Summary != "auto" {
		t.Error("expected summary auto")
	}
	// Should have default tools
	if len(req.Tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(req.Tools))
	}
}

func TestBuildRequest_ModelOverride(t *testing.T) {
	h := &Harness{defaultModel: "gpt-5.2-codex"}
	turn := &harness.Turn{Model: "o3"}
	req, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "o3" {
		t.Errorf("expected o3, got %s", req.Model)
	}
}

func TestBuildRequest_UserToolsAreStrict(t *testing.T) {
	h := &Harness{defaultModel: "gpt-5.2-codex"}
	turn := &harness.Turn{
		Tools: []harness.ToolSpec{{
			Name:        "exec",
			Description: "Run command",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"command"},
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
				},
			},
		}},
	}
	req, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(req.Tools))
	}
	if !req.Tools[0].Strict {
		t.Fatal("expected custom function tools to be strict")
	}
	if !strings.Contains(string(req.Tools[0].Parameters), `"additionalProperties":false`) {
		t.Fatalf("expected strict schema to include additionalProperties=false, got: %s", string(req.Tools[0].Parameters))
	}
}

func TestBuildRequest_UserToolsInferObjectTypeForStrict(t *testing.T) {
	h := &Harness{defaultModel: "gpt-5.2-codex"}
	turn := &harness.Turn{
		Tools: []harness.ToolSpec{{
			Name: "read",
			Parameters: map[string]any{
				"required": []string{"path"},
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
			},
		}},
	}
	req, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(req.Tools))
	}
	raw := string(req.Tools[0].Parameters)
	if !strings.Contains(raw, `"type":"object"`) {
		t.Fatalf("expected inferred type=object in schema, got: %s", raw)
	}
	if !strings.Contains(raw, `"additionalProperties":false`) {
		t.Fatalf("expected additionalProperties=false, got: %s", raw)
	}
}

func TestBuildRequest_UserToolsStrictNormalizesRequired(t *testing.T) {
	h := &Harness{defaultModel: "gpt-5.2-codex"}
	turn := &harness.Turn{
		Tools: []harness.ToolSpec{{
			Name: "read",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"path"},
				"properties": map[string]any{
					"path":   map[string]any{"type": "string"},
					"offset": map[string]any{"type": "number"},
					"limit":  map[string]any{"type": "number"},
				},
			},
		}},
	}
	req, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(req.Tools))
	}
	var schema map[string]any
	if err := json.Unmarshal(req.Tools[0].Parameters, &schema); err != nil {
		t.Fatalf("invalid schema: %v", err)
	}
	rawReq, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("required missing: %#v", schema["required"])
	}
	reqSet := map[string]bool{}
	for _, v := range rawReq {
		if s, ok := v.(string); ok {
			reqSet[s] = true
		}
	}
	for _, k := range []string{"path", "offset", "limit"} {
		if !reqSet[k] {
			t.Fatalf("required missing %s: %#v", k, rawReq)
		}
	}
}

func TestBuildRequest_UserToolsStrictNormalizesNestedObjectInUnion(t *testing.T) {
	h := &Harness{defaultModel: "gpt-5.2-codex"}
	turn := &harness.Turn{
		Tools: []harness.ToolSpec{{
			Name: "exec",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"command"},
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
					"env": map[string]any{
						"type": []any{"object", "null"},
						"properties": map[string]any{
							"FOO": map[string]any{"type": "string"},
						},
					},
				},
			},
		}},
	}
	req, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(req.Tools[0].Parameters, &schema); err != nil {
		t.Fatalf("invalid schema: %v", err)
	}
	props := schema["properties"].(map[string]any)
	env := props["env"].(map[string]any)
	if ap, ok := env["additionalProperties"].(bool); !ok || ap {
		t.Fatalf("expected nested additionalProperties=false, got %#v", env["additionalProperties"])
	}
}

func TestBuildRequest_MessageTypes(t *testing.T) {
	h := &Harness{defaultModel: "test"}
	turn := &harness.Turn{
		Messages: []harness.Message{
			{Role: "user", Content: "do it"},
			{Role: "assistant", Content: "args", Name: "shell", ToolID: "call_1"},
			{Role: "tool", Content: "output", ToolID: "call_1"},
			{Role: "assistant", Content: "Done!"},
		},
	}
	req, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Input) != 4 {
		t.Fatalf("expected 4 input items, got %d", len(req.Input))
	}
	if req.Input[0].Type != "message" || req.Input[0].Role != "user" {
		t.Error("expected user message")
	}
	if req.Input[1].Type != "function_call" {
		t.Error("expected function_call")
	}
	if req.Input[2].Type != "function_call_output" {
		t.Error("expected function_call_output")
	}
	if req.Input[3].Type != "message" || req.Input[3].Role != "assistant" {
		t.Error("expected assistant message")
	}
	if len(req.Input[3].Content) != 1 || req.Input[3].Content[0].Type != "output_text" {
		t.Error("expected assistant history content type output_text")
	}
}

func TestDefaultTools(t *testing.T) {
	tools := DefaultTools()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, expected := range []string{"apply_patch", "update_plan", "shell"} {
		if !names[expected] {
			t.Errorf("missing tool %q", expected)
		}
	}
}

func TestApplyPatchToolSpec_HasLarkGrammar(t *testing.T) {
	spec := ApplyPatchToolSpec()
	if spec.Format == nil {
		t.Fatal("expected format")
	}
	if spec.Format.Syntax != "lark" {
		t.Errorf("expected lark syntax, got %q", spec.Format.Syntax)
	}
	if spec.Format.Definition == "" {
		t.Error("expected grammar definition")
	}
}

func TestUpdatePlanToolSpec_ValidJSON(t *testing.T) {
	spec := UpdatePlanToolSpec()
	var schema map[string]any
	if err := json.Unmarshal(spec.Parameters, &schema); err != nil {
		t.Fatalf("invalid parameters JSON: %v", err)
	}
	if schema["type"] != "object" {
		t.Error("expected object type")
	}
}

func TestShellToolSpec_ValidJSON(t *testing.T) {
	spec := ShellToolSpec()
	var schema map[string]any
	if err := json.Unmarshal(spec.Parameters, &schema); err != nil {
		t.Fatalf("invalid parameters JSON: %v", err)
	}
}
