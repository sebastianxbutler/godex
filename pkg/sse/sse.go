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
	callNames    map[string]string
	itemArgs     map[string]*strings.Builder
	emittedCalls map[string]bool
	text         strings.Builder
}

func NewCollector() *Collector {
	return &Collector{
		itemToCallID: map[string]string{},
		callArgs:     map[string]*strings.Builder{},
		callNames:    map[string]string{},
		itemArgs:     map[string]*strings.Builder{},
		emittedCalls: map[string]bool{},
	}
}

func (c *Collector) Observe(ev protocol.StreamEvent) {
	if ev.Type == "response.output_item.added" && ev.Item != nil {
		item := ev.Item
		if item.ID != "" && item.CallID != "" {
			c.itemToCallID[item.ID] = item.CallID
			if pending, ok := c.itemArgs[item.ID]; ok {
				builder := c.ensureCallBuilder(item.CallID)
				builder.WriteString(pending.String())
				delete(c.itemArgs, item.ID)
			}
		}
		if item.CallID != "" && item.Name != "" {
			c.callNames[item.CallID] = item.Name
		}
		if item.Type == "function_call" && item.CallID != "" && item.Arguments != "" {
			builder := c.ensureCallBuilder(item.CallID)
			if builder.Len() == 0 {
				builder.WriteString(item.Arguments)
			}
		}
	}
	if ev.Type == "response.function_call_arguments.delta" {
		callID := ev.CallID
		if callID == "" {
			callID = c.itemToCallID[ev.ItemID]
		}
		if callID != "" && ev.Delta != "" {
			builder := c.ensureCallBuilder(callID)
			builder.WriteString(ev.Delta)
		} else if ev.ItemID != "" && ev.Delta != "" {
			builder := c.ensureItemBuilder(ev.ItemID)
			builder.WriteString(ev.Delta)
		}
	}
	if ev.Type == "response.function_call_arguments.done" {
		if ev.Item != nil {
			if ev.Item.ID != "" && ev.Item.CallID != "" {
				c.itemToCallID[ev.Item.ID] = ev.Item.CallID
				if pending, ok := c.itemArgs[ev.Item.ID]; ok {
					builder := c.ensureCallBuilder(ev.Item.CallID)
					builder.WriteString(pending.String())
					delete(c.itemArgs, ev.Item.ID)
				}
			}
			if ev.Item.CallID != "" && ev.Item.Name != "" {
				c.callNames[ev.Item.CallID] = ev.Item.Name
			}
			if ev.Item.CallID != "" && ev.Item.Arguments != "" {
				builder := c.ensureCallBuilder(ev.Item.CallID)
				if builder.Len() == 0 {
					builder.WriteString(ev.Item.Arguments)
				}
			}
		}
		if ev.CallID != "" && ev.Name != "" {
			c.callNames[ev.CallID] = ev.Name
		}
		if ev.CallID != "" && ev.Arguments != "" {
			builder := c.ensureCallBuilder(ev.CallID)
			if builder.Len() == 0 {
				builder.WriteString(ev.Arguments)
			}
		} else if ev.ItemID != "" && ev.Arguments != "" {
			callID := c.itemToCallID[ev.ItemID]
			if callID != "" {
				builder := c.ensureCallBuilder(callID)
				if builder.Len() == 0 {
					builder.WriteString(ev.Arguments)
				}
			} else {
				builder := c.ensureItemBuilder(ev.ItemID)
				if builder.Len() == 0 {
					builder.WriteString(ev.Arguments)
				}
			}
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

func (c *Collector) FunctionName(callID string) string {
	return c.callNames[callID]
}

func (c *Collector) CallIDForItem(itemID string) string {
	return c.itemToCallID[itemID]
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

func (c *Collector) MarkToolCallEmitted(callID string) bool {
	if callID == "" {
		return true
	}
	if c.emittedCalls[callID] {
		return false
	}
	c.emittedCalls[callID] = true
	return true
}

func (c *Collector) ensureCallBuilder(callID string) *strings.Builder {
	if b := c.callArgs[callID]; b != nil {
		return b
	}
	b := &strings.Builder{}
	c.callArgs[callID] = b
	return b
}

func (c *Collector) ensureItemBuilder(itemID string) *strings.Builder {
	if b := c.itemArgs[itemID]; b != nil {
		return b
	}
	b := &strings.Builder{}
	c.itemArgs[itemID] = b
	return b
}
