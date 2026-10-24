package openaichat

import (
	"testing"
)

func TestNormalizeMessage(t *testing.T) {
	got := NormalizeMessage(mustObj(t, `{"role":"assistant","content":"hi","provider_metadata":{"x":1},"proxy_metadata":{"y":2}}`))
	if _, ok := got["provider_metadata"]; ok {
		t.Fatal("provider_metadata not stripped")
	}
	if _, ok := got["proxy_metadata"]; ok {
		t.Fatal("proxy_metadata not stripped")
	}
	if got["content"] != "hi" {
		t.Fatalf("content = %v", got["content"])
	}
}

func TestNormalizeMessageToolCallsKeepContentKey(t *testing.T) {
	got := NormalizeMessage(mustObj(t, `{"role":"assistant","tool_calls":[{"id":"c1"}]}`))
	c, ok := got["content"]
	if !ok || c != "" {
		t.Fatalf("content = %v (ok=%v), want empty string present", c, ok)
	}
}

func TestNormalizeResponseStripsNestedMetadata(t *testing.T) {
	in := mustObj(t, `{"id":"c1","provider_metadata":{"a":1},"choices":[{
		"index":0,"proxy_metadata":{"b":2},
		"message":{"role":"assistant","content":"x","provider_metadata":{"c":3}},
		"delta":{"content":"y","provider_metadata":{"d":4}}}]}`)
	out := NormalizeResponse(in)
	if _, ok := out["provider_metadata"]; ok {
		t.Fatal("top-level metadata not stripped")
	}
	choice := out["choices"].([]any)[0].(map[string]any)
	if _, ok := choice["proxy_metadata"]; ok {
		t.Fatal("choice metadata not stripped")
	}
	if _, ok := choice["message"].(map[string]any)["provider_metadata"]; ok {
		t.Fatal("message metadata not stripped")
	}
	if _, ok := choice["delta"].(map[string]any)["provider_metadata"]; ok {
		t.Fatal("delta metadata not stripped")
	}
	if out["id"] != "c1" {
		t.Fatalf("id lost: %v", out["id"])
	}
}

func TestNormalizeResponseDeltaToolCallContent(t *testing.T) {
	in := mustObj(t, `{"choices":[{"delta":{"tool_calls":[{"index":0}]}}]}`)
	delta := NormalizeResponse(in)["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
	if c, ok := delta["content"]; !ok || c != "" {
		t.Fatalf("delta content = %v (ok=%v)", c, ok)
	}
}

func TestNormalizeResponseLeavesNonObjects(t *testing.T) {
	in := map[string]any{"choices": []any{"raw-string"}}
	out := NormalizeResponse(in)
	if out["choices"].([]any)[0] != "raw-string" {
		t.Fatalf("non-object choice altered: %v", out["choices"])
	}
	if got := NormalizeResponse(mustObj(t, `{"id":"x"}`)); got["id"] != "x" {
		t.Fatal("choices-less response changed")
	}
}
