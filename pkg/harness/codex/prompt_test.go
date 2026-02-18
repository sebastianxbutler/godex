package codex

import (
	"strings"
	"testing"

	"godex/pkg/harness"
)

func TestBuildSystemPrompt_BaseInstructions(t *testing.T) {
	turn := &harness.Turn{Model: "gpt-5.2-codex"}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "You are a coding agent running in the Codex CLI") {
		t.Error("expected base instructions to contain Codex identity")
	}
}

func TestBuildSystemPrompt_SandboxMode(t *testing.T) {
	tests := []struct {
		policy   string
		contains string
	}{
		{"danger-full-access", "danger-full-access"},
		{"full-access", "danger-full-access"},
		{"read-only", "read-only"},
		{"workspace-write", "workspace-write"},
		{"", "workspace-write"},
	}
	for _, tt := range tests {
		t.Run(tt.policy, func(t *testing.T) {
			turn := &harness.Turn{
				Model:       "test",
				Permissions: &harness.PermissionsCtx{SandboxPolicy: tt.policy, Mode: "suggest"},
			}
			prompt, err := BuildSystemPrompt(turn)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(prompt, tt.contains) {
				t.Errorf("expected sandbox mode %q in prompt for policy %q", tt.contains, tt.policy)
			}
		})
	}
}

func TestBuildSystemPrompt_ApprovalPolicy(t *testing.T) {
	tests := []struct {
		mode     string
		contains string
	}{
		{"never", "never"},
		{"full-auto", "never"},
		{"on-failure", "on-failure"},
		{"on-request", "on-request"},
		{"suggest", "on-request"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			turn := &harness.Turn{
				Model:       "test",
				Permissions: &harness.PermissionsCtx{Mode: tt.mode, SandboxPolicy: "read-only"},
			}
			prompt, err := BuildSystemPrompt(turn)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(prompt, tt.contains) {
				t.Errorf("expected approval %q in prompt for mode %q", tt.contains, tt.mode)
			}
		})
	}
}

func TestBuildSystemPrompt_CollaborationMode(t *testing.T) {
	tests := []struct {
		mode     string
		contains string
	}{
		{"default", "Default mode"},
		{"plan", "Plan Mode"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			turn := &harness.Turn{
				Model:       "test",
				UserContext: &harness.UserContext{Collaboration: tt.mode},
			}
			prompt, err := BuildSystemPrompt(turn)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(prompt, tt.contains) {
				t.Errorf("expected %q in prompt for collab mode %q", tt.contains, tt.mode)
			}
		})
	}
}

func TestBuildSystemPrompt_EnvironmentContext(t *testing.T) {
	turn := &harness.Turn{
		Model: "test",
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
	if !strings.Contains(prompt, "<environment_context>") {
		t.Error("expected environment context XML")
	}
	if !strings.Contains(prompt, "/home/user/project") {
		t.Error("expected working directory in environment context")
	}
	if !strings.Contains(prompt, "bash") {
		t.Error("expected shell in environment context")
	}
}

func TestBuildSystemPrompt_AgentsMD(t *testing.T) {
	turn := &harness.Turn{
		Model: "test",
		Environment: &harness.EnvironmentCtx{
			WorkingDir: "/my/project",
		},
		UserContext: &harness.UserContext{
			AgentsMD: "Use tabs for indentation.\nRun tests with pytest.",
		},
	}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "# AGENTS.md instructions for /my/project") {
		t.Error("expected AGENTS.md header with directory")
	}
	if !strings.Contains(prompt, "<INSTRUCTIONS>") {
		t.Error("expected INSTRUCTIONS tags")
	}
	if !strings.Contains(prompt, "Use tabs for indentation") {
		t.Error("expected AGENTS.md content")
	}
}

func TestBuildSystemPrompt_CustomInstructions(t *testing.T) {
	turn := &harness.Turn{
		Model:        "test",
		Instructions: "Always use TypeScript.",
	}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Always use TypeScript.") {
		t.Error("expected custom instructions in prompt")
	}
}

func TestBuildSystemPrompt_NetworkDisabled(t *testing.T) {
	turn := &harness.Turn{
		Model: "test",
		Environment: &harness.EnvironmentCtx{
			Sandbox: "network-off",
		},
		Permissions: &harness.PermissionsCtx{
			SandboxPolicy: "workspace-write",
			Mode:          "suggest",
		},
	}
	prompt, err := BuildSystemPrompt(turn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "disabled") {
		t.Error("expected network access to be disabled")
	}
}

func TestFormatAgentsMD(t *testing.T) {
	result := formatAgentsMD("/project", "Do this")
	if !strings.Contains(result, "# AGENTS.md instructions for /project") {
		t.Error("expected header")
	}
	if !strings.Contains(result, "<INSTRUCTIONS>\nDo this\n</INSTRUCTIONS>") {
		t.Error("expected instructions tags wrapping content")
	}
}
