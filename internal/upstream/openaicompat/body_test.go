package openaicompat

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildChatBodyCopiesAndSanitizes(t *testing.T) {
	params := map[string]any{
		"model":       "vendor/model",
		"temperature": 0.3,
		"max_tokens":  float64(4096),
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "assistant", "tool_calls": []any{
				map[string]any{"id": "call_bad", "function": map[string]any{"name": ""}},
			}},
		},
		"tools": []any{map[string]any{"function": map[string]any{
			"name":        "lookup",
			"description": nil,
			"parameters":  nil,
		}}},
	}
	body := BuildChatBody(params, true, BodyOptions{
		PassThroughKeys:  []string{"temperature", "tools"},
		MinMaxTokens:     16,
		DefaultMaxTokens: 32000,
	})
	if body["stream"] != true || body["temperature"] != 0.3 || body["model"] != "vendor/model" {
		t.Fatalf("body = %v", body)
	}
	if body["max_tokens"] != float64(4096) {
		t.Fatalf("max_tokens = %v", body["max_tokens"])
	}
	msgs := body["messages"].([]any)
	if _, ok := msgs[1].(map[string]any)["tool_calls"]; ok {
		t.Fatal("invalid tool_calls must be removed")
	}
	tool := body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if _, ok := tool["description"]; ok {
		t.Fatal("null description must be removed")
	}
	if _, ok := tool["parameters"]; ok {
		t.Fatal("null parameters must be removed")
	}
}

func TestBuildChatBodyClamps(t *testing.T) {
	var logs []string
	params := map[string]any{
		"model":      "vendor/small",
		"max_tokens": float64(2),
	}
	body := BuildChatBody(params, false, BodyOptions{
		MinMaxTokens:     16,
		DefaultMaxTokens: 32000,
		Logf: func(format string, args ...any) {
			logs = append(logs, strings.TrimSpace(format))
		},
	})
	if body["max_tokens"] != 32000 {
		t.Fatalf("below-min clamp = %v", body["max_tokens"])
	}

	params = map[string]any{"model": "vendor/small", "max_tokens": float64(128000)}
	body = BuildChatBody(params, false, BodyOptions{
		MinMaxTokens:     16,
		DefaultMaxTokens: 32000,
		ModelMaxOutputLimit: func(model string) int {
			if model == "vendor/small" {
				return 65536
			}
			return 0
		},
	})
	if body["max_tokens"] != float64(65536) {
		t.Fatalf("model limit clamp = %v", body["max_tokens"])
	}
	if len(logs) != 1 {
		t.Fatalf("logs = %v", logs)
	}
}

func TestPrepareBody(t *testing.T) {
	data, err := PrepareBody(map[string]any{"model": "m", "messages": []any{}}, true, BodyOptions{
		MinMaxTokens:     16,
		DefaultMaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("PrepareBody: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["stream"] != true || decoded["model"] != "m" {
		t.Fatalf("decoded = %v", decoded)
	}
}
