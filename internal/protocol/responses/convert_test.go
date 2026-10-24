package responses

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToChatRequest(t *testing.T) {
	body := map[string]any{
		"model":             "m",
		"stream":            true,
		"max_output_tokens": float64(256),
		"temperature":       0.4,
		"instructions":      "sys",
		"input":             "hello",
		"tools": []any{map[string]any{
			"type": "function", "name": "f", "description": "d",
			"parameters": map[string]any{"type": "object"},
		}},
		"tool_choice": "auto",
	}
	out := ToChat(body)
	if out["model"] != "m" || out["stream"] != true || out["max_tokens"] != 256 || out["temperature"] != 0.4 {
		t.Fatalf("out = %v", out)
	}
	msgs := out["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" {
		t.Fatalf("messages = %v", msgs)
	}
	tools := out["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v", tools)
	}
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "f" || fn["description"] != "d" || fn["parameters"] == nil {
		t.Fatalf("fn = %v", fn)
	}
	if out["tool_choice"] != "auto" {
		t.Fatalf("tool_choice = %v", out["tool_choice"])
	}
}

func TestInputToMessagesItems(t *testing.T) {
	input := []any{
		map[string]any{"type": "message", "content": []any{
			map[string]any{"type": "output_text", "text": "a"},
			map[string]any{"type": "text", "text": "b"},
		}},
		map[string]any{"type": "function_call", "call_id": "c1", "name": "f", "arguments": map[string]any{"x": 1}},
		map[string]any{"type": "function_call_output", "call_id": "c1", "output": map[string]any{"ok": true}},
		map[string]any{"type": "reasoning"},
	}
	msgs := InputToMessages(input)
	if len(msgs) != 3 {
		t.Fatalf("messages = %v", msgs)
	}
	if msgs[0].(map[string]any)["content"] != "a\nb" || msgs[0].(map[string]any)["role"] != "user" {
		t.Fatalf("message = %v", msgs[0])
	}
	call := msgs[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if call["id"] != "c1" || call["function"].(map[string]any)["arguments"] != `{"x":1}` {
		t.Fatalf("call = %v", call)
	}
	tool := msgs[2].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "c1" || tool["content"] != `{"ok":true}` {
		t.Fatalf("tool = %v", tool)
	}
}

func TestStringifyContent(t *testing.T) {
	if got := StringifyContent("x"); got != "x" {
		t.Fatalf("string = %q", got)
	}
	if got := StringifyContent([]any{
		map[string]any{"text": "a"}, map[string]any{"text": "b"},
	}); got != "a\nb" {
		t.Fatalf("blocks = %q", got)
	}
	if got := StringifyContent(42); got != "" {
		t.Fatalf("other = %q", got)
	}
}

func TestToolsToChatSkipsNonFunctions(t *testing.T) {
	out := ToolsToChat([]any{
		map[string]any{"type": "web_search"},
		map[string]any{"type": "function", "name": "f"},
		"not-a-map",
	})
	if len(out) != 1 {
		t.Fatalf("out = %v", out)
	}
	fn := out[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "f" {
		t.Fatalf("fn = %v", fn)
	}
	if _, ok := fn["description"]; ok {
		t.Fatalf("unexpected description: %v", fn)
	}
}

func TestChatToResponsesTextAndUsage(t *testing.T) {
	out := ChatToResponses(map[string]any{
		"model": "m",
		"choices": []any{map[string]any{
			"message": map[string]any{"content": "hello"},
		}},
		"usage": map[string]any{
			"prompt_tokens":             float64(10),
			"completion_tokens":         float64(5),
			"total_tokens":              float64(15),
			"prompt_tokens_details":     map[string]any{"cached_tokens": float64(2)},
			"completion_tokens_details": map[string]any{"reasoning_tokens": float64(1)},
		},
	})
	if out["object"] != "response" || out["status"] != "completed" || out["output_text"] != "hello" {
		t.Fatalf("out = %v", out)
	}
	outputs := out["output"].([]any)
	if len(outputs) != 1 {
		t.Fatalf("outputs = %v", outputs)
	}
	msg := outputs[0].(map[string]any)
	blocks := msg["content"].([]any)
	if len(blocks) != 1 || blocks[0].(map[string]any)["text"] != "hello" {
		t.Fatalf("blocks = %v", blocks)
	}
	usage := out["usage"].(map[string]any)
	if usage["input_tokens"] != float64(10) || usage["output_tokens"] != float64(5) || usage["total_tokens"] != float64(15) {
		t.Fatalf("usage = %v", usage)
	}
	if usage["input_tokens_details"].(map[string]any)["cached_tokens"] != float64(2) {
		t.Fatalf("cached = %v", usage)
	}
	if usage["output_tokens_details"].(map[string]any)["reasoning_tokens"] != float64(1) {
		t.Fatalf("reasoning = %v", usage)
	}
}

func TestChatToResponsesToolCalls(t *testing.T) {
	out := ChatToResponses(map[string]any{
		"choices": []any{map[string]any{
			"message": map[string]any{
				"tool_calls": []any{
					map[string]any{"id": "c1", "function": map[string]any{"name": "f", "arguments": `{"x":1}`}},
					map[string]any{"function": map[string]any{"name": ""}},
				},
			},
		}},
	})
	outputs := out["output"].([]any)
	if len(outputs) != 2 {
		t.Fatalf("outputs = %v", outputs)
	}
	call := outputs[1].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "c1" || call["name"] != "f" || call["arguments"] != `{"x":1}` {
		t.Fatalf("call = %v", call)
	}
}

func TestNewIDPrefix(t *testing.T) {
	for _, prefix := range []string{"resp_", "msg_", "fc_"} {
		id := NewID(prefix)
		if !strings.HasPrefix(id, prefix) || len(id) == len(prefix) {
			t.Fatalf("id = %q", id)
		}
		if _, err := json.Marshal(id); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUsageToResponses(t *testing.T) {
	u := UsageToResponses(Usage{Prompt: 10, Completion: 5, Cached: 3})
	if u["total_tokens"] != int64(15) {
		t.Fatalf("total = %v", u["total_tokens"])
	}
	if u["input_tokens_details"].(map[string]any)["cached_tokens"] != int64(3) {
		t.Fatalf("cached = %v", u)
	}
	u2 := UsageToResponses(Usage{Prompt: 1, Completion: 2, Total: 99})
	if u2["total_tokens"] != int64(99) {
		t.Fatalf("explicit total = %v", u2["total_tokens"])
	}
	if u2["input_tokens_details"].(map[string]any)["cached_tokens"] != 0 {
		t.Fatalf("zero cached = %v", u2)
	}
}
