package anthropic

import (
	"encoding/json"
	"testing"
)

func TestToOpenAIBasic(t *testing.T) {
	req := Request{
		Model: "claude", MaxTokens: 100, Stream: true, Temperature: 0.5, TopP: 0.9,
		System:   json.RawMessage(`"be nice"`),
		Messages: []Message{{Role: "user", Content: "hi"}},
	}
	var logs []string
	out := ToOpenAI(req, Options{Logf: func(f string, a ...any) { logs = append(logs, f) }})
	if out["model"] != "claude" || out["max_tokens"] != 100 || out["stream"] != true {
		t.Fatalf("out = %v", out)
	}
	if out["temperature"] != 0.5 || out["top_p"] != 0.9 {
		t.Fatalf("sampling = %v", out)
	}
	msgs := out["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %v", msgs)
	}
	if msgs[0].(map[string]any)["role"] != "system" || msgs[0].(map[string]any)["content"] != "be nice" {
		t.Fatalf("system = %v", msgs[0])
	}
	if msgs[1].(map[string]any)["content"] != "hi" {
		t.Fatalf("user = %v", msgs[1])
	}
	if len(logs) != 1 {
		t.Fatalf("logs = %v", logs)
	}
}

func TestToOpenAIOverrideWins(t *testing.T) {
	req := Request{Messages: []Message{{Role: "user", Content: "x"}}, System: json.RawMessage(`"client"`)}
	out := ToOpenAI(req, Options{SystemOverride: "override"})
	msgs := out["messages"].([]any)
	if msgs[0].(map[string]any)["content"] != "override" {
		t.Fatalf("system = %v", msgs[0])
	}
}

func TestToOpenAIThinking(t *testing.T) {
	base := Request{Messages: []Message{{Role: "user", Content: "x"}}}
	base.Thinking = json.RawMessage(`{"type":"enabled"}`)
	if got := ToOpenAI(base, Options{ReasoningEffort: "high"})["reasoning_effort"]; got != "high" {
		t.Fatalf("enabled = %v", got)
	}
	base.Thinking = json.RawMessage(`{"type":"disabled"}`)
	if got := ToOpenAI(base, Options{ReasoningEffort: "high"})["reasoning_effort"]; got != "none" {
		t.Fatalf("disabled = %v", got)
	}
	base.Thinking = json.RawMessage(`{"type":"other"}`)
	if _, ok := ToOpenAI(base, Options{ReasoningEffort: "high"})["reasoning_effort"]; ok {
		t.Fatal("unexpected reasoning_effort")
	}
}

func TestToolsToOpenAI(t *testing.T) {
	tools := []any{
		map[string]any{"name": "search", "description": "find", "input_schema": map[string]any{"type": "object"}},
		map[string]any{"name": "silent", "input_schema": map[string]any{"type": "object"}},
		map[string]any{"type": "function", "function": map[string]any{"name": "passthrough"}},
	}
	out := ToolsToOpenAI(tools)
	if len(out) != 3 {
		t.Fatalf("tools = %v", out)
	}
	fn := out[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "search" || fn["description"] != "find" || fn["parameters"] == nil {
		t.Fatalf("fn = %v", fn)
	}
	fn2 := out[1].(map[string]any)["function"].(map[string]any)
	if _, ok := fn2["description"]; ok {
		t.Fatalf("null description leaked: %v", fn2)
	}
	if out[2].(map[string]any)["type"] != "function" {
		t.Fatalf("passthrough = %v", out[2])
	}
}

func TestToOpenAIToolUseAndResult(t *testing.T) {
	req := Request{
		Messages: []Message{
			{Role: "assistant", Content: []any{
				map[string]any{"type": "text", "text": "checking"},
				map[string]any{"type": "tool_use", "name": "lookup", "input": map[string]any{"q": "x"}},
				map[string]any{"type": "tool_use", "name": ""},
			}},
			{Role: "user", Content: []any{
				map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": map[string]any{"ok": true}},
				map[string]any{"type": "text", "text": "thanks"},
			}},
		},
	}
	out := ToOpenAI(req, Options{NewToolUseID: func() string { return "generated" }})
	msgs := out["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %v", msgs)
	}
	assistant := msgs[0].(map[string]any)
	calls := assistant["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("tool calls = %v", calls)
	}
	call := calls[0].(map[string]any)
	if call["id"] != "generated" {
		t.Fatalf("id = %v", call["id"])
	}
	if call["function"].(map[string]any)["arguments"] != `{"q":"x"}` {
		t.Fatalf("args = %v", call["function"])
	}
	tool := msgs[1].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "t1" || tool["content"] != `{"ok":true}` {
		t.Fatalf("tool = %v", tool)
	}
	if msgs[2].(map[string]any)["content"] != "thanks" {
		t.Fatalf("text after tool = %v", msgs[2])
	}
}

func TestToOpenAICompactionBlock(t *testing.T) {
	req := Request{Messages: []Message{{Role: "user", Content: []any{
		map[string]any{"type": "compaction", "content": "summary text"},
	}}}}
	out := ToOpenAI(req, Options{})
	msgs := out["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %v", msgs)
	}
	if msgs[0].(map[string]any)["content"] != "[Conversation Summary]\nsummary text" {
		t.Fatalf("summary = %v", msgs[0])
	}
}

func TestExtractStringContent(t *testing.T) {
	if got := ExtractStringContent(json.RawMessage(`"plain"`)); got != "plain" {
		t.Fatalf("string = %q", got)
	}
	if got := ExtractStringContent(json.RawMessage(`[{"type":"text","text":"a"},{"type":"text","text":"b"}]`)); got != "a\nb" {
		t.Fatalf("blocks = %q", got)
	}
	if got := ExtractStringContent(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	if got := ExtractStringContent(json.RawMessage(`123`)); got != "" {
		t.Fatalf("number = %q", got)
	}
}

func TestFromOpenAIText(t *testing.T) {
	out := FromOpenAI(map[string]any{
		"model": "m",
		"choices": []any{map[string]any{
			"message": map[string]any{"content": "hello"}, "finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": float64(3), "completion_tokens": float64(4)},
	}, Options{})
	if out["stop_reason"] != "end_turn" || out["role"] != "assistant" {
		t.Fatalf("out = %v", out)
	}
	blocks := out["content"].([]any)
	if len(blocks) != 1 || blocks[0].(map[string]any)["text"] != "hello" {
		t.Fatalf("blocks = %v", blocks)
	}
	usage := out["usage"].(map[string]any)
	if usage["input_tokens"] != float64(3) || usage["output_tokens"] != float64(4) {
		t.Fatalf("usage = %v", usage)
	}
}

func TestFromOpenAIReasoningAndTools(t *testing.T) {
	out := FromOpenAI(map[string]any{
		"choices": []any{map[string]any{
			"message": map[string]any{
				"content":           "answer",
				"reasoning_content": "thought",
				"tool_calls": []any{
					map[string]any{"id": "c1", "function": map[string]any{"name": "lookup", "arguments": `{"q":"x"}`}},
					map[string]any{"function": map[string]any{"name": ""}},
				},
			},
			"finish_reason": "tool_calls",
		}},
	}, Options{NewToolUseID: func() string { return "gen" }})
	blocks := out["content"].([]any)
	if len(blocks) != 3 {
		t.Fatalf("blocks = %v", blocks)
	}
	if blocks[0].(map[string]any)["type"] != "thinking" || blocks[0].(map[string]any)["thinking"] != "thought" {
		t.Fatalf("thinking = %v", blocks[0])
	}
	if blocks[1].(map[string]any)["text"] != "answer" {
		t.Fatalf("text = %v", blocks[1])
	}
	tool := blocks[2].(map[string]any)
	if tool["type"] != "tool_use" || tool["id"] != "c1" || tool["name"] != "lookup" {
		t.Fatalf("tool = %v", tool)
	}
	if tool["input"].(map[string]any)["q"] != "x" {
		t.Fatalf("input = %v", tool["input"])
	}
	if out["stop_reason"] != "tool_use" {
		t.Fatalf("stop = %v", out["stop_reason"])
	}
}

func TestFromOpenAIEmptyChoices(t *testing.T) {
	out := FromOpenAI(map[string]any{}, Options{})
	if out["stop_reason"] != "end_turn" {
		t.Fatalf("out = %v", out)
	}
	blocks := out["content"].([]any)
	if len(blocks) != 1 || blocks[0].(map[string]any)["text"] != "" {
		t.Fatalf("blocks = %v", blocks)
	}
}

func TestFromOpenAIRolledToolID(t *testing.T) {
	out := FromOpenAI(map[string]any{
		"choices": []any{map[string]any{
			"message": map[string]any{
				"tool_calls": []any{map[string]any{"function": map[string]any{"name": "x", "arguments": "{}"}}},
			},
			"finish_reason": "length",
		}},
	}, Options{NewToolUseID: func() string { return "gen-id" }})
	block := out["content"].([]any)[0].(map[string]any)
	if block["id"] != "gen-id" {
		t.Fatalf("id = %v", block)
	}
	if out["stop_reason"] != "max_tokens" {
		t.Fatalf("stop = %v", out["stop_reason"])
	}
}
