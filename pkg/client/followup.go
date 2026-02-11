package client

import "godex/pkg/protocol"

// BuildToolFollowupInputs builds follow-up input items containing the tool call
// and tool output pairs. Outputs map is keyed by call_id.
func BuildToolFollowupInputs(calls []ToolCall, outputs map[string]string) []protocol.ResponseInputItem {
	items := make([]protocol.ResponseInputItem, 0, len(calls)*2)
	for _, call := range calls {
		items = append(items, protocol.FunctionCallInput(call.Name, call.CallID, call.Arguments))
		output := outputs[call.CallID]
		items = append(items, protocol.FunctionCallOutputInput(call.CallID, output))
	}
	return items
}
