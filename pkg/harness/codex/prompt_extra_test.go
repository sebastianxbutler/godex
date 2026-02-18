package codex

import (
	"strings"
	"testing"

	"godex/pkg/harness"
)

func TestBuildSystemPrompt_CollaborationUnknown(t *testing.T) {
	turn := &harness.Turn{
		UserContext: &harness.UserContext{Collaboration: "unknown-mode"},
	}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	// Unknown collab mode should fall through to default
	if !strings.Contains(prompt, "Default mode") {
		t.Error("unknown collaboration mode should use default template")
	}
}

func TestBuildSystemPrompt_SandboxUnknown(t *testing.T) {
	turn := &harness.Turn{
		Permissions: &harness.PermissionsCtx{SandboxPolicy: "unknown-policy", Mode: "suggest"},
	}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	// Should fall through to workspace-write default
	if !strings.Contains(prompt, "workspace-write") {
		t.Error("unknown sandbox should use workspace-write default")
	}
}

func TestBuildSystemPrompt_ApprovalUnknown(t *testing.T) {
	turn := &harness.Turn{
		Permissions: &harness.PermissionsCtx{Mode: "unknown-mode", SandboxPolicy: "read-only"},
	}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	// Should fall through to on-request default
	if !strings.Contains(prompt, "on-request") {
		t.Error("unknown approval mode should use on-request default")
	}
}

func TestBuildSystemPrompt_AgentsMD_DefaultDir(t *testing.T) {
	turn := &harness.Turn{
		UserContext: &harness.UserContext{AgentsMD: "instructions here"},
		// No Environment set -> dir defaults to "."
	}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "AGENTS.md instructions for .") {
		t.Error("expected default dir '.'")
	}
}

func TestBuildSystemPrompt_EmptyCollaboration(t *testing.T) {
	turn := &harness.Turn{
		UserContext: &harness.UserContext{Collaboration: ""},
	}
	// Empty string collaboration should not add collab section (skipped by the if check)
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	// Just verify it doesn't error - empty string is skipped
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
}

func TestLoadTemplate_Invalid(t *testing.T) {
	_, err := loadTemplate("nonexistent_template.md")
	if err == nil {
		t.Error("expected error for missing template")
	}
}

func TestRenderTemplate_Error(t *testing.T) {
	_, err := renderTemplate("bad", "{{.Missing}", nil)
	if err == nil {
		t.Error("expected template parse error")
	}
}
