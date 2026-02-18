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

// Marker tags used to identify replaceable sections in the base prompt.
// When Codex releases a new base prompt, just keep these markers around
// the tool-specific content and the rest is handled automatically.
const (
	markerPrefix = "<!-- "
	markerSuffix = " -->"
)

// sectionReplacements maps marker names to their proxy-mode replacements.
// Empty string means the section is removed entirely.
// Nil map value means use the default (keep original content).
var defaultReplacements = map[string]string{
	"CAPABILITIES":      "- Receive user prompts and respond using the provided tools.\n- Communicate with the user by streaming responses.",
	"TOOL_USAGE":        "", // removed — caller provides their own tool instructions
	"TOOL_HINTS":        "", // removed — not applicable to caller's tools
	"TOOL_PRESENTATION": "", // removed — caller handles presentation
	"TOOL_GUIDELINES":   "", // removed — caller provides tool context
}

// BuildProxySystemPrompt builds a system prompt for proxy mode: keeps the Codex
// base prompt (personality, planning, task execution, formatting) but replaces
// marked tool-specific sections with caller-appropriate content.
//
// Sections are marked in base_instructions.md with HTML comments:
//
//	<!-- SECTION_NAME_START -->
//	...content...
//	<!-- SECTION_NAME_END -->
//
// The replacements map controls what each section becomes. When updating to a
// new Codex prompt version, just preserve the markers and everything works.
func BuildProxySystemPrompt(turn *harness.Turn) (string, error) {
	base, err := loadTemplate("base_instructions.md")
	if err != nil {
		return "", fmt.Errorf("codex prompt: load base instructions: %w", err)
	}

	// Apply section replacements
	base = replaceMarkedSections(base, defaultReplacements)

	var parts []string
	parts = append(parts, base)

	// Append caller's instructions (these contain the caller's tool context)
	if turn.Instructions != "" {
		parts = append(parts, turn.Instructions)
	}

	return strings.Join(parts, "\n\n"), nil
}

// replaceMarkedSections finds <!-- NAME_START --> ... <!-- NAME_END --> blocks
// and replaces them with the provided content. Empty replacement removes the
// section entirely (including markers).
func replaceMarkedSections(text string, replacements map[string]string) string {
	for name, replacement := range replacements {
		startTag := markerPrefix + name + "_START" + markerSuffix
		endTag := markerPrefix + name + "_END" + markerSuffix

		startIdx := strings.Index(text, startTag)
		endIdx := strings.Index(text, endTag)
		if startIdx < 0 || endIdx < 0 || endIdx <= startIdx {
			continue
		}
		endIdx += len(endTag)

		if replacement == "" {
			// Remove the section and surrounding blank lines
			text = text[:startIdx] + text[endIdx:]
		} else {
			text = text[:startIdx] + replacement + text[endIdx:]
		}
	}

	// Clean up multiple consecutive blank lines left by removals
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}

	return text
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
