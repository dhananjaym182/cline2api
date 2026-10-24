package main

import (
	"encoding/json"
	"testing"
)

func mustMsgs(t *testing.T, s string) []any {
	t.Helper()
	var out []any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

// 空名 tool_call 被剔除，合法 tool_call 保留
func TestSanitizeMessagesDropsEmptyNameToolCalls(t *testing.T) {
	in := mustMsgs(t, `[
		{"role":"user","content":"hi"},
		{"role":"assistant","tool_calls":[
			{"id":"a1","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"ls\"}"}},
			{"id":"a2","type":"function","function":{"name":"","arguments":"{}"}}
		]},
		{"role":"tool","tool_call_id":"a1","content":"ok"}
	]`)
	out := sanitizeMessages(in)
	if len(out) != 3 {
		t.Fatalf("want 3 msgs, got %d", len(out))
	}
	tcs := out[1].(map[string]any)["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("want 1 tool_call kept, got %d", len(tcs))
	}
	fn := tcs[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "Bash" {
		t.Fatalf("wrong tool_call kept: %v", fn)
	}
}

// 全部为空名时移除 tool_calls 字段
func TestSanitizeMessagesRemovesEmptyToolCallsField(t *testing.T) {
	in := mustMsgs(t, `[
		{"role":"assistant","tool_calls":[
			{"id":"a2","type":"function","function":{"name":"","arguments":"{}"}}
		]}
	]`)
	out := sanitizeMessages(in)
	if len(out) != 1 {
		t.Fatalf("want 1 msg, got %d", len(out))
	}
	if _, exists := out[0].(map[string]any)["tool_calls"]; exists {
		t.Fatal("tool_calls field should be removed when all entries are empty-name")
	}
}

// 孤儿 tool 结果（无对应合法 assistant tool_call）被丢弃
func TestSanitizeMessagesDropsOrphanToolResults(t *testing.T) {
	in := mustMsgs(t, `[
		{"role":"user","content":"hi"},
		{"role":"tool","tool_call_id":"ghost","content":"orphan result"},
		{"role":"tool","tool_call_id":"","content":"no id"}
	]`)
	out := sanitizeMessages(in)
	if len(out) != 1 {
		t.Fatalf("want only user msg, got %d msgs: %v", len(out), out)
	}
}

// 正常历史不受影响
func TestSanitizeMessagesKeepsValidHistory(t *testing.T) {
	in := mustMsgs(t, `[
		{"role":"system","content":"sys"},
		{"role":"user","content":"q"},
		{"role":"assistant","tool_calls":[{"id":"a1","type":"function","function":{"name":"Read","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"a1","content":"data"}
	]`)
	out := sanitizeMessages(in)
	if len(out) != 4 {
		t.Fatalf("valid history should be untouched, got %d msgs", len(out))
	}
}
