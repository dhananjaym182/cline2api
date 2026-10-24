package responses

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

type sseEvent struct {
	Name string
	Data map[string]any
}

func parseSSE(t *testing.T, raw string) []sseEvent {
	t.Helper()
	var out []sseEvent
	for _, block := range strings.Split(strings.TrimSpace(raw), "\n\n") {
		if block == "" {
			continue
		}
		var ev sseEvent
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.Name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev.Data); err != nil {
					t.Fatalf("bad data %q: %v", line, err)
				}
			}
		}
		out = append(out, ev)
	}
	return out
}

func eventNames(events []sseEvent) []string {
	names := make([]string, len(events))
	for i, e := range events {
		names[i] = e.Name
	}
	return names
}

func TestSSEWriterStart(t *testing.T) {
	rec := httptest.NewRecorder()
	NewSSEWriter(rec).Start()
	names := eventNames(parseSSE(t, rec.Body.String()))
	if len(names) != 2 || names[0] != "response.created" || names[1] != "response.in_progress" {
		t.Fatalf("events = %v", names)
	}
}

func TestSSEWriterTextLifecycle(t *testing.T) {
	rec := httptest.NewRecorder()
	s := NewSSEWriter(rec)
	st := &StreamState{}
	s.EmitDelta(map[string]any{"content": "hel"}, st)
	s.EmitDelta(map[string]any{"content": "lo"}, st)
	events := parseSSE(t, rec.Body.String())
	want := []string{
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
		"response.output_text.delta",
	}
	if got := eventNames(events); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v", got)
	}
	if events[2].Data["delta"] != "hel" || events[3].Data["delta"] != "lo" {
		t.Fatalf("deltas = %v / %v", events[2].Data["delta"], events[3].Data["delta"])
	}
	if st.Text.String() != "hello" {
		t.Fatalf("text = %q", st.Text.String())
	}
}

func TestSSEWriterReasoningDelta(t *testing.T) {
	rec := httptest.NewRecorder()
	s := NewSSEWriter(rec)
	s.EmitDelta(map[string]any{"reasoning_content": "think"}, &StreamState{})
	events := parseSSE(t, rec.Body.String())
	if len(events) != 1 || events[0].Name != "response.reasoning_summary_text.delta" {
		t.Fatalf("events = %v", eventNames(events))
	}
	if events[0].Data["delta"] != "think" {
		t.Fatalf("delta = %v", events[0].Data["delta"])
	}
}

func TestSSEWriterToolCalls(t *testing.T) {
	rec := httptest.NewRecorder()
	s := NewSSEWriter(rec)
	st := &StreamState{}
	s.EmitDelta(map[string]any{"tool_calls": []any{
		map[string]any{"id": "call_1", "function": map[string]any{"name": "lookup", "arguments": `{"q":`}},
	}}, st)
	s.EmitDelta(map[string]any{"tool_calls": []any{
		map[string]any{"function": map[string]any{"arguments": `"x"}`}},
	}}, st)
	s.Complete(st, Usage{Prompt: 3, Completion: 4, Cached: 1, Total: 7})

	events := parseSSE(t, rec.Body.String())
	var added, argsDone, itemDone, completed map[string]any
	for _, e := range events {
		switch e.Name {
		case "response.output_item.added":
			if added == nil {
				added = e.Data
			}
		case "response.function_call_arguments.done":
			argsDone = e.Data
		case "response.output_item.done":
			itemDone = e.Data
		case "response.completed":
			completed = e.Data
		}
	}
	if added == nil || added["output_index"] != float64(1) {
		t.Fatalf("tool item not announced: %v", added)
	}
	if argsDone == nil || argsDone["arguments"] != `{"q":"x"}` {
		t.Fatalf("arguments = %v", argsDone)
	}
	item, _ := itemDone["item"].(map[string]any)
	if item["call_id"] != "call_1" || item["name"] != "lookup" {
		t.Fatalf("item = %v", item)
	}
	resp, _ := completed["response"].(map[string]any)
	usage, _ := resp["usage"].(map[string]any)
	if usage["input_tokens"] != float64(3) || usage["output_tokens"] != float64(4) {
		t.Fatalf("usage = %v", usage)
	}
}

func TestSSEWriterCompleteWithoutText(t *testing.T) {
	rec := httptest.NewRecorder()
	s := NewSSEWriter(rec)
	s.Model = "m"
	s.Complete(&StreamState{}, Usage{})

	events := parseSSE(t, rec.Body.String())
	if len(events) != 1 || events[0].Name != "response.completed" {
		t.Fatalf("events = %v", eventNames(events))
	}
	resp, _ := events[0].Data["response"].(map[string]any)
	if resp["model"] != "m" || resp["output_text"] != "" {
		t.Fatalf("response = %v", resp)
	}
}

func TestSSEWriterCompleteText(t *testing.T) {
	rec := httptest.NewRecorder()
	s := NewSSEWriter(rec)
	st := &StreamState{}
	s.EmitDelta(map[string]any{"content": "done"}, st)
	s.Complete(st, Usage{})
	events := parseSSE(t, rec.Body.String())
	names := eventNames(events)
	if names[len(names)-1] != "response.completed" {
		t.Fatalf("last event = %v", names)
	}
	for _, want := range []string{"response.output_text.done", "response.content_part.done", "response.output_item.done"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
}
