package sse

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"godex/pkg/protocol"
)

type Event struct {
	Raw   json.RawMessage
	Value protocol.StreamEvent
}

func ParseStream(r io.Reader, emit func(Event) error) error {
	s := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	s.Buffer(buf, 1024*1024)

	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		joined := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		if strings.TrimSpace(joined) == "" || strings.TrimSpace(joined) == "[DONE]" {
			return nil
		}
		raw := json.RawMessage(joined)
		var ev protocol.StreamEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return nil
		}
		return emit(Event{Raw: raw, Value: ev})
	}

	for s.Scan() {
		line := s.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := s.Err(); err != nil {
		return err
	}
	return flush()
}

type Collector struct {
	itemToCallID map[string]string
	callArgs     map[string]*strings.Builder
	text         strings.Builder
}

func NewCollector() *Collector {
	return &Collector{
		itemToCallID: map[string]string{},
		callArgs:     map[string]*strings.Builder{},
	}
}

func (c *Collector) Observe(ev protocol.StreamEvent) {
	if ev.Type == "response.output_item.added" && ev.Item != nil {
		item := ev.Item
		if item.ID != "" && item.CallID != "" {
			c.itemToCallID[item.ID] = item.CallID
		}
		if item.Type == "function_call" && item.CallID != "" && item.Arguments != "" {
			builder := c.ensureCallBuilder(item.CallID)
			builder.WriteString(item.Arguments)
		}
	}
	if ev.Type == "response.function_call_arguments.delta" {
		callID := c.itemToCallID[ev.ItemID]
		if callID != "" && ev.Delta != "" {
			builder := c.ensureCallBuilder(callID)
			builder.WriteString(ev.Delta)
		}
	}
	if ev.Type == "response.output_text.delta" {
		c.text.WriteString(ev.Delta)
	}
	if ev.Type == "response.content_part.added" && ev.Part != nil && ev.Part.Type == "output_text" {
		c.text.WriteString(ev.Part.Text)
	}
}

func (c *Collector) FunctionArgs(callID string) string {
	if b, ok := c.callArgs[callID]; ok {
		return b.String()
	}
	return ""
}

func (c *Collector) AllFunctionArgs() map[string]string {
	out := make(map[string]string, len(c.callArgs))
	for k, b := range c.callArgs {
		out[k] = b.String()
	}
	return out
}

func (c *Collector) OutputText() string {
	return c.text.String()
}

func (c *Collector) ensureCallBuilder(callID string) *strings.Builder {
	if b := c.callArgs[callID]; b != nil {
		return b
	}
	b := &strings.Builder{}
	c.callArgs[callID] = b
	return b
}
