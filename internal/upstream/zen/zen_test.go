package zen

import (
	"strings"
	"testing"
)

func TestIdentifierShape(t *testing.T) {
	g := NewIdentifier()
	id := g.New("ses", true)
	parts := strings.SplitN(id, "_", 2)
	if len(parts) != 2 || parts[0] != "ses" {
		t.Fatalf("id = %q", id)
	}
	if len(parts[1]) != 26 {
		t.Fatalf("suffix length = %d (%q)", len(parts[1]), parts[1])
	}
	for _, r := range parts[1] {
		if !strings.ContainsRune(base62Alphabet, r) && !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("unexpected character %q in %q", r, id)
		}
	}
	if other := g.New("msg", false); !strings.HasPrefix(other, "msg_") {
		t.Fatalf("prefix not honored: %q", other)
	}
}

func TestRandHelpers(t *testing.T) {
	if got := RandHex(8); len(got) != 16 {
		t.Fatalf("RandHex length = %d", len(got))
	}
	for i := 0; i < 20; i++ {
		if n := RandIntn(7); n < 0 || n >= 7 {
			t.Fatalf("RandIntn out of range: %d", n)
		}
	}
	if RandIntn(0) != 0 {
		t.Fatal("RandIntn(0) must be 0")
	}
	if got := WithRetryJitter(-1); got != -1 {
		t.Fatalf("non-positive delay must pass through, got %v", got)
	}
	if got := WithRetryJitter(100); got < 100 || got > 125 {
		t.Fatalf("jitter out of range: %v", got)
	}
}

func TestConversationSeedAndSessions(t *testing.T) {
	params := map[string]any{
		"metadata": map[string]any{"session_id": "session-1"},
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	if got := ConversationSeed(params); got != "session-1" {
		t.Fatalf("seed = %q", got)
	}
	noMeta := map[string]any{"messages": []any{
		map[string]any{"role": "system", "content": "sys"},
		map[string]any{"role": "user", "content": "first"},
	}}
	if got := ConversationSeed(noMeta); got != `"first"` {
		t.Fatalf("seed = %q", got)
	}
	if got := ConversationSeed(map[string]any{}); got != "" {
		t.Fatalf("empty seed = %q", got)
	}

	cache := NewSessionCache()
	s1 := cache.SessionID(params)
	s2 := cache.SessionID(params)
	if s1 != s2 {
		t.Fatalf("same conversation must reuse the session: %q vs %q", s1, s2)
	}
	if !strings.HasPrefix(s1, "ses_") {
		t.Fatalf("session = %q", s1)
	}
	if other := cache.SessionID(map[string]any{}); other == "" || strings.HasPrefix(other, "ses__") {
		t.Fatalf("random session = %q", other)
	}
}

func TestUserAgent(t *testing.T) {
	ua := UserAgent("responses")
	if !strings.Contains(ua, "opencode/"+ClientVersion) || !strings.Contains(ua, "provider-utils/4.0.40") {
		t.Fatalf("ua = %q", ua)
	}
	if ua := UserAgent("unknown"); !strings.Contains(ua, "provider-utils/4.0.23") {
		t.Fatalf("fallback ua = %q", ua)
	}
}

func TestEnsureAnonymousTools(t *testing.T) {
	body := map[string]any{}
	EnsureAnonymousTools(body)
	tools := body["tools"].([]any)
	if len(tools) != len(AnonymousCoreTools) {
		t.Fatalf("tools = %d", len(tools))
	}

	body = map[string]any{"tools": []any{
		map[string]any{"type": "function", "function": map[string]any{"name": "read"}},
	}}
	EnsureAnonymousTools(body)
	tools = body["tools"].([]any)
	if len(tools) != len(AnonymousCoreTools) {
		t.Fatalf("tools = %d", len(tools))
	}
	found := 0
	for _, item := range tools {
		fn := item.(map[string]any)["function"].(map[string]any)
		if fn["name"] == "read" {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("declared read tool duplicated: %d", found)
	}

	body = map[string]any{"tools": "not-a-list"}
	EnsureAnonymousTools(body)
	if body["tools"] != "not-a-list" {
		t.Fatal("non-array tools must be left untouched")
	}
}

func TestBuildChatBody(t *testing.T) {
	params := map[string]any{
		"model":            "opencode/zen-free",
		"max_tokens":       4096,
		"temperature":      0.2,
		"reasoning_effort": "high",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "assistant", "tool_calls": []any{
				map[string]any{"id": "call_bad", "function": map[string]any{"name": ""}},
			}},
		},
	}
	opts := BodyOptions{
		PassThroughKeys: []string{"temperature", "tools"},
		ResolveModel: func(model string) (string, bool) {
			if model == "opencode/zen-free" {
				return "zen-free", true
			}
			return "", false
		},
	}
	body := BuildChatBody(params, false, true, opts)
	if body["model"] != "zen-free" || body["max_tokens"] != 4096 || body["temperature"] != 0.2 {
		t.Fatalf("body = %v", body)
	}
	if body["stream"] != true {
		t.Fatal("anonymous requests must force streaming")
	}
	if so, _ := body["stream_options"].(map[string]any); so["include_usage"] != true {
		t.Fatalf("stream_options = %v", body["stream_options"])
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Fatal("reasoning_effort must be removed")
	}
	msgs := body["messages"].([]any)
	if _, ok := msgs[1].(map[string]any)["tool_calls"]; ok {
		t.Fatal("invalid tool_calls must be removed")
	}
	if tools, _ := body["tools"].([]any); len(tools) != len(AnonymousCoreTools) {
		t.Fatalf("core tools missing: %v", body["tools"])
	}

	body = BuildChatBody(map[string]any{"model": "opencode/other"}, false, false, BodyOptions{
		ResolveModel: func(string) (string, bool) { return "", false },
	})
	if body["model"] != "other" {
		t.Fatalf("model = %v", body["model"])
	}
	if body["stream"] != false {
		t.Fatal("non-anonymous non-stream request must stay non-streaming")
	}
	if _, ok := body["stream_options"]; ok {
		t.Fatal("stream_options only applies to anonymous streams")
	}
}
