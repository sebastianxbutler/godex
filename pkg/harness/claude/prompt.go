package claude

import (
	"fmt"
	"strings"

	"godex/pkg/harness"
	"godex/pkg/harness/prompt"
)

// BuildSystemPrompt constructs the full Claude system prompt from a Turn.
// Claude uses the system parameter natively (not a user message), so this
// returns a single string to be set as the system block.
func BuildSystemPrompt(turn *harness.Turn) (string, error) {
	var parts []string

	// 1. Base identity and coding instructions for Claude
	parts = append(parts, baseInstructions)

	// 2. Tool use instructions
	if len(turn.Tools) > 0 {
		parts = append(parts, toolUseInstructions)
	}

	// 3. Permission/sandbox context
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

	// 6. Custom instructions override (appended last)
	if turn.Instructions != "" {
		parts = append(parts, turn.Instructions)
	}

	return strings.Join(parts, "\n\n"), nil
}

const baseInstructions = `You are Claude, an AI assistant made by Anthropic. You are an expert software engineer helping with coding tasks.

## Guidelines

- Be direct and concise. Avoid unnecessary preamble.
- When editing code, make minimal, targeted changes. Don't rewrite entire files unnecessarily.
- Always read files before editing them to understand the current state.
- Validate your changes by running tests or build commands when available.
- If you're unsure about something, say so rather than guessing.
- Use the available tools to accomplish tasks. Prefer tool use over generating code blocks for the user to copy-paste.
- When running shell commands, prefer non-interactive flags and handle errors gracefully.
- Write clear commit messages that describe what changed and why.`

const toolUseInstructions = `## Tool Use

You have access to tools that let you interact with the user's system. Use them to:
- Read and write files
- Execute shell commands
- Search codebases

When using tools:
- Verify your changes work by running relevant tests or builds after editing.
- Chain tool calls efficiently — don't ask permission for each step of a multi-step task.
- If a tool call fails, read the error carefully and adjust your approach.
- For file edits, always read the file first to understand context.`

// buildPermissionBlock creates the permission instructions for Claude.
func buildPermissionBlock(perms *harness.PermissionsCtx) string {
	var lines []string
	lines = append(lines, "## Permissions")

	switch perms.Mode {
	case "full-auto", "never":
		lines = append(lines, "You have full autonomous execution permissions. Execute tools without asking for approval.")
	case "suggest":
		lines = append(lines, "Execute tools as needed. The user will be prompted for approval on potentially destructive operations.")
	case "ask-every-time":
		lines = append(lines, "Always describe what you plan to do and wait for user approval before executing any tool.")
	default:
		lines = append(lines, "Execute tools as needed. The user will be prompted for approval on potentially destructive operations.")
	}

	if len(perms.AllowedTools) > 0 {
		lines = append(lines, fmt.Sprintf("Auto-approved tools: %s", strings.Join(perms.AllowedTools, ", ")))
	}

	if perms.SandboxPolicy != "" {
		lines = append(lines, fmt.Sprintf("Sandbox policy: %s", perms.SandboxPolicy))
	}

	return strings.Join(lines, "\n")
}

// formatAgentsMD wraps AGENTS.md content for Claude.
func formatAgentsMD(dir, content string) string {
	return fmt.Sprintf("# Project Instructions (AGENTS.md) for %s\n\n<INSTRUCTIONS>\n%s\n</INSTRUCTIONS>", dir, content)
}
