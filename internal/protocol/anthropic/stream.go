package anthropic

import (
	"fmt"
	"sort"
	"time"

	"cline-go-proxy/internal/protocol"
)

// Event is a single Anthropic stream event: the SSE event name and its payload.
type Event struct {
	Name string
	Data map[string]any
}

// toolAccumulator collects a streamed tool call across deltas.
type toolAccumulator struct {
	index   int
	id      string
	name    string
	args    string
	emitted bool
}

// StreamState translates OpenAI chat chunks into the Anthropic streaming event
// sequence. Block indices, the thinking/text lifecycle, and tool accumulation are
// protocol concerns owned here; the caller owns the SSE writer, heartbeats,
// retries, usage accounting and logging.
type StreamState struct {
	// ToolID generates an id for a tool call that arrives without one.
	ToolID func() string

	textIndex    int
	hasText      bool
	sawThinking  bool
	thinkingIdx  int
	thinkingOpen bool
	sawNamedTool bool
	stopReason   string
	pendingTools map[int]*toolAccumulator
}

// NewStreamState starts block numbering after startIndex. A compaction advisory
// block occupies index 0, so callers pass -1 normally and 0 in that case.
func NewStreamState(startIndex int) *StreamState {
	return &StreamState{
		textIndex:    startIndex,
		thinkingIdx:  -1,
		stopReason:   "end_turn",
		pendingTools: map[int]*toolAccumulator{},
	}
}

func (s *StreamState) newToolID() string {
	if s.ToolID != nil {
		return s.ToolID()
	}
	return fmt.Sprintf("toolu_%x", time.Now().UnixNano())
}

// HasContent reports whether any content event has been produced. Until then the
// attempt is invisible to the client and safe to retry on another route.
func (s *StreamState) HasContent() bool { return s.hasText || s.sawThinking || s.sawNamedTool }

// TextStarted reports whether a text block has been opened.
func (s *StreamState) TextStarted() bool { return s.hasText }

// StopReason is the resolved finish reason for the message_delta envelope.
func (s *StreamState) StopReason() string { return s.stopReason }

// ToolCount is the number of accumulated tool calls.
func (s *StreamState) ToolCount() int { return len(s.pendingTools) }

// PushDelta converts one upstream choice object (delta or message shape) into
// the Anthropic events it produces.
func (s *StreamState) PushDelta(choice map[string]any) []Event {
	if choice == nil {
		return nil
	}
	delta, ok := choice["delta"].(map[string]any)
	if !ok {
		delta = choice
	}
	var events []Event

	if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
		s.sawThinking = true
		if !s.thinkingOpen {
			s.textIndex++
			s.thinkingIdx = s.textIndex
			s.thinkingOpen = true
			events = append(events, Event{Name: "content_block_start", Data: map[string]any{
				"type":  "content_block_start",
				"index": s.thinkingIdx,
				"content_block": map[string]any{
					"type":      "thinking",
					"thinking":  "",
					"signature": "",
				},
			}})
		}
		events = append(events, Event{Name: "content_block_delta", Data: map[string]any{
			"type":  "content_block_delta",
			"index": s.thinkingIdx,
			"delta": map[string]any{
				"type":     "thinking_delta",
				"thinking": rc,
			},
		}})
	}

	if c, ok := delta["content"].(string); ok && c != "" {
		if !s.hasText {
			s.hasText = true
			if s.thinkingOpen {
				events = append(events, Event{Name: "content_block_stop", Data: map[string]any{
					"type":  "content_block_stop",
					"index": s.thinkingIdx,
				}})
				s.thinkingOpen = false
			}
			s.textIndex++
			events = append(events, Event{Name: "content_block_start", Data: map[string]any{
				"type":  "content_block_start",
				"index": s.textIndex,
				"content_block": map[string]any{
					"type": "text",
					"text": "",
				},
			}})
		}
		events = append(events, Event{Name: "content_block_delta", Data: map[string]any{
			"type":  "content_block_delta",
			"index": s.textIndex,
			"delta": map[string]any{
				"type": "text_delta",
				"text": protocol.SanitizeContent(c),
			},
		}})
	}

	if tcRaw, ok := delta["tool_calls"].([]any); ok {
		for _, tc := range tcRaw {
			tcMap, _ := tc.(map[string]any)
			if tcMap == nil {
				continue
			}
			idx := 0
			if i, ok := tcMap["index"].(float64); ok {
				idx = int(i)
			}
			acc, exists := s.pendingTools[idx]
			if !exists {
				acc = &toolAccumulator{index: idx}
				s.pendingTools[idx] = acc
			}
			if id, ok := tcMap["id"].(string); ok && id != "" {
				acc.id = id
			}
			if fn, ok := tcMap["function"].(map[string]any); ok {
				if name, ok := fn["name"].(string); ok && name != "" {
					acc.name = name
					s.sawNamedTool = true
				}
				if args, ok := fn["arguments"].(string); ok && args != "" {
					acc.args += args
				}
			}
		}
	}

	if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
		switch fr {
		case "length":
			s.stopReason = "max_tokens"
		case "tool_calls":
			if s.sawNamedTool {
				s.stopReason = "tool_use"
			}
		}
	}
	return events
}

// Finalize closes open blocks and emits tool_use blocks in index order. A
// malformed tool call without a name is dropped (streamed fragments can lose
// the name chunk entirely).
func (s *StreamState) Finalize() []Event {
	var events []Event
	if s.thinkingOpen {
		events = append(events, Event{Name: "content_block_stop", Data: map[string]any{
			"type":  "content_block_stop",
			"index": s.thinkingIdx,
		}})
		s.thinkingOpen = false
	}
	if s.hasText {
		events = append(events, Event{Name: "content_block_stop", Data: map[string]any{
			"type":  "content_block_stop",
			"index": s.textIndex,
		}})
	}
	nextIndex := s.textIndex + 1
	keys := make([]int, 0, len(s.pendingTools))
	for k := range s.pendingTools {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, k := range keys {
		acc := s.pendingTools[k]
		if acc.name == "" {
			continue
		}
		if acc.id == "" {
			acc.id = s.newToolID()
		}
		if acc.emitted {
			continue
		}
		acc.emitted = true
		args := acc.args
		if args == "" {
			args = "{}"
		}
		events = append(events,
			Event{Name: "content_block_start", Data: map[string]any{
				"type":  "content_block_start",
				"index": nextIndex,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    acc.id,
					"name":  acc.name,
					"input": map[string]any{},
				},
			}},
			Event{Name: "content_block_delta", Data: map[string]any{
				"type":  "content_block_delta",
				"index": nextIndex,
				"delta": map[string]any{
					"type":         "input_json_delta",
					"partial_json": args,
				},
			}},
			Event{Name: "content_block_stop", Data: map[string]any{
				"type":  "content_block_stop",
				"index": nextIndex,
			}},
		)
		nextIndex++
	}
	return events
}
