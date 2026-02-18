// Package codex implements the Codex harness for the Responses API.
package codex

import (
	"embed"
	"fmt"
	"strings"
	"text/template"

	"godex/pkg/harness"
	"godex/pkg/harness/prompt"
)

//go:embed templates/*.md
var templateFS embed.FS

// loadTemplate reads an embedded template by name.
func loadTemplate(name string) (string, error) {
	data, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// renderTemplate renders a Go text/template string with the given data.
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

// BuildSystemPrompt constructs the full Codex system prompt from a Turn.
// It layers: base instructions, sandbox mode, approval policy, collaboration
// mode, environment context, AGENTS.md, and any custom instructions.
func BuildSystemPrompt(turn *harness.Turn) (string, error) {
	var parts []string

	// 1. Base instructions (the full Codex default.md)
	base, err := loadTemplate("base_instructions.md")
	if err != nil {
		return "", fmt.Errorf("codex prompt: load base instructions: %w", err)
	}
	parts = append(parts, base)

	// 2. Sandbox mode
	if turn.Permissions != nil {
		sandboxTpl, err := sandboxTemplate(turn.Permissions.SandboxPolicy)
		if err == nil && sandboxTpl != "" {
			networkAccess := "enabled"
			if turn.Environment != nil && turn.Environment.Sandbox == "network-off" {
				networkAccess = "disabled"
			}
			rendered, err := renderTemplate("sandbox", sandboxTpl, map[string]string{
				"NetworkAccess": networkAccess,
			})
			if err != nil {
				return "", fmt.Errorf("codex prompt: render sandbox: %w", err)
			}
			parts = append(parts, rendered)
		}
	}

	// 3. Approval policy
	if turn.Permissions != nil {
		approvalTpl, err := approvalTemplate(turn.Permissions.Mode)
		if err == nil && approvalTpl != "" {
			parts = append(parts, approvalTpl)
		}
	}

	// 4. Collaboration mode
	if turn.UserContext != nil && turn.UserContext.Collaboration != "" {
		collabTpl, err := collaborationTemplate(turn.UserContext.Collaboration)
		if err == nil && collabTpl != "" {
			parts = append(parts, collabTpl)
		}
	}

	// 5. Environment context XML (using the shared prompt builder)
	if turn.Environment != nil {
		pb := &prompt.Builder{
			Environment: &prompt.EnvironmentInfo{
				WorkingDir: turn.Environment.WorkingDir,
				Shell:      turn.Environment.Shell,
				Platform:   turn.Environment.Platform,
				Sandbox:    turn.Environment.Sandbox,
				Custom:     turn.Environment.CustomAttrs,
			},
		}
		envXML := pb.BuildEnvironmentContext()
		if envXML != "" {
			parts = append(parts, envXML)
		}
	}

	// 6. AGENTS.md wrapping
	if turn.UserContext != nil && turn.UserContext.AgentsMD != "" {
		dir := "."
		if turn.Environment != nil && turn.Environment.WorkingDir != "" {
			dir = turn.Environment.WorkingDir
		}
		parts = append(parts, formatAgentsMD(dir, turn.UserContext.AgentsMD))
	}

	// 7. Custom instructions override (appended last)
	if turn.Instructions != "" {
		parts = append(parts, turn.Instructions)
	}

	return strings.Join(parts, "\n\n"), nil
}

// BuildProxySystemPrompt builds a system prompt for proxy mode: keeps the Codex
// base prompt (personality, planning, task execution, formatting) but replaces
// tool-specific sections (Shell commands, apply_patch, update_plan) with the
// caller's instructions.
func BuildProxySystemPrompt(turn *harness.Turn) (string, error) {
	base, err := loadTemplate("base_instructions.md")
	if err != nil {
		return "", fmt.Errorf("codex prompt: load base instructions: %w", err)
	}

	// Strip tool-specific sections from the base prompt:
	// 1. "# Tool Guidelines" section at the end
	// 2. apply_patch usage instructions embedded in "## Task execution"
	base = stripToolSections(base)

	var parts []string
	parts = append(parts, base)

	// Append caller's instructions (these contain the caller's tool context)
	if turn.Instructions != "" {
		parts = append(parts, turn.Instructions)
	}

	return strings.Join(parts, "\n\n"), nil
}

// stripToolSections removes Codex-specific tool references from the base prompt,
// keeping the core personality, planning, and formatting guidance intact.
func stripToolSections(prompt string) string {
	// Remove "# Tool Guidelines" section and everything after it
	if idx := strings.Index(prompt, "\n# Tool Guidelines"); idx >= 0 {
		prompt = strings.TrimRight(prompt[:idx], "\n ")
	}

	// Remove apply_patch usage line from Task execution section
	lines := strings.Split(prompt, "\n")
	var filtered []string
	for _, line := range lines {
		// Skip lines that reference apply_patch tool specifics
		if strings.Contains(line, "apply_patch") {
			continue
		}
		filtered = append(filtered, line)
	}

	return strings.Join(filtered, "\n")
}

// formatAgentsMD wraps AGENTS.md content in the Codex-standard format.
func formatAgentsMD(dir, content string) string {
	return fmt.Sprintf("# AGENTS.md instructions for %s\n\n<INSTRUCTIONS>\n%s\n</INSTRUCTIONS>", dir, content)
}

// sandboxTemplate returns the template for a given sandbox policy.
func sandboxTemplate(policy string) (string, error) {
	switch policy {
	case "full-access", "danger-full-access":
		return loadTemplate("sandbox_full_access.md")
	case "read-only":
		return loadTemplate("sandbox_read_only.md")
	case "workspace-write", "":
		return loadTemplate("sandbox_workspace_write.md")
	default:
		return loadTemplate("sandbox_workspace_write.md")
	}
}

// approvalTemplate returns the template for a given approval mode.
func approvalTemplate(mode string) (string, error) {
	switch mode {
	case "never", "full-auto":
		return loadTemplate("approval_never.md")
	case "on-failure":
		return loadTemplate("approval_on_failure.md")
	case "on-request", "suggest", "":
		return loadTemplate("approval_on_request.md")
	default:
		return loadTemplate("approval_on_request.md")
	}
}

// collaborationTemplate returns the template for a given collaboration mode.
func collaborationTemplate(mode string) (string, error) {
	switch mode {
	case "plan":
		return loadTemplate("collaboration_plan.md")
	case "default", "":
		return loadTemplate("collaboration_default.md")
	default:
		return loadTemplate("collaboration_default.md")
	}
}
