package proxy

import "encoding/json"

type OpenAIResponsesRequest struct {
	Model              string          `json:"model"`
	Instructions       string          `json:"instructions,omitempty"`
	Input              json.RawMessage `json:"input,omitempty"`
	Tools              []OpenAITool    `json:"tools,omitempty"`
	ToolChoice         any             `json:"tool_choice,omitempty"`
	ParallelToolCalls  *bool           `json:"parallel_tool_calls,omitempty"`
	Stream             *bool           `json:"stream,omitempty"`
	User               string          `json:"user,omitempty"`
	Metadata           any             `json:"metadata,omitempty"`
	Reasoning          any             `json:"reasoning,omitempty"`
	Store              *bool           `json:"store,omitempty"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
	Truncation         string          `json:"truncation,omitempty"`
	MaxOutputTokens    *int            `json:"max_output_tokens,omitempty"`
}

type OpenAITool struct {
	Type     string          `json:"type"`
	Function *OpenAIFunction `json:"function,omitempty"`
	// Flat format fields (Responses API puts these at top level)
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

// ResolvedFunction returns the function spec, handling both flat (Responses API)
// and nested (Chat Completions API) tool formats.
func (t OpenAITool) ResolvedFunction() *OpenAIFunction {
	if t.Function != nil {
		return t.Function
	}
	// Flat format: name/description/parameters at top level
	if t.Name != "" {
		return &OpenAIFunction{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
			Strict:      t.Strict,
		}
	}
	return nil
}

type OpenAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type OpenAIItem struct {
	Type      string `json:"type,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   any    `json:"content,omitempty"`
	Name      string `json:"name,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

type OpenAIChatRequest struct {
	Model      string              `json:"model"`
	Messages   []OpenAIChatMessage `json:"messages"`
	Tools      []OpenAIChatTool    `json:"tools,omitempty"`
	ToolChoice any                 `json:"tool_choice,omitempty"`
	Stream     bool                `json:"stream,omitempty"`
	User       string              `json:"user,omitempty"`
	MaxTokens  *int                `json:"max_tokens,omitempty"`
}

type OpenAIChatMessage struct {
	Role       string               `json:"role"`
	Content    any                  `json:"content"`
	Name       string               `json:"name,omitempty"`
	ToolCalls  []OpenAIChatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"` // For role="tool" messages
}

type OpenAIChatTool struct {
	Type     string          `json:"type"`
	Function *OpenAIFunction `json:"function,omitempty"`
}

type OpenAIModelsResponse struct {
	Object string        `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

type OpenAIResponsesResponse struct {
	ID     string           `json:"id"`
	Object string           `json:"object"`
	Model  string           `json:"model"`
	Output []OpenAIRespItem `json:"output"`
	Usage  *OpenAIUsage     `json:"usage,omitempty"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

type OpenAIRespItem struct {
	Type      string              `json:"type"`
	Role      string              `json:"role,omitempty"`
	Content   []OpenAIRespContent `json:"content,omitempty"`
	Name      string              `json:"name,omitempty"`
	CallID    string              `json:"call_id,omitempty"`
	Arguments string              `json:"arguments,omitempty"`
}

type OpenAIRespContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type OpenAIChatResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []OpenAIChatChoice `json:"choices"`
}

type OpenAIChatChoice struct {
	Index        int               `json:"index"`
	Message      OpenAIChatMessage `json:"message"`
	FinishReason string            `json:"finish_reason,omitempty"`
}

type OpenAIChatStreamChunk struct {
	ID      string                  `json:"id"`
	Object  string                  `json:"object"`
	Created int64                   `json:"created"`
	Model   string                  `json:"model"`
	Choices []OpenAIChatDeltaChoice `json:"choices"`
}

type OpenAIChatDeltaChoice struct {
	Index        int             `json:"index"`
	Delta        OpenAIChatDelta `json:"delta"`
	FinishReason *string         `json:"finish_reason,omitempty"`
}

type OpenAIChatDelta struct {
	Role      string                    `json:"role,omitempty"`
	Content   string                    `json:"content,omitempty"`
	ToolCalls []OpenAIChatToolCallDelta `json:"tool_calls,omitempty"`
}

type OpenAIChatToolCallDelta struct {
	Index    int                      `json:"index"`
	ID       string                   `json:"id,omitempty"`
	Type     string                   `json:"type,omitempty"`
	Function *OpenAIChatToolFuncDelta `json:"function,omitempty"`
}

type OpenAIChatToolFuncDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type OpenAIChatToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function OpenAIChatToolFunction `json:"function"`
}

type OpenAIChatToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
