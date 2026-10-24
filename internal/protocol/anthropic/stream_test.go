package anthropic

import (
	"strings"
	"testing"
)

func names(events []Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Name
	}
	return out
}

func TestStreamStateThinkingThenText(t *testing.T) {
	st := NewStreamState(-1)
	events := st.PushDelta(map[string]any{"delta": map[string]any{"reasoning_content": "why"}})
	events = append(events, st.PushDelta(map[string]any{"delta": map[string]any{"content": "hi"}})...)

	want := []string{
		"content_block_start", "content_block_delta", // thinking at index 0
		"content_block_stop",                         // thinking closes
		"content_block_start", "content_block_delta", // text at index 1
	}
	if got := names(events); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v", got)
	}
	if events[0].Data["index"] != 0 || events[3].Data["index"] != 1 {
		t.Fatalf("indices = %v / %v", events[0].Data["index"], events[3].Data["index"])
	}
	if !st.HasContent() || !st.TextStarted() {
		t.Fatal("state not marked as content")
	}
}

func TestStreamStateStartIndexForCompaction(t *testing.T) {
	st := NewStreamState(0)
	events := st.PushDelta(map[string]any{"delta": map[string]any{"content": "x"}})
	if events[0].Data["index"] != 1 {
		t.Fatalf("text index = %v, want 1", events[0].Data["index"])
	}
}

func TestStreamStateThinkingOnlyStop(t *testing.T) {
	st := NewStreamState(-1)
	st.PushDelta(map[string]any{"delta": map[string]any{"reasoning_content": "why"}})
	final := st.Finalize()
	if len(final) != 1 || final[0].Name != "content_block_stop" || final[0].Data["index"] != 0 {
		t.Fatalf("final = %+v", final)
	}
}

func TestStreamStateTools(t *testing.T) {
	st := NewStreamState(-1)
	st.ToolID = func() string { return "toolu_fixed" }
	st.PushDelta(map[string]any{"delta": map[string]any{"content": "let me check"}})
	st.PushDelta(map[string]any{"delta": map[string]any{"tool_calls": []any{
		map[string]any{"index": float64(0), "id": "call_a", "function": map[string]any{"name": "lookup", "arguments": `{"q":`}},
	}}})
	st.PushDelta(map[string]any{"delta": map[string]any{"tool_calls": []any{
		map[string]any{"index": float64(0), "function": map[string]any{"arguments": `"x"}`}},
	}}})
	// A malformed call with no name must be dropped.
	st.PushDelta(map[string]any{"delta": map[string]any{"tool_calls": []any{
		map[string]any{"index": float64(1), "function": map[string]any{"arguments": "{}"}},
	}}})
	st.PushDelta(map[string]any{"choice": nil, "finish_reason": "tool_calls"})

	if st.StopReason() != "tool_use" {
		t.Fatalf("stop reason = %s", st.StopReason())
	}
	final := st.Finalize()
	// text stop + one tool block (start/delta/stop)
	if len(final) != 4 {
		t.Fatalf("final events = %v", names(final))
	}
	if final[0].Name != "content_block_stop" || final[0].Data["index"] != 0 {
		t.Fatalf("text stop = %+v", final[0])
	}
	start := final[1]
	block := start.Data["content_block"].(map[string]any)
	if start.Data["index"] != 1 || block["id"] != "call_a" || block["name"] != "lookup" {
		t.Fatalf("tool start = %+v", start.Data)
	}
	delta := final[2].Data["delta"].(map[string]any)
	if delta["partial_json"] != `{"q":"x"}` {
		t.Fatalf("args = %v", delta["partial_json"])
	}
}

func TestStreamStateToolWithoutIDAndEmptyArgs(t *testing.T) {
	st := NewStreamState(-1)
	st.ToolID = func() string { return "toolu_gen" }
	st.PushDelta(map[string]any{"delta": map[string]any{"tool_calls": []any{
		map[string]any{"index": float64(0), "function": map[string]any{"name": "noop"}},
	}}})
	final := st.Finalize()
	if len(final) != 3 {
		t.Fatalf("events = %v", names(final))
	}
	block := final[0].Data["content_block"].(map[string]any)
	if block["id"] != "toolu_gen" {
		t.Fatalf("id = %v", block["id"])
	}
	if final[1].Data["delta"].(map[string]any)["partial_json"] != "{}" {
		t.Fatalf("empty args should become {}: %v", final[1].Data["delta"])
	}
	// A second Finalize must not re-emit.
	if again := st.Finalize(); len(again) != 0 {
		t.Fatalf("second finalize = %v", names(again))
	}
}

func TestStreamStateStopReasons(t *testing.T) {
	st := NewStreamState(-1)
	st.PushDelta(map[string]any{"finish_reason": "length"})
	if st.StopReason() != "max_tokens" {
		t.Fatalf("length => %s", st.StopReason())
	}
	// tool_calls without a named tool stays end_turn.
	st2 := NewStreamState(-1)
	st2.PushDelta(map[string]any{"finish_reason": "tool_calls"})
	if st2.StopReason() != "end_turn" {
		t.Fatalf("unnamed tool_calls => %s", st2.StopReason())
	}
}

func TestStreamStatePlainChoiceShape(t *testing.T) {
	st := NewStreamState(-1)
	events := st.PushDelta(map[string]any{"content": "hi"})
	if len(events) != 2 || events[0].Name != "content_block_start" || !st.HasContent() {
		t.Fatalf("plain choice not handled: %v", names(events))
	}
	if got := st.PushDelta(nil); len(got) != 0 {
		t.Fatalf("nil choice produced %v", names(got))
	}
}
