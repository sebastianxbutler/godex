// Package prompt provides a composable prompt builder for constructing
// system instructions across all harness providers. It handles injection of
// permission instructions, environment context, AGENTS.md wrapping,
// collaboration modes, and sandbox policies.
package prompt

import (
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates/*.md
var templateFS embed.FS

// Builder constructs system prompts by composing template fragments.
type Builder struct {
	// BaseInstructions is the core identity/behavior prompt.
	BaseInstructions string

	// PermissionMode controls the tool approval policy.
	// Valid values: "full-auto", "suggest", "ask-every-time"
	PermissionMode string

	// SandboxMode controls execution sandbox.
	// Valid values: "full", "network-off", "none"
	SandboxMode string

	// CollaborationMode controls the interaction style.
	// Valid values: "default", "plan"
	CollaborationMode string

	// Environment provides execution context (cwd, shell, platform).
	Environment *EnvironmentInfo

	// AgentsMD is the content of the user's AGENTS.md file.
	AgentsMD string

	// CustomSections are additional named sections appended to the prompt.
	CustomSections map[string]string
}

// EnvironmentInfo holds environment context for prompt injection.
type EnvironmentInfo struct {
	WorkingDir string
	Shell      string
	Platform   string
	OSName     string
	Sandbox    string
	Custom     map[string]string
}

// NewBuilder creates a Builder with sensible defaults.
func NewBuilder() *Builder {
	return &Builder{
		PermissionMode:    "suggest",
		SandboxMode:       "full",
		CollaborationMode: "default",
		CustomSections:    make(map[string]string),
	}
}

// Build assembles the complete system prompt from all configured sections.
func (b *Builder) Build() (string, error) {
	var parts []string

	// 1. Base instructions
	base := b.BaseInstructions
	if base == "" {
		content, err := loadTemplate("base_instructions.md")
		if err != nil {
			return "", fmt.Errorf("prompt: load base instructions: %w", err)
		}
		base = content
	}
	parts = append(parts, base)

	// 2. Permission instructions
	permTpl, err := loadTemplate("permissions.md")
	if err == nil && permTpl != "" {
		rendered, err := renderTemplate("permissions", permTpl, map[string]string{
			"Mode": b.PermissionMode,
		})
		if err != nil {
			return "", fmt.Errorf("prompt: render permissions: %w", err)
		}
		parts = append(parts, rendered)
	}

	// 3. Sandbox mode
	sandboxTpl, err := loadTemplate("sandbox.md")
	if err == nil && sandboxTpl != "" {
		rendered, err := renderTemplate("sandbox", sandboxTpl, map[string]string{
			"Mode": b.SandboxMode,
		})
		if err != nil {
			return "", fmt.Errorf("prompt: render sandbox: %w", err)
		}
		parts = append(parts, rendered)
	}

	// 4. Collaboration mode
	collabTpl, err := loadTemplate("collaboration.md")
	if err == nil && collabTpl != "" {
		rendered, err := renderTemplate("collaboration", collabTpl, map[string]string{
			"Mode": b.CollaborationMode,
		})
		if err != nil {
			return "", fmt.Errorf("prompt: render collaboration: %w", err)
		}
		parts = append(parts, rendered)
	}

	// 5. Environment context
	if b.Environment != nil {
		envXML := b.buildEnvironmentContext()
		parts = append(parts, envXML)
	}

	// 6. AGENTS.md
	if b.AgentsMD != "" {
		parts = append(parts, fmt.Sprintf("<agents_md>\n%s\n</agents_md>", b.AgentsMD))
	}

	// 7. Custom sections
	for name, content := range b.CustomSections {
		parts = append(parts, fmt.Sprintf("<%s>\n%s\n</%s>", name, content, name))
	}

	return strings.Join(parts, "\n\n"), nil
}

// buildEnvironmentContext generates the XML environment context block.
func (b *Builder) buildEnvironmentContext() string {
	env := b.Environment
	var lines []string
	lines = append(lines, "<environment_context>")
	if env.WorkingDir != "" {
		lines = append(lines, fmt.Sprintf("  <working_directory>%s</working_directory>", env.WorkingDir))
	}
	if env.Shell != "" {
		lines = append(lines, fmt.Sprintf("  <shell>%s</shell>", env.Shell))
	}
	if env.Platform != "" {
		lines = append(lines, fmt.Sprintf("  <platform>%s</platform>", env.Platform))
	}
	if env.OSName != "" {
		lines = append(lines, fmt.Sprintf("  <os>%s</os>", env.OSName))
	}
	if env.Sandbox != "" {
		lines = append(lines, fmt.Sprintf("  <sandbox>%s</sandbox>", env.Sandbox))
	}
	for k, v := range env.Custom {
		lines = append(lines, fmt.Sprintf("  <%s>%s</%s>", k, v, k))
	}
	lines = append(lines, "</environment_context>")
	return strings.Join(lines, "\n")
}

func loadTemplate(name string) (string, error) {
	data, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func renderTemplate(name, tplStr string, data any) (string, error) {
	tpl, err := template.New(name).Parse(tplStr)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	if err := tpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// LoadTemplate loads a named template from the embedded templates directory.
// This is useful for provider-specific harnesses that want to use shared
// templates directly.
func LoadTemplate(name string) (string, error) {
	return loadTemplate(name)
}
