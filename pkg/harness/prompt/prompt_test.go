package prompt

import (
	"strings"
	"testing"
)

func TestBuilderDefaults(t *testing.T) {
	b := NewBuilder()
	result, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	// Should contain base instructions
	if !strings.Contains(result, "coding assistant") {
		t.Error("should contain base instructions")
	}
	// Should contain default permission mode (suggest)
	if !strings.Contains(result, "describe what you plan to do") {
		t.Error("should contain suggest permission instructions")
	}
	// Should contain default sandbox (full)
	if !strings.Contains(result, "fully sandboxed") {
		t.Error("should contain full sandbox instructions")
	}
	// Should contain default collaboration (default)
	if !strings.Contains(result, "Work collaboratively") {
		t.Error("should contain default collaboration instructions")
	}
}

func TestBuilderFullAuto(t *testing.T) {
	b := NewBuilder()
	b.PermissionMode = "full-auto"
	result, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "full permission") {
		t.Error("should contain full-auto permission text")
	}
}

func TestBuilderAskEveryTime(t *testing.T) {
	b := NewBuilder()
	b.PermissionMode = "ask-every-time"
	result, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Always ask for explicit permission") {
		t.Error("should contain ask-every-time text")
	}
}

func TestBuilderPlanMode(t *testing.T) {
	b := NewBuilder()
	b.CollaborationMode = "plan"
	result, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "create a plan") {
		t.Error("should contain plan mode text")
	}
}

func TestBuilderNetworkOff(t *testing.T) {
	b := NewBuilder()
	b.SandboxMode = "network-off"
	result, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "network access disabled") {
		t.Error("should contain network-off text")
	}
}

func TestBuilderNoSandbox(t *testing.T) {
	b := NewBuilder()
	b.SandboxMode = "none"
	result, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "without sandboxing") {
		t.Error("should contain no-sandbox text")
	}
}

func TestBuilderEnvironment(t *testing.T) {
	b := NewBuilder()
	b.Environment = &EnvironmentInfo{
		WorkingDir: "/home/user/project",
		Shell:      "/bin/bash",
		Platform:   "linux/amd64",
		OSName:     "Ubuntu 22.04",
		Sandbox:    "full",
		Custom:     map[string]string{"node_version": "v20"},
	}
	result, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "<environment_context>") {
		t.Error("should contain environment XML")
	}
	if !strings.Contains(result, "<working_directory>/home/user/project</working_directory>") {
		t.Error("should contain working directory")
	}
	if !strings.Contains(result, "<shell>/bin/bash</shell>") {
		t.Error("should contain shell")
	}
	if !strings.Contains(result, "<node_version>v20</node_version>") {
		t.Error("should contain custom attrs")
	}
}

func TestBuilderAgentsMD(t *testing.T) {
	b := NewBuilder()
	b.AgentsMD = "# My Workspace\nDo things."
	result, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "<agents_md>") {
		t.Error("should wrap AGENTS.md in XML tags")
	}
	if !strings.Contains(result, "# My Workspace") {
		t.Error("should contain AGENTS.md content")
	}
}

func TestBuilderCustomSections(t *testing.T) {
	b := NewBuilder()
	b.CustomSections["personality"] = "Be witty and concise."
	result, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "<personality>") {
		t.Error("should contain custom section tags")
	}
	if !strings.Contains(result, "Be witty and concise.") {
		t.Error("should contain custom section content")
	}
}

func TestBuilderCustomBase(t *testing.T) {
	b := NewBuilder()
	b.BaseInstructions = "You are a custom assistant."
	result, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "custom assistant") {
		t.Error("should use custom base instructions")
	}
	if strings.Contains(result, "coding assistant") {
		t.Error("should not use default base instructions")
	}
}

func TestLoadTemplate(t *testing.T) {
	content, err := LoadTemplate("base_instructions.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "coding assistant") {
		t.Error("should load base instructions template")
	}
}

func TestLoadTemplateMissing(t *testing.T) {
	_, err := LoadTemplate("nonexistent.md")
	if err == nil {
		t.Error("should error for missing template")
	}
}

func TestBuilderUnknownPermission(t *testing.T) {
	b := NewBuilder()
	b.PermissionMode = "unknown-mode"
	result, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	// Falls through to else branch
	if !strings.Contains(result, "Describe your intended actions") {
		t.Error("unknown mode should use fallback text")
	}
}

func TestBuilderUnknownSandbox(t *testing.T) {
	b := NewBuilder()
	b.SandboxMode = "unknown"
	result, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Standard sandbox policy") {
		t.Error("unknown sandbox should use fallback")
	}
}
