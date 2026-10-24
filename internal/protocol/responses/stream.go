package responses

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SSEWriter emits a Responses event stream. It writes HTTP bytes and flushes;
// callers own request lifecycle, logging, and usage accounting.
type SSEWriter struct {
	W       http.ResponseWriter
	Flusher http.Flusher
	MsgID   string
	RespID  string
	Model   string
}

// NewSSEWriter wraps w with fresh response and message IDs.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	f, _ := w.(http.Flusher)
	return &SSEWriter{
		W:       w,
		Flusher: f,
		MsgID:   NewID("msg_"),
		RespID:  NewID("resp_"),
	}
}

// Event writes one SSE event and flushes.
func (s *SSEWriter) Event(event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(s.W, "event: %s\ndata: %s\n\n", event, string(b))
	if s.Flusher != nil {
		s.Flusher.Flush()
	}
}

// Start emits the response.created / response.in_progress pair.
func (s *SSEWriter) Start() {
	s.Event("response.created", map[string]any{"type": "response.created"})
	s.Event("response.in_progress", map[string]any{"type": "response.in_progress"})
}

// Fail emits a terminal error event. No response.completed follows, so the
// client sees an abnormal end instead of a silently short answer.
func (s *SSEWriter) Fail(message, code string) {
	s.Event("error", map[string]any{"type": "error", "code": code, "message": message})
}

// StreamState accumulates text and function-call output across deltas.
type StreamState struct {
	TextEmitted bool
	CallID      string
	CallName    string
	CallEmitted bool
	Args        strings.Builder
	Text        strings.Builder
}

// EmitDelta handles one chat.completion delta and emits the matching Responses
// events. Text, reasoning, and function-call arguments are preserved.
func (s *SSEWriter) EmitDelta(delta map[string]any, st *StreamState) {
	if c, ok := delta["content"].(string); ok && c != "" {
		if !st.TextEmitted {
			st.TextEmitted = true
			s.Event("response.output_item.added", map[string]any{
				"type": "response.output_item.added", "output_index": 0,
				"item": map[string]any{"id": s.MsgID, "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}},
			})
			s.Event("response.content_part.added", map[string]any{
				"type": "response.content_part.added", "item_id": s.MsgID, "output_index": 0, "content_index": 0,
				"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
			})
		}
		st.Text.WriteString(c)
		s.Event("response.output_text.delta", map[string]any{
			"type": "response.output_text.delta", "item_id": s.MsgID, "output_index": 0, "content_index": 0, "delta": c,
		})
	}
	if r, ok := delta["reasoning_content"].(string); ok && r != "" {
		s.Event("response.reasoning_summary_text.delta", map[string]any{
			"type": "response.reasoning_summary_text.delta", "item_id": s.MsgID, "output_index": 0, "content_index": 0, "delta": r,
		})
	}
	if tc, ok := delta["tool_calls"].([]any); ok {
		for _, c := range tc {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if id, ok := cm["id"].(string); ok && id != "" {
				st.CallID = id
			}
			fn, _ := cm["function"].(map[string]any)
			if fn != nil {
				if n, ok := fn["name"].(string); ok && n != "" {
					st.CallName = n
				}
				if a, ok := fn["arguments"].(string); ok && a != "" {
					st.Args.WriteString(a)
				}
			}
			if !st.CallEmitted && st.CallName != "" {
				st.CallEmitted = true
				s.Event("response.output_item.added", map[string]any{
					"type": "response.output_item.added", "output_index": 1,
					"item": map[string]any{"type": "function_call", "id": "fc_" + st.CallName, "call_id": st.CallID,
						"name": st.CallName, "arguments": "", "status": "in_progress"},
				})
			}
		}
	}
}

// Complete emits the closing output items and response.completed.
func (s *SSEWriter) Complete(st *StreamState, usage Usage) {
	if st.TextEmitted {
		text := st.Text.String()
		s.Event("response.output_text.done", map[string]any{
			"type": "response.output_text.done", "item_id": s.MsgID, "output_index": 0, "content_index": 0, "text": text,
		})
		s.Event("response.content_part.done", map[string]any{
			"type": "response.content_part.done", "item_id": s.MsgID, "output_index": 0, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": text, "annotations": []any{}},
		})
		s.Event("response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": 0,
			"item": map[string]any{"id": s.MsgID, "type": "message", "role": "assistant", "status": "completed",
				"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}},
		})
	}
	if st.CallEmitted {
		args := st.Args.String()
		s.Event("response.function_call_arguments.done", map[string]any{
			"type": "response.function_call_arguments.done", "item_id": "fc_" + st.CallName, "output_index": 1, "arguments": args,
		})
		s.Event("response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": 1,
			"item": map[string]any{"type": "function_call", "id": "fc_" + st.CallName, "call_id": st.CallID,
				"name": st.CallName, "arguments": args, "status": "completed"},
		})
	}
	s.Event("response.completed", map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": s.RespID, "object": "response", "created_at": time.Now().Unix(), "status": "completed",
			"model": s.Model, "output": []any{}, "output_text": st.Text.String(),
			"usage": UsageToResponses(usage),
		},
	})
}
