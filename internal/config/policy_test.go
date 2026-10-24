package config

import (
	"encoding/json"
	"testing"
)

func TestProxyNormalizePolicyDefaults(t *testing.T) {
	c := DefaultProxy()
	c.Strategy = "weighted_round_robin"
	c.Stream.BootstrapRetries = -3
	c.MaxRequestBodyBytes = -1
	c.Normalize()

	if c.Strategy != "weighted_round_robin" {
		t.Fatalf("strategy = %q", c.Strategy)
	}
	if c.MaxRequestBodyBytes != 0 {
		t.Fatalf("max body = %d", c.MaxRequestBodyBytes)
	}
	if c.Stream.BootstrapRetries != 0 {
		t.Fatalf("bootstrap retries = %d", c.Stream.BootstrapRetries)
	}
	if c.Stream.BootstrapBufferBytes != 1<<20 || c.Stream.BootstrapBufferFrames != 64 {
		t.Fatalf("buffer defaults = %d bytes / %d frames", c.Stream.BootstrapBufferBytes, c.Stream.BootstrapBufferFrames)
	}
	if c.SessionAffinity.TTLSeconds != 3600 {
		t.Fatalf("affinity ttl = %d", c.SessionAffinity.TTLSeconds)
	}
}

func TestProxyNormalizeDropsUnusablePolicies(t *testing.T) {
	c := DefaultProxy()
	c.ModelAliases = []ModelAlias{
		{Alias: "fast", Target: "gpt-5"},
		{Alias: " fast ", Target: "gpt-5.4"},
		{Alias: "", Target: "x"},
	}
	c.ExcludedModels = []string{" *-thinking ", ""}
	c.RequestErrorRules = []RequestErrorRule{
		{StatusCodes: []int{400}, Match: "bad", Action: "STOP"},
		{StatusCodes: []int{400}, Match: "[", Regex: true},
		{StatusCodes: []int{400}, Match: "x", Action: "explode"},
	}
	c.Normalize()

	if len(c.ModelAliases) != 1 || c.ModelAliases[0].Target != "gpt-5" {
		t.Fatalf("aliases = %+v", c.ModelAliases)
	}
	if len(c.ExcludedModels) != 1 || c.ExcludedModels[0] != "*-thinking" {
		t.Fatalf("exclusions = %+v", c.ExcludedModels)
	}
	if len(c.RequestErrorRules) != 1 || c.RequestErrorRules[0].Action != ErrorActionStop {
		t.Fatalf("rules = %+v", c.RequestErrorRules)
	}
}

func TestExcludesModelWildcards(t *testing.T) {
	c := DefaultProxy()
	c.ExcludedModels = []string{"*-thinking*", "gpt-4?"}
	cases := map[string]bool{
		"gpt-5-thinking-high": true,
		"gpt-4o":              true,
		"gpt-4oo":             false,
		"claude-sonnet":       false,
	}
	for id, want := range cases {
		if got := c.ExcludesModel(id); got != want {
			t.Errorf("ExcludesModel(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestAliasForAndValidate(t *testing.T) {
	c := DefaultProxy()
	c.ModelAliases = []ModelAlias{{Alias: "fast", Target: "gpt-5", Provider: "OpenRouter"}}
	if a, ok := c.AliasFor("fast"); !ok || a.Target != "gpt-5" {
		t.Fatalf("alias = %+v ok=%v", a, ok)
	}
	if err := c.ValidateModelPolicies(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	c.ModelAliases = append(c.ModelAliases, ModelAlias{Alias: "fast", Target: "other"})
	if err := c.ValidateModelPolicies(); err == nil {
		t.Fatal("duplicate alias must fail validation")
	}
}

func TestPolicyJSONRoundTrip(t *testing.T) {
	c := DefaultProxy()
	c.Retry.MaxRounds = 2
	c.Stream.FirstContentTimeoutSeconds = 45
	c.SessionAffinity = SessionAffinityPolicy{Enabled: true, TTLSeconds: 600}
	c.PassthroughHeaders = true
	c.MaxRequestBodyBytes = 8 << 20
	c.NonstreamKeepaliveSeconds = 10
	c.ExcludedModels = []string{"debug-*"}
	c.Normalize()

	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back Proxy
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	back.Normalize()
	if back.Retry.MaxRounds != 2 || back.Stream.FirstContentTimeoutSeconds != 45 ||
		!back.SessionAffinity.Enabled || back.SessionAffinity.TTLSeconds != 600 ||
		!back.PassthroughHeaders || back.MaxRequestBodyBytes != 8<<20 ||
		back.NonstreamKeepaliveSeconds != 10 || len(back.ExcludedModels) != 1 {
		t.Fatalf("round trip lost fields: %+v", back)
	}
}

func TestMatchRequestError(t *testing.T) {
	c := DefaultProxy()
	c.RequestErrorRules = []RequestErrorRule{
		{StatusCodes: []int{429}, Match: "rate limit", Action: ErrorActionStopAndCooldown},
		{StatusCodes: []int{400}, Regex: true, Match: `invalid .* field`, Action: ErrorActionStop},
		{Match: "overloaded_error", Action: ErrorActionContinue},
		{Action: ErrorActionStop}, // catch-all: any unmatched error stops
	}
	c.Normalize()

	if a, ok := c.MatchRequestError(429, "upstream rate limit exceeded"); !ok || a != ErrorActionStopAndCooldown {
		t.Fatalf("429 rate limit = %q ok=%v", a, ok)
	}
	if a, ok := c.MatchRequestError(429, "case-insensitive RATE LIMIT here"); !ok || a != ErrorActionStopAndCooldown {
		t.Fatalf("case-insensitive substring = %q ok=%v", a, ok)
	}
	if a, ok := c.MatchRequestError(400, "invalid input: bad field name"); !ok || a != ErrorActionStop {
		t.Fatalf("regex 400 = %q ok=%v", a, ok)
	}
	if a, ok := c.MatchRequestError(529, "server overloaded_error now"); !ok || a != ErrorActionContinue {
		t.Fatalf("overloaded continue = %q ok=%v", a, ok)
	}
	if a, ok := c.MatchRequestError(500, "totally unrelated"); !ok || a != ErrorActionStop {
		t.Fatalf("catch-all = %q ok=%v", a, ok)
	}

	rc := DefaultProxy()
	rc.RequestErrorRules = []RequestErrorRule{{StatusCodes: []int{400}, Regex: true, Match: `invalid .* field`, Action: ErrorActionStop}}
	rc.Normalize()
	if _, ok := rc.MatchRequestError(400, "invalid_request missing"); ok {
		t.Fatal("regex should not match without trailing 'field'")
	}
	if _, ok := rc.MatchRequestError(400, "invalid something field"); !ok {
		t.Fatal("regex should match with trailing 'field'")
	}
}

func TestMatchRequestErrorStatusOnlyAndEmpty(t *testing.T) {
	c := DefaultProxy()
	c.RequestErrorRules = []RequestErrorRule{{StatusCodes: []int{402}}}
	c.Normalize()
	if a, ok := c.MatchRequestError(402, "whatever"); !ok || a != ErrorActionContinue {
		t.Fatalf("status-only default action = %q ok=%v", a, ok)
	}
	if _, ok := c.MatchRequestError(500, "whatever"); ok {
		t.Fatal("unmatched status must not match")
	}

	empty := DefaultProxy()
	empty.Normalize()
	if _, ok := empty.MatchRequestError(500, "x"); ok {
		t.Fatal("no rules must never match")
	}
}
