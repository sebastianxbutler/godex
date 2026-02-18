package openai

import (
	"strings"
	"testing"

	"godex/pkg/harness"
)

func TestBuildSystemPrompt_Basic(t *testing.T) {
	turn := &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "hi"}},
	}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "helpful AI coding assistant") {
		t.Error("expected base instructions in prompt")
	}
	// No tools, so no tool instructions
	if strings.Contains(prompt, "Tool Use") {
		t.Error("did not expect tool use instructions without tools")
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
	if !strings.Contains(prompt, "Tool Use") {
		t.Error("expected tool use instructions")
	}
}

func TestBuildSystemPrompt_WithPermissions(t *testing.T) {
	tests := []struct {
		mode     string
		contains string
	}{
		{"full-auto", "full autonomous"},
		{"suggest", "Destructive operations"},
		{"ask-every-time", "wait for approval"},
		{"unknown", "Destructive operations"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			turn := &harness.Turn{
				Permissions: &harness.PermissionsCtx{Mode: tt.mode},
			}
			prompt, err := BuildSystemPrompt(turn)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(prompt, tt.contains) {
				t.Errorf("expected %q in prompt for mode %s", tt.contains, tt.mode)
			}
		})
	}
}

func TestBuildSystemPrompt_WithAllowedTools(t *testing.T) {
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
	if !strings.Contains(prompt, "bash") {
		t.Error("expected shell in prompt")
	}
}

func TestBuildSystemPrompt_WithAgentsMD(t *testing.T) {
	turn := &harness.Turn{
		UserContext: &harness.UserContext{
			AgentsMD: "Use tabs not spaces.",
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
		t.Error("expected AGENTS.md header")
	}
	if !strings.Contains(prompt, "Use tabs not spaces") {
		t.Error("expected AGENTS.md content")
	}
	if !strings.Contains(prompt, "/project") {
		t.Error("expected directory in AGENTS.md header")
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
	if !strings.HasSuffix(strings.TrimSpace(prompt), "Always respond in haiku.") {
		t.Error("expected custom instructions at end of prompt")
	}
}

func TestBuildSystemPrompt_NeverMode(t *testing.T) {
	turn := &harness.Turn{
		Permissions: &harness.PermissionsCtx{Mode: "never"},
	}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "full autonomous") {
		t.Error("expected 'never' mode to map to full autonomous")
	}
}
