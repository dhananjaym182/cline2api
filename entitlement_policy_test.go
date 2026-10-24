package main

import (
	"strings"
	"testing"
)

func TestModelFreeOnlyEligibility(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
	})
	pool = &AccountPool{Models: []Model{
		{ID: "vendor/free-model", Cost: "free", TierSignal: "suffix-free", Source: "provider"},
		{ID: "vendor/legacy-free", Cost: "free", Source: "remote"},
		{ID: "vendor/provider-flag", Cost: "free", TierSignal: tierSignalProviderFree, Source: "provider"},
		{ID: "vendor/paid-model", Cost: "pass", TierSignal: "pricing-paid", Source: "provider"},
		{ID: "cline-pass/model", Cost: "pass", Source: "remote"},
	}}
	cfg := defaultProxyConfig()
	cfg.OnlyFree = true
	setProxyConfig(cfg)

	cases := []struct {
		model string
		want  bool
	}{
		{"free", true},
		{"vendor/free-model", true},
		{"vendor/legacy-free", true},
		{"opencode/vendor/free-model", true},
		{"vendor/provider-flag", false},
		{"vendor/paid-model", false},
		{"cline-pass/model", false},
		{"unknown/model", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := modelFreeOnlyEligible(tc.model); got != tc.want {
			t.Errorf("modelFreeOnlyEligible(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}

	if msg := freeOnlyRejection("vendor/paid-model"); !strings.Contains(msg, "not eligible") {
		t.Fatalf("paid model should be rejected, got %q", msg)
	}
	if msg := freeOnlyRejection("vendor/provider-flag"); !strings.Contains(msg, "unknown") {
		t.Fatalf("provider-level free flag must be unknown, got %q", msg)
	}
	if msg := freeOnlyRejection("vendor/free-model"); msg != "" {
		t.Fatalf("free model should pass, got %q", msg)
	}

	cfg.OnlyFree = false
	setProxyConfig(cfg)
	if msg := freeOnlyRejection("vendor/paid-model"); msg != "" {
		t.Fatalf("Free-only policy must not apply when onlyFree is disabled, got %q", msg)
	}
}
