package claude

import (
	"strings"
	"testing"

	"godex/pkg/harness"
)

func TestBuildSystemPrompt_Basic(t *testing.T) {
	turn := &harness.Turn{}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "You are Claude") {
		t.Error("expected base instructions")
	}
	// No tools, so no tool use instructions
	if strings.Contains(prompt, "## Tool Use") {
		t.Error("unexpected tool use instructions without tools")
	}
}

func TestBuildSystemPrompt_WithTools(t *testing.T) {
	turn := &harness.Turn{
		Tools: []harness.ToolSpec{{Name: "shell"}},
	}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "## Tool Use") {
		t.Error("expected tool use instructions")
	}
}

func TestBuildSystemPrompt_WithPermissions(t *testing.T) {
	tests := []struct {
		mode     string
		expected string
	}{
		{"full-auto", "full autonomous"},
		{"suggest", "prompted for approval"},
		{"ask-every-time", "wait for user approval"},
		{"", "prompted for approval"},
	}
	for _, tt := range tests {
		turn := &harness.Turn{
			Permissions: &harness.PermissionsCtx{Mode: tt.mode},
		}
		prompt, err := BuildSystemPrompt(turn)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(prompt, tt.expected) {
			t.Errorf("mode %q: expected %q in prompt", tt.mode, tt.expected)
		}
	}
}

func TestBuildSystemPrompt_WithPermissions_AllowedTools(t *testing.T) {
	turn := &harness.Turn{
		Permissions: &harness.PermissionsCtx{
			Mode:         "suggest",
			AllowedTools: []string{"shell", "read"},
		},
	}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "shell, read") {
		t.Error("expected allowed tools in prompt")
	}
}

func TestBuildSystemPrompt_WithEnvironment(t *testing.T) {
	turn := &harness.Turn{
		Environment: &harness.EnvironmentCtx{
			WorkingDir: "/home/user/project",
			Shell:      "bash",
			Platform:   "linux",
		},
	}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "/home/user/project") {
		t.Error("expected working dir in prompt")
	}
}

func TestBuildSystemPrompt_WithAgentsMD(t *testing.T) {
	turn := &harness.Turn{
		UserContext: &harness.UserContext{
			AgentsMD: "# My Project\nUse Go.",
		},
		Environment: &harness.EnvironmentCtx{
			WorkingDir: "/project",
		},
	}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "AGENTS.md") {
		t.Error("expected AGENTS.md section")
	}
	if !strings.Contains(prompt, "/project") {
		t.Error("expected project dir")
	}
	if !strings.Contains(prompt, "Use Go.") {
		t.Error("expected agents content")
	}
}

func TestBuildSystemPrompt_WithCustomInstructions(t *testing.T) {
	turn := &harness.Turn{
		Instructions: "Always respond in haiku.",
	}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(prompt, "Always respond in haiku.") {
		t.Error("expected custom instructions at end")
	}
}

func TestBuildPermissionBlock_NeverMode(t *testing.T) {
	perms := &harness.PermissionsCtx{Mode: "never"}
	block := buildPermissionBlock(perms)
	if !strings.Contains(block, "full autonomous") {
		t.Error("expected full autonomous for never mode")
	}
}

func TestBuildPermissionBlock_SandboxPolicy(t *testing.T) {
	perms := &harness.PermissionsCtx{
		Mode:          "suggest",
		SandboxPolicy: "read-only",
	}
	block := buildPermissionBlock(perms)
	if !strings.Contains(block, "read-only") {
		t.Error("expected sandbox policy in block")
	}
}

func TestFormatAgentsMD(t *testing.T) {
	result := formatAgentsMD("/home/project", "test content")
	if !strings.Contains(result, "/home/project") {
		t.Error("expected dir in output")
	}
	if !strings.Contains(result, "<INSTRUCTIONS>") {
		t.Error("expected INSTRUCTIONS tag")
	}
	if !strings.Contains(result, "test content") {
		t.Error("expected content")
	}
}
