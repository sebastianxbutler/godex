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
