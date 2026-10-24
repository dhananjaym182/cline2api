package zen

import (
	"encoding/json"
	"strings"
	"testing"
)

func sseStream(events ...map[string]any) string {
	var b strings.Builder
	for _, ev := range events {
		data, _ := json.Marshal(ev)
		b.WriteString("data: " + string(data) + "\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func TestResponsesUsage(t *testing.T) {
	u := ResponsesUsage(map[string]any{
		"input_tokens": float64(10), "output_tokens": float64(5),
		"input_tokens_details": map[string]any{"cached_tokens": float64(4)},
	})
	if !u.Valid || u.Prompt != 10 || u.Completion != 5 || u.Total != 15 || u.Cached != 4 {
		t.Fatalf("usage = %+v", u)
	}
	if u := ResponsesUsage(nil); u.Valid {
		t.Fatal("nil usage must be invalid")
	}
}

func TestPumpEventsStreaming(t *testing.T) {
	sse := sseStream(
		map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_1", "model": "muse"}},
		map[string]any{"type": "response.output_text.delta", "delta": "Hel"},
		map[string]any{"type": "response.output_text.delta", "delta": "lo"},
		map[string]any{"type": "response.output_item.added", "item": map[string]any{
			"type": "function_call", "id": "fc_1", "call_id": "call_9", "name": "bash",
		}},
		map[string]any{"type": "response.function_call_arguments.delta", "item_id": "fc_1", "delta": `{"cm`},
		map[string]any{"type": "response.function_call_arguments.delta", "item_id": "fc_1", "delta": `d":"ls"}`},
		map[string]any{"type": "response.function_call_arguments.done", "item_id": "fc_1", "arguments": `{"cmd":"ls"}`},
		map[string]any{"type": "response.completed", "response": map[string]any{
			"id": "resp_1", "model": "muse",
			"usage": map[string]any{"input_tokens": float64(10), "output_tokens": float64(5)},
		}},
	)
	var out strings.Builder
	sink := &ChatChunkSink{W: &out, ID: "chatcmpl-x", Created: 123, Model: "muse"}
	if err := PumpEvents(strings.NewReader(sse), sink); err != nil {
		t.Fatal(err)
	}
	if sink.Err != nil {
		t.Fatal(sink.Err)
	}
	raw := out.String()
	if !strings.Contains(raw, "data: [DONE]") {
		t.Fatalf("missing terminator:\n%s", raw)
	}
	var chunks []map[string]any
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") || strings.Contains(line, "[DONE]") {
			continue
		}
		var c map[string]any
		if err := json.Unmarshal([]byte(line[6:]), &c); err != nil {
			t.Fatalf("bad chunk %q: %v", line, err)
		}
		chunks = append(chunks, c)
	}
	// role + 2 content + tool start + 2 args (the done event is redundant) + finish + usage
	if len(chunks) != 8 {
		t.Fatalf("chunk count = %d:\n%s", len(chunks), raw)
	}
	if chunks[0]["id"] != "resp_1" || chunks[0]["object"] != "chat.completion.chunk" {
		t.Fatalf("header chunk = %v", chunks[0])
	}
	finish := chunks[6]["choices"].([]any)[0].(map[string]any)["finish_reason"]
	if finish != "tool_calls" {
		t.Fatalf("finish = %v", finish)
	}
	if choices := chunks[7]["choices"].([]any); len(choices) != 0 {
		t.Fatalf("usage chunk choices = %v", choices)
	}
}

func TestChatChunkSinkOrphanArgsIgnored(t *testing.T) {
	var out strings.Builder
	sink := &ChatChunkSink{W: &out}
	sink.ToolArgs("missing", "x", false)
	if out.Len() != 0 {
		t.Fatalf("orphan args produced output: %s", out.String())
	}
}

func TestPumpEventsCollapse(t *testing.T) {
	sse := sseStream(
		map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_2", "model": "muse"}},
		map[string]any{"type": "response.output_text.delta", "delta": "answer"},
		map[string]any{"type": "response.reasoning_summary_text.delta", "delta": "think"},
		map[string]any{"type": "response.output_item.added", "item": map[string]any{
			"type": "function_call", "id": "fc_a", "call_id": "call_a", "name": "read",
		}},
		map[string]any{"type": "response.function_call_arguments.delta", "item_id": "fc_a", "delta": `{"pa`},
		map[string]any{"type": "response.function_call_arguments.done", "item_id": "fc_a", "arguments": `{"path":"x"}`},
		map[string]any{"type": "response.completed", "response": map[string]any{
			"model": "muse-final",
			"usage": map[string]any{"input_tokens": float64(7), "output_tokens": float64(3)},
		}},
	)
	acc := &CollapseAcc{Model: "muse", Created: 42}
	if err := PumpEvents(strings.NewReader(sse), NewChatAccSink(acc)); err != nil {
		t.Fatal(err)
	}
	if acc.ID != "resp_2" || acc.Content != "answer" || acc.Reasoning != "think" || acc.Model != "muse-final" {
		t.Fatalf("acc = %+v", acc)
	}
	if acc.FinishReason != "tool_calls" || len(acc.ToolCalls) != 1 {
		t.Fatalf("finish/tools = %+v", acc)
	}
	if acc.ToolCalls[0].Arguments != `{"path":"x"}` {
		t.Fatalf("args = %q", acc.ToolCalls[0].Arguments)
	}
	if !acc.Usage.Valid || acc.Usage.Total != 10 {
		t.Fatalf("usage = %+v", acc.Usage)
	}

	resp, err := CollapseToChatResponse(acc, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["object"] != "chat.completion" || out["model"] != "muse-final" {
		t.Fatalf("response = %v", out)
	}
	choice := out["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("finish = %v", choice["finish_reason"])
	}
}

func TestCollapseToChatResponseDefaults(t *testing.T) {
	acc := &CollapseAcc{Content: "x"}
	resp, err := CollapseToChatResponse(acc, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	choice := out["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "stop" {
		t.Fatalf("finish = %v", choice["finish_reason"])
	}
}

func TestInt64(t *testing.T) {
	for _, v := range []any{float64(2), int64(2), 2, json.Number("2")} {
		if got := Int64(v); got != 2 {
			t.Errorf("%T => %d", v, got)
		}
	}
	if Int64("x") != 0 {
		t.Fatal("non-numeric must be 0")
	}
}
