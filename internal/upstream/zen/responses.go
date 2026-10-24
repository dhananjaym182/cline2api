package zen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"cline-go-proxy/internal/protocol"
	"cline-go-proxy/internal/protocol/openaichat"
)

// Usage is the shared normalized token accounting record.
type Usage = openaichat.Usage

// EventSink consumes Responses events. itemID identifies one function_call
// output item; final=true carries the authoritative full argument string.
type EventSink interface {
	Open(id, model string)
	Content(text string)
	Reasoning(text string)
	ToolStart(itemID, callID, name string)
	ToolArgs(itemID, args string, final bool)
	Done(model, status string, usage Usage)
}

// PumpEvents parses a Responses SSE stream and drives sink.
func PumpEvents(src io.Reader, sink EventSink) error {
	opened := false
	return protocol.ScanSSE(src, func(obj map[string]any) {
		evType, _ := obj["type"].(string)
		resp, _ := obj["response"].(map[string]any)
		switch evType {
		case "response.created":
			if !opened && resp != nil {
				opened = true
				id, _ := resp["id"].(string)
				model, _ := resp["model"].(string)
				sink.Open(id, model)
			}
		case "response.output_text.delta":
			if d, _ := obj["delta"].(string); d != "" {
				sink.Content(d)
			}
		case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
			if d, _ := obj["delta"].(string); d != "" {
				sink.Reasoning(d)
			}
		case "response.output_item.added":
			item, _ := obj["item"].(map[string]any)
			if it, _ := item["type"].(string); it == "function_call" {
				itemID, _ := item["id"].(string)
				callID, _ := item["call_id"].(string)
				name, _ := item["name"].(string)
				sink.ToolStart(itemID, callID, name)
			}
		case "response.function_call_arguments.delta":
			itemID, _ := obj["item_id"].(string)
			if d, _ := obj["delta"].(string); d != "" {
				sink.ToolArgs(itemID, d, false)
			}
		case "response.function_call_arguments.done":
			itemID, _ := obj["item_id"].(string)
			args, _ := obj["arguments"].(string)
			sink.ToolArgs(itemID, args, true)
		case "response.completed", "response.incomplete", "response.failed":
			model := ""
			var usage Usage
			if resp != nil {
				model, _ = resp["model"].(string)
				usage = ResponsesUsage(resp["usage"])
			}
			sink.Done(model, strings.TrimPrefix(evType, "response."), usage)
		}
	})
}

// ResponsesUsage maps Responses usage fields to the normalized record.
func ResponsesUsage(raw any) Usage {
	m, ok := raw.(map[string]any)
	if !ok {
		return Usage{}
	}
	u := Usage{Valid: true}
	u.Prompt = Int64(m["input_tokens"])
	u.Completion = Int64(m["output_tokens"])
	u.Total = Int64(m["total_tokens"])
	if u.Total == 0 {
		u.Total = u.Prompt + u.Completion
	}
	if d, ok := m["input_tokens_details"].(map[string]any); ok {
		u.Cached = Int64(d["cached_tokens"])
	}
	return u
}

// Int64 coerces a JSON-decoded numeric value to int64.
func Int64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

// ChatChunkSink rewrites a Responses event stream into chat.completion.chunk SSE.
type ChatChunkSink struct {
	W       io.Writer
	ID      string
	Created int64
	Model   string
	Err     error

	toolIdx  map[string]int
	toolSeen map[string]bool
	toolN    int
	hasTools bool
}

func (s *ChatChunkSink) write(chunk map[string]any) {
	if s.Err != nil {
		return
	}
	b, err := json.Marshal(chunk)
	if err != nil {
		s.Err = err
		return
	}
	_, s.Err = fmt.Fprintf(s.W, "data: %s\n\n", b)
}

func (s *ChatChunkSink) chunk(delta map[string]any, finish any) {
	s.write(map[string]any{
		"id":      s.ID,
		"object":  "chat.completion.chunk",
		"created": s.Created,
		"model":   s.Model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
	})
}

// Open emits the role chunk.
func (s *ChatChunkSink) Open(id, model string) {
	if id != "" {
		s.ID = id
	}
	if model != "" {
		s.Model = model
	}
	s.chunk(map[string]any{"role": "assistant", "content": ""}, nil)
}

// Content emits a text delta.
func (s *ChatChunkSink) Content(text string) {
	s.chunk(map[string]any{"content": text}, nil)
}

// Reasoning emits a reasoning delta.
func (s *ChatChunkSink) Reasoning(text string) {
	s.chunk(map[string]any{"reasoning_content": text}, nil)
}

// ToolStart emits the opening tool-call chunk and assigns its index.
func (s *ChatChunkSink) ToolStart(itemID, callID, name string) {
	if s.toolIdx == nil {
		s.toolIdx = map[string]int{}
	}
	idx := s.toolN
	s.toolN++
	if itemID == "" {
		itemID = fmt.Sprintf("tc_%d", idx)
	}
	s.toolIdx[itemID] = idx
	s.hasTools = true
	s.chunk(map[string]any{"tool_calls": []any{map[string]any{
		"index": idx, "id": callID, "type": "function",
		"function": map[string]any{"name": name, "arguments": ""},
	}}}, nil)
}

// ToolArgs emits argument fragments. A final event is skipped when incremental
// fragments already delivered the full arguments.
func (s *ChatChunkSink) ToolArgs(itemID, args string, final bool) {
	idx, ok := s.toolIdx[itemID]
	if !ok {
		// No matching start event: an unnamed tool call would be malformed.
		return
	}
	if final {
		if s.toolSeen[itemID] {
			return
		}
	} else {
		if s.toolSeen == nil {
			s.toolSeen = map[string]bool{}
		}
		s.toolSeen[itemID] = true
	}
	s.chunk(map[string]any{"tool_calls": []any{map[string]any{
		"index": idx, "function": map[string]any{"arguments": args},
	}}}, nil)
}

// Done emits the finish chunk, a usage chunk when available, and [DONE].
func (s *ChatChunkSink) Done(model, status string, usage Usage) {
	if model != "" {
		s.Model = model
	}
	if status == "failed" {
		log.Printf("  zen responses: upstream reported failure mid-stream")
	}
	finish := "stop"
	if status == "incomplete" {
		finish = "length"
	} else if s.hasTools && status == "completed" {
		finish = "tool_calls"
	}
	s.chunk(map[string]any{}, finish)
	if usage.Valid {
		s.write(map[string]any{
			"id":      s.ID,
			"object":  "chat.completion.chunk",
			"created": s.Created,
			"model":   s.Model,
			"choices": []any{},
			"usage": map[string]any{
				"prompt_tokens":         usage.Prompt,
				"completion_tokens":     usage.Completion,
				"total_tokens":          usage.Total,
				"prompt_tokens_details": map[string]any{"cached_tokens": usage.Cached},
			},
		})
	}
	if s.Err == nil {
		_, s.Err = fmt.Fprint(s.W, "data: [DONE]\n\n")
	}
}

// CollapseTool is one accumulated tool call in a collapsed response.
type CollapseTool struct {
	ID        string
	Name      string
	Arguments string
}

// CollapseAcc accumulates a Responses stream into a single chat.completion.
type CollapseAcc struct {
	ID           string
	Model        string
	Created      int64
	Content      string
	Reasoning    string
	FinishReason string
	ToolCalls    []CollapseTool
	Usage        Usage
}

// ChatAccSink folds Responses events into a CollapseAcc.
type ChatAccSink struct {
	Acc     *CollapseAcc
	toolIdx map[string]int
}

// NewChatAccSink builds an accumulator sink.
func NewChatAccSink(acc *CollapseAcc) *ChatAccSink {
	return &ChatAccSink{Acc: acc, toolIdx: map[string]int{}}
}

// Open records the response id and model.
func (s *ChatAccSink) Open(id, model string) {
	if id != "" {
		s.Acc.ID = id
	}
	if model != "" {
		s.Acc.Model = model
	}
}

// Content appends text.
func (s *ChatAccSink) Content(text string) { s.Acc.Content += text }

// Reasoning appends reasoning text.
func (s *ChatAccSink) Reasoning(text string) { s.Acc.Reasoning += text }

// ToolStart appends a tool call.
func (s *ChatAccSink) ToolStart(itemID, callID, name string) {
	idx := len(s.Acc.ToolCalls)
	s.Acc.ToolCalls = append(s.Acc.ToolCalls, CollapseTool{ID: callID, Name: name})
	if itemID == "" {
		itemID = fmt.Sprintf("tc_%d", idx)
	}
	s.toolIdx[itemID] = idx
}

// ToolArgs accumulates fragments; a final event's non-empty arguments replace
// the accumulated value so lost deltas cannot truncate the call.
func (s *ChatAccSink) ToolArgs(itemID, args string, final bool) {
	idx, ok := s.toolIdx[itemID]
	if !ok || idx >= len(s.Acc.ToolCalls) {
		return
	}
	if final {
		if args != "" {
			s.Acc.ToolCalls[idx].Arguments = args
		}
		return
	}
	s.Acc.ToolCalls[idx].Arguments += args
}

// Done records the finish reason and usage.
func (s *ChatAccSink) Done(model, status string, usage Usage) {
	if model != "" {
		s.Acc.Model = model
	}
	switch status {
	case "incomplete":
		s.Acc.FinishReason = "length"
	case "completed":
		if len(s.Acc.ToolCalls) > 0 {
			s.Acc.FinishReason = "tool_calls"
		} else {
			s.Acc.FinishReason = "stop"
		}
	default:
		s.Acc.FinishReason = "stop"
	}
	if usage.Valid {
		s.Acc.Usage = usage
	}
}

// CollapseToChatResponse renders a folded accumulator as a chat.completion JSON
// response. It is shared by the chat-SSE and Responses collapse paths.
func CollapseToChatResponse(acc *CollapseAcc, req *http.Request) (*http.Response, error) {
	msg := map[string]any{"role": "assistant", "content": acc.Content}
	if acc.Reasoning != "" {
		msg["reasoning_content"] = acc.Reasoning
	}
	if len(acc.ToolCalls) > 0 {
		calls := make([]any, 0, len(acc.ToolCalls))
		for i, t := range acc.ToolCalls {
			if t.ID == "" {
				t.ID = fmt.Sprintf("call_%d", i)
			}
			calls = append(calls, map[string]any{
				"id":       t.ID,
				"type":     "function",
				"function": map[string]any{"name": t.Name, "arguments": t.Arguments},
			})
		}
		msg["tool_calls"] = calls
	}
	finish := acc.FinishReason
	if finish == "" {
		finish = "stop"
	}
	out := map[string]any{
		"id":      acc.ID,
		"object":  "chat.completion",
		"created": acc.Created,
		"model":   acc.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       msg,
			"finish_reason": finish,
		}},
		"usage": map[string]any{
			"prompt_tokens":     acc.Usage.Prompt,
			"completion_tokens": acc.Usage.Completion,
			"total_tokens":      acc.Usage.Total,
		},
	}
	data, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("collapse zen stream: %w", err)
	}
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: int64(len(data)),
		Request:       req,
	}, nil
}

// StreamResponsesAsChat rewrites an upstream Responses SSE into chat chunk SSE.
// The conversion goroutine owns the upstream body and watchdog cancel: both are
// released when the stream ends or the downstream writer fails.
func StreamResponsesAsChat(resp *http.Response, model string, cancel context.CancelFunc) *http.Response {
	pr, pw := io.Pipe()
	go func() {
		sink := &ChatChunkSink{
			W:       pw,
			ID:      "chatcmpl-" + RandHex(12),
			Created: time.Now().Unix(),
			Model:   model,
		}
		err := PumpEvents(resp.Body, sink)
		if sink.Err != nil {
			err = sink.Err
		}
		resp.Body.Close()
		cancel()
		pw.CloseWithError(err)
	}()
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:          pr,
		ContentLength: -1,
		Request:       resp.Request,
	}
}

// CollapseResponsesStream consumes a Responses SSE stream into a single
// chat.completion JSON response.
func CollapseResponsesStream(resp *http.Response, model string, cancel context.CancelFunc) (*http.Response, error) {
	defer resp.Body.Close()
	defer cancel()
	acc := &CollapseAcc{Model: model, Created: time.Now().Unix()}
	if err := PumpEvents(resp.Body, NewChatAccSink(acc)); err != nil {
		return nil, fmt.Errorf("zen responses stream read: %w", err)
	}
	log.Printf("  zen responses collapse: stream -> json (content_len=%d tool_calls=%d)", len(acc.Content), len(acc.ToolCalls))
	return CollapseToChatResponse(acc, resp.Request)
}
