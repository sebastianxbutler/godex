package proxy

import (
	"testing"

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
