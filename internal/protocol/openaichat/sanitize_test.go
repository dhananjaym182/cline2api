package openaichat

import (
	"strings"
	"testing"
)

const longToolName = "mcp__some_extremely_long_server_name__some_extremely_long_tool_name"

func TestShortenToolName(t *testing.T) {
	got := ShortenToolName(longToolName)
	if len(got) > MaxToolNameLength {
		t.Fatalf("shortened name is %d bytes: %q", len(got), got)
	}
	if again := ShortenToolName(longToolName); again != got {
		t.Fatal("shortening must be deterministic")
	}
	if ShortenToolName("other_long_tool_name_for_collision_check_that_is_definitely_longer") == got {
		t.Fatal("different names must not collapse to the same short name")
	}
}

func TestClampToolNames(t *testing.T) {
	params := map[string]any{
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": longToolName}},
		},
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{"id": "call_1", "function": map[string]any{"name": longToolName}},
				},
			},
		},
		"tool_choice": map[string]any{"type": "function", "function": map[string]any{"name": longToolName}},
	}
	mapping := ClampToolNames(params)
	if len(mapping) != 1 {
		t.Fatalf("mapping = %v", mapping)
	}
	short := ShortenToolName(longToolName)
	if len(short) > MaxToolNameLength {
		t.Fatalf("short name too long: %q", short)
	}
	toolName := params["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"]
	if toolName != short {
		t.Fatalf("tool definition name = %v", toolName)
	}
	call := params["messages"].([]any)[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if call["function"].(map[string]any)["name"] != short {
		t.Fatal("historical tool_call name must be clamped")
	}
	if params["tool_choice"].(map[string]any)["function"].(map[string]any)["name"] != short {
		t.Fatal("tool_choice name must be clamped")
	}

	if m := ClampToolNames(map[string]any{
		"tools": []any{map[string]any{"function": map[string]any{"name": "short"}}},
	}); m != nil {
		t.Fatalf("no clamping expected, got %v", m)
	}
}

func TestRestoreToolCalls(t *testing.T) {
	mapping := map[string]string{"short": "original"}
	stream := map[string]any{"choices": []any{
		map[string]any{"delta": map[string]any{"tool_calls": []any{
			map[string]any{"function": map[string]any{"name": "short"}},
		}}},
	}}
	RestoreToolCalls(stream, mapping)
	name := stream["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"]
	if name != "original" {
		t.Fatalf("delta name = %v", name)
	}

	nonStream := map[string]any{"choices": []any{
		map[string]any{"message": map[string]any{"tool_calls": []any{
			map[string]any{"function": map[string]any{"name": "short"}},
		}}},
	}}
	RestoreToolCalls(nonStream, mapping)
	name = nonStream["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"]
	if name != "original" {
		t.Fatalf("message name = %v", name)
	}

	RestoreToolCalls(stream, nil) // must be a no-op
}

func TestSanitizeMessages(t *testing.T) {
	in := []any{
		map[string]any{"role": "user", "content": "hi"},
		map[string]any{
			"role": "assistant",
			"tool_calls": []any{
				map[string]any{"id": "call_good", "function": map[string]any{"name": "lookup"}},
				map[string]any{"id": "call_bad", "function": map[string]any{"name": ""}},
			},
		},
		map[string]any{"role": "tool", "tool_call_id": "call_good", "content": "ok"},
		map[string]any{"role": "tool", "tool_call_id": "call_orphan", "content": "stale"},
		map[string]any{
			"role":       "assistant",
			"tool_calls": []any{map[string]any{"id": "call_empty", "function": map[string]any{"name": ""}}},
		},
	}
	out := SanitizeMessages(in)
	if len(out) != 4 {
		t.Fatalf("len(out) = %d, want 4", len(out))
	}
	assistant := out[1].(map[string]any)
	tcs := assistant["tool_calls"].([]any)
	if len(tcs) != 1 || tcs[0].(map[string]any)["id"] != "call_good" {
		t.Fatalf("tool_calls = %v", tcs)
	}
	if _, ok := out[4-1].(map[string]any)["tool_calls"]; ok {
		t.Fatal("assistant with only invalid tool_calls must drop the field")
	}
}

func TestSanitizeTools(t *testing.T) {
	body := map[string]any{"tools": []any{
		map[string]any{"type": "function", "function": map[string]any{
			"name":        "nested",
			"description": nil,
			"parameters":  nil,
		}},
		map[string]any{
			"name":        "flat",
			"description": "kept",
			"parameters":  map[string]any{"type": "object"},
		},
		map[string]any{"type": "function", "function": map[string]any{"description": "nameless"}},
	}}
	SanitizeTools(body)
	tools := body["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %v", tools)
	}
	nested := tools[0].(map[string]any)["function"].(map[string]any)
	if _, ok := nested["description"]; ok {
		t.Fatal("null description must be removed")
	}
	if _, ok := nested["parameters"]; ok {
		t.Fatal("null parameters must be removed")
	}
	flat := tools[1].(map[string]any)
	if flat["description"] != "kept" {
		t.Fatal("non-empty description must be preserved")
	}
	if _, ok := flat["parameters"]; !ok {
		t.Fatal("object parameters must be preserved")
	}
}

func TestHelpers(t *testing.T) {
	if n, ok := NumericInt(float64(42)); !ok || n != 42 {
		t.Fatalf("NumericInt(float64) = %d, %v", n, ok)
	}
	if _, ok := NumericInt("42"); ok {
		t.Fatal("strings are not numeric ints")
	}
	if HasToolUseBlocks([]any{map[string]any{"type": "text"}}) {
		t.Fatal("text-only content has no tool_use")
	}
	if !HasToolUseBlocks([]any{map[string]any{"type": "tool_use", "id": "toolu_1"}}) {
		t.Fatal("tool_use block not detected")
	}
	if !strings.HasPrefix(GenToolUseID(), "toolu_") {
		t.Fatal("unexpected tool_use id")
	}
}
