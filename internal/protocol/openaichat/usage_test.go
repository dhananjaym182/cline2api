package openaichat

import (
	"encoding/json"
	"testing"
)

func mustObj(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return m
}

func TestParseUsageOpenAIShape(t *testing.T) {
	u := ParseUsage(mustObj(t, `{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`))
	if !u.Valid || u.Prompt != 10 || u.Completion != 5 || u.Total != 15 {
		t.Fatalf("usage = %+v", u)
	}
}

func TestParseUsageAnthropicShape(t *testing.T) {
	u := ParseUsage(mustObj(t, `{"input_tokens":8,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":1}`))
	if !u.Valid || u.Prompt != 8 || u.Completion != 2 {
		t.Fatalf("usage = %+v", u)
	}
	if u.Cached != 4 {
		t.Fatalf("cached = %d, want 4", u.Cached)
	}
	if u.Total != 10 {
		t.Fatalf("total = %d, want prompt+completion", u.Total)
	}
}

func TestParseUsageNestedDetails(t *testing.T) {
	u := ParseUsage(mustObj(t, `{"input_tokens":8,"input_tokens_details":{"cached_tokens":6}}`))
	if u.Cached != 6 {
		t.Fatalf("cached = %d", u.Cached)
	}
}

func TestParseUsageInvalid(t *testing.T) {
	if u := ParseUsage(nil); u.Valid {
		t.Fatal("nil usage must be invalid")
	}
	if u := ParseUsage(mustObj(t, `{"foo":1}`)); u.Valid {
		t.Fatal("unknown keys must be invalid")
	}
	if u := ParseUsage(mustObj(t, `{"prompt_tokens":-1}`)); !u.Valid || u.Prompt != 0 {
		t.Fatalf("negative must clamp to 0 but stay valid: %+v", u)
	}
}

func TestMergeUsage(t *testing.T) {
	base := Usage{Prompt: 1}
	if got := MergeUsage(base, Usage{}); got != base {
		t.Fatalf("invalid frame changed usage: %+v", got)
	}
	got := MergeUsage(Usage{Prompt: 10}, Usage{Completion: 5, Valid: true})
	if got.Prompt != 10 || got.Completion != 5 || got.Total != 15 || !got.Valid {
		t.Fatalf("merged = %+v", got)
	}
	got = MergeUsage(Usage{Prompt: 10, Valid: true}, Usage{Prompt: 12, Valid: true})
	if got.Prompt != 12 {
		t.Fatalf("later non-zero must win: %+v", got)
	}
}

func TestHasFirstOutput(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{`{"choices":[{"delta":{"role":"assistant"}}]}`, false},
		{`{"choices":[{"delta":{"content":"hi"}}]}`, true},
		{`{"choices":[{"delta":{"tool_calls":[{"index":0}]}}]}`, true},
		{`{"choices":[{"message":{"content":"hi"}}]}`, true},
		{`{"choices":[]}`, false},
		{`{"choices":[{"delta":null}]}`, false},
		{`{}`, false},
	}
	for _, c := range cases {
		if got := HasFirstOutput(mustObj(t, c.raw)); got != c.want {
			t.Errorf("%s => %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestUnwrapDataEnvelope(t *testing.T) {
	inner := mustObj(t, `{"id":"x","choices":[]}`)
	if got := UnwrapDataEnvelope(mustObj(t, `{"data":{"id":"x","choices":[]}}`)); got["id"] != inner["id"] {
		t.Fatalf("envelope not unwrapped: %v", got)
	}
	plain := mustObj(t, `{"id":"x","choices":[]}`)
	if got := UnwrapDataEnvelope(plain); got["id"] != "x" {
		t.Fatalf("plain object altered: %v", got)
	}
	other := mustObj(t, `{"data":{"unrelated":true}}`)
	if got := UnwrapDataEnvelope(other); got["data"] == nil {
		t.Fatalf("non-chat envelope must pass through: %v", got)
	}
}
