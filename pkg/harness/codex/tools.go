package codex

import (
	"godex/pkg/harness"
	"godex/pkg/protocol"
)

// ApplyPatchLarkGrammar is the Lark grammar defining the apply_patch tool format.
const ApplyPatchLarkGrammar = `start: begin_patch hunk+ end_patch
begin_patch: "*** Begin Patch" LF
end_patch: "*** End Patch" LF?

hunk: add_hunk | delete_hunk | update_hunk
add_hunk: "*** Add File: " filename LF add_line+
delete_hunk: "*** Delete File: " filename LF
update_hunk: "*** Update File: " filename LF change_move? change?

filename: /(.+)/
add_line: "+" /(.*)/ LF -> line

change_move: "*** Move to: " filename LF
change: (change_context | change_line)+ eof_line?
change_context: ("@@" | "@@ " /(.+)/) LF
change_line: ("+" | "-" | " ") /(.*)/ LF
eof_line: "*** End of File" LF

%import common.LF`

// ApplyPatchToolSpec returns the Codex apply_patch tool in protocol wire format.
// This is a freeform tool (custom format with Lark grammar).
func ApplyPatchToolSpec() protocol.ToolSpec {
	return protocol.ToolSpec{
		Type:        "function",
		Name:        "apply_patch",
		Description: "Apply a patch to files. Use the Codex patch format.",
		Format: &protocol.CustomFormat{
			Type:       "freeform",
			Syntax:     "lark",
			Definition: ApplyPatchLarkGrammar,
		},
	}
}

// UpdatePlanToolSpec returns the Codex update_plan tool spec.
func UpdatePlanToolSpec() protocol.ToolSpec {
	return protocol.ToolSpec{
		Type:        "function",
		Name:        "update_plan",
		Description: "Update the plan with step-by-step progress. Use to track task progress.",
		Parameters: mustJSON(`{
			"type": "object",
			"properties": {
				"steps": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"title": {"type": "string"},
							"status": {"type": "string", "enum": ["pending", "in_progress", "completed"]}
						},
						"required": ["title", "status"]
					}
				},
				"explanation": {"type": "string"}
			},
			"required": ["steps"]
		}`),
	}
}

// ShellToolSpec returns the shell/container.exec tool spec.
func ShellToolSpec() protocol.ToolSpec {
	return protocol.ToolSpec{
		Type:        "function",
		Name:        "shell",
		Description: "Execute a shell command in the sandbox environment.",
		Parameters: mustJSON(`{
			"type": "object",
			"properties": {
				"command": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Command and arguments to execute"
				},
				"sandbox_permissions": {
					"type": "string",
					"enum": ["sandbox", "require_escalated"]
				},
				"justification": {
					"type": "string",
					"description": "Reason for requiring escalated permissions"
				}
			},
			"required": ["command"]
		}`),
	}
}

// DefaultTools returns the standard Codex tool set.
func DefaultTools() []protocol.ToolSpec {
	return []protocol.ToolSpec{
		ApplyPatchToolSpec(),
		UpdatePlanToolSpec(),
		ShellToolSpec(),
	}
}

// DefaultHarnessTools returns the standard Codex tool set as harness.ToolSpec.
func DefaultHarnessTools() []harness.ToolSpec {
	return []harness.ToolSpec{
		{
			Name:        "apply_patch",
			Description: "Apply a patch to files using the Codex patch format.",
		},
		{
			Name:        "update_plan",
			Description: "Update the plan with step-by-step progress.",
		},
		{
			Name:        "shell",
			Description: "Execute a shell command in the sandbox environment.",
		},
	}
}

func mustJSON(s string) []byte {
	return []byte(s)
}
