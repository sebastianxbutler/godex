package protocol

import "encoding/json"

type ResponsesRequest struct {
	Model             string              `json:"model"`
	Instructions      string              `json:"instructions,omitempty"`
	Input             []ResponseInputItem `json:"input,omitempty"`
	Tools             []ToolSpec          `json:"tools,omitempty"`
	ToolChoice        string              `json:"tool_choice,omitempty"`
	ParallelToolCalls bool                `json:"parallel_tool_calls,omitempty"`
	Reasoning         *Reasoning          `json:"reasoning,omitempty"`
	Store             bool                `json:"store"`
	Stream            bool                `json:"stream"`
	Include           []string            `json:"include,omitempty"`
	PromptCacheKey    string              `json:"prompt_cache_key,omitempty"`
	Text              *TextControls       `json:"text,omitempty"`
}

type Reasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type TextControls struct {
	Verbosity string      `json:"verbosity,omitempty"`
	Format    *TextFormat `json:"format,omitempty"`
}

type TextFormat struct {
	Type   string          `json:"type"`
	Strict bool            `json:"strict,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
}

type ResponseInputItem struct {
	Type      string             `json:"type"`
	Role      string             `json:"role,omitempty"`
	Content   []InputContentPart `json:"content,omitempty"`
	Name      string             `json:"name,omitempty"`
	Arguments string             `json:"arguments,omitempty"`
	CallID    string             `json:"call_id,omitempty"`
	Output    string             `json:"output,omitempty"`
	Meta      map[string]any     `json:"metadata,omitempty"`
	Raw       map[string]any     `json:"-"`
	Extra     *json.RawMessage   `json:"-"`
}

type InputContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ToolSpec struct {
	Type              string          `json:"type"`
	Name              string          `json:"name,omitempty"`
	Description       string          `json:"description,omitempty"`
	Strict            bool            `json:"strict,omitempty"`
	Parameters        json.RawMessage `json:"parameters,omitempty"`
	ExternalWebAccess bool            `json:"external_web_access,omitempty"`
	Format            *CustomFormat   `json:"format,omitempty"`
}

type CustomFormat struct {
	Type       string `json:"type,omitempty"`
	Syntax     string `json:"syntax,omitempty"`
	Definition string `json:"definition,omitempty"`
}

type StreamEvent struct {
	Type     string       `json:"type"`
	Response *ResponseRef `json:"response,omitempty"`
	Item     *OutputItem  `json:"item,omitempty"`
	Part     *ContentPart `json:"part,omitempty"`
	Delta    string       `json:"delta,omitempty"`
	ItemID   string       `json:"item_id,omitempty"`
	Message  string       `json:"message,omitempty"`
}

type ResponseRef struct {
	ID    string `json:"id,omitempty"`
	Usage *Usage `json:"usage,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	CachedTokens int `json:"cached_tokens,omitempty"`
}

type OutputItem struct {
	ID        string `json:"id,omitempty"`
	Type      string `json:"type,omitempty"`
	Name      string `json:"name,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Status    string `json:"status,omitempty"`
	Output    string `json:"output,omitempty"`
}

type ContentPart struct {
	Type string `json:"type,omitempty"`
	Text string `json:"text,omitempty"`
}

func UserMessage(text string) ResponseInputItem {
	return ResponseInputItem{
		Type: "message",
		Role: "user",
		Content: []InputContentPart{{
			Type: "input_text",
			Text: text,
		}},
	}
}

func FunctionCallInput(name, callID, arguments string) ResponseInputItem {
	return ResponseInputItem{Type: "function_call", Name: name, CallID: callID, Arguments: arguments}
}

func FunctionCallOutputInput(callID, output string) ResponseInputItem {
	return ResponseInputItem{Type: "function_call_output", CallID: callID, Output: output}
}
