package openai

import (
	"fmt"
	"strings"

	"godex/pkg/harness"
	"godex/pkg/harness/prompt"
)

// BuildSystemPrompt constructs a system prompt for generic OpenAI-compatible
// models. This is intentionally simpler than the Codex or Claude prompts since
// it needs to work with any Chat Completions provider.
func BuildSystemPrompt(turn *harness.Turn) (string, error) {
	var parts []string

	// 1. Base identity instructions
	parts = append(parts, baseInstructions)

	// 2. Tool use guidance (only if tools are available)
	if len(turn.Tools) > 0 {
		parts = append(parts, toolUseInstructions)
	}

	// 3. Permission context
	if turn.Permissions != nil {
		perm := buildPermissionBlock(turn.Permissions)
		if perm != "" {
			parts = append(parts, perm)
		}
	}

	// 4. Environment context (reuse shared prompt builder)
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

	// 5. AGENTS.md injection
	if turn.UserContext != nil && turn.UserContext.AgentsMD != "" {
		dir := "."
		if turn.Environment != nil && turn.Environment.WorkingDir != "" {
			dir = turn.Environment.WorkingDir
		}
		parts = append(parts, formatAgentsMD(dir, turn.UserContext.AgentsMD))
	}

	// 6. Custom instructions (appended last, highest priority)
	if turn.Instructions != "" {
		parts = append(parts, turn.Instructions)
	}

	return strings.Join(parts, "\n\n"), nil
}

const baseInstructions = `You are a helpful AI coding assistant. You are an expert software engineer.

## Guidelines

- Be direct and concise. Avoid unnecessary filler.
- When editing code, make minimal, targeted changes.
- Read files before editing to understand context.
- Validate changes by running tests or build commands when available.
- If unsure about something, say so rather than guessing.
- Use available tools to accomplish tasks directly.`

const toolUseInstructions = `## Tool Use

You have access to tools for interacting with the system. When using tools:
- Execute tools as needed to accomplish the task.
- Chain tool calls efficiently for multi-step work.
- If a tool call fails, read the error and adjust.
- For file edits, read the file first.`

// buildPermissionBlock creates permission instructions.
func buildPermissionBlock(perms *harness.PermissionsCtx) string {
	var lines []string
	lines = append(lines, "## Permissions")

	switch perms.Mode {
	case "full-auto", "never":
		lines = append(lines, "You have full autonomous execution permissions.")
	case "suggest":
		lines = append(lines, "Execute tools as needed. Destructive operations require user approval.")
	case "ask-every-time":
		lines = append(lines, "Describe your plan and wait for approval before executing tools.")
	default:
		lines = append(lines, "Execute tools as needed. Destructive operations require user approval.")
	}

	if len(perms.AllowedTools) > 0 {
		lines = append(lines, fmt.Sprintf("Auto-approved tools: %s", strings.Join(perms.AllowedTools, ", ")))
	}

	return strings.Join(lines, "\n")
}

// formatAgentsMD wraps AGENTS.md content.
func formatAgentsMD(dir, content string) string {
	return fmt.Sprintf("# Project Instructions (AGENTS.md) for %s\n\n<INSTRUCTIONS>\n%s\n</INSTRUCTIONS>", dir, content)
}
