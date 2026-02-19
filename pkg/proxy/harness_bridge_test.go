package proxy

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"godex/pkg/harness"
)

func TestRepairEmptyExecArgs_BacktickCommand(t *testing.T) {
	turn := &harness.Turn{
		Messages: []harness.Message{
			{Role: "user", Content: "Can you run `ls /home/cmd/clawd` now?"},
		},
	}
	args, ok := repairEmptyExecArgs(turn)
	if !ok {
		t.Fatalf("expected repaired args")
	}
	if args != `{"command":"ls /home/cmd/clawd"}` {
		t.Fatalf("unexpected args: %s", args)
	}
}

func TestRepairEmptyExecArgs_QuotedCommand(t *testing.T) {
	turn := &harness.Turn{
		Messages: []harness.Message{
			{Role: "user", Content: `Please run the "pwd" command.`},
		},
	}
	args, ok := repairEmptyExecArgs(turn)
	if !ok {
		t.Fatalf("expected repaired args")
	}
	if args != `{"command":"pwd"}` {
		t.Fatalf("unexpected args: %s", args)
	}
}

func TestRepairEmptyExecArgs_NoSignal(t *testing.T) {
	turn := &harness.Turn{
		Messages: []harness.Message{
			{Role: "user", Content: "Hello there"},
		},
	}
	if _, ok := repairEmptyExecArgs(turn); ok {
		t.Fatalf("expected no repaired args")
	}
}

func TestNeedsExecArgRepair(t *testing.T) {
	cases := []struct {
		args string
		want bool
	}{
		{"{}", true},
		{"", true},
		{`{"workdir":"/tmp"}`, true},
		{`{"command":""}`, true},
		{`{"command":"ls"}`, false},
	}
	for _, tc := range cases {
		if got := needsExecArgRepair(tc.args); got != tc.want {
			t.Fatalf("needsExecArgRepair(%q)=%v want %v", tc.args, got, tc.want)
		}
	}
}

func TestSanitizeExecArgs(t *testing.T) {
	got := sanitizeExecArgs(`{"ask":"off","command":"ls /tmp","workdir":"/home/cmd/clawd","yieldMs":1000}`)
	want := `{"command":"ls /tmp","workdir":"/home/cmd/clawd","yieldMs":1000}`
	if got != want {
		t.Fatalf("sanitizeExecArgs()=%s want %s", got, want)
	}
}

func TestNormalizeExecToolCall_RepairsEmptyArgs(t *testing.T) {
	turn := &harness.Turn{
		Messages: []harness.Message{
			{Role: "user", Content: `Please run the "ls /home/cmd/clawd" command.`},
		},
	}
	tc := &harness.ToolCallEvent{
		CallID:    "call_1",
		Name:      "exec",
		Arguments: "{}",
	}
	normalizeExecToolCall(turn, tc)
	if tc.Arguments != `{"command":"ls /home/cmd/clawd"}` {
		t.Fatalf("unexpected repaired args: %s", tc.Arguments)
	}
}

func TestHarnessResponsesStream_FunctionCallArgsDoneHasArguments(t *testing.T) {
	s := &Server{cache: NewCache(time.Hour)}
	h := harness.NewMock(harness.MockConfig{
		Responses: [][]harness.Event{
			{
				harness.NewToolCallEvent("call_1", "read", `{"path":"/tmp/a.txt"}`),
				harness.NewDoneEvent(),
			},
		},
	})
	turn := &harness.Turn{Model: "gpt-5.3-codex"}
	rr := httptest.NewRecorder()

	err := s.harnessResponsesStream(
		context.Background(),
		rr,
		rr,
		h,
		turn,
		"gpt-5.3-codex",
		nil,
		time.Now(),
		nil,
		"",
		"req_test",
	)
	if err != nil {
		t.Fatalf("harnessResponsesStream error: %v", err)
	}

	var argsDone map[string]any
	for _, chunk := range strings.Split(rr.Body.String(), "\n\n") {
		line := strings.TrimSpace(chunk)
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatalf("invalid SSE JSON: %v", err)
		}
		if ev["type"] == "response.function_call_arguments.done" {
			argsDone = ev
			break
		}
	}
	if argsDone == nil {
		t.Fatalf("missing response.function_call_arguments.done event")
	}
	if argsDone["arguments"] != `{"path":"/tmp/a.txt"}` {
		t.Fatalf("arguments = %#v, want tool-call args", argsDone["arguments"])
	}
}
