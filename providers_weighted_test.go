package main

import (
	"testing"
)

func weightedFixture(t *testing.T) {
	t.Helper()
	cfg := defaultProxyConfig()
	cfg.Strategy = "weighted_round_robin"
	setProxyConfig(cfg)
	t.Cleanup(func() {
		setProxyConfig(defaultProxyConfig())
		providerWRRMu.Lock()
		providerWRRState = map[string]float64{}
		providerWRRMu.Unlock()
	})
}

func TestOrderProviderCandsWeightedKeepsProportion(t *testing.T) {
	weightedFixture(t)
	a := providerCand{p: CustomProvider{ID: "a", Weight: 3}}
	b := providerCand{p: CustomProvider{ID: "b", Weight: 1}}

	first := map[string]int{}
	for i := 0; i < 12; i++ {
		ordered := orderProviderCands([]providerCand{a, b}, "m1")
		if len(ordered) != 2 {
			t.Fatalf("round %d lost failover candidates: %v", i, ordered)
		}
		first[ordered[0].p.ID]++
	}
	if first["a"] != 9 || first["b"] != 3 {
		t.Fatalf("weighted distribution = %v, want a=9 b=3", first)
	}
}

func TestOrderProviderCandsWeightedDefaultsAndExcludes(t *testing.T) {
	weightedFixture(t)
	zero := providerCand{p: CustomProvider{ID: "zero"}}
	neg := providerCand{p: CustomProvider{ID: "neg", Weight: -1}}

	ordered := orderProviderCands([]providerCand{zero, neg}, "m2")
	if len(ordered) != 1 || ordered[0].p.ID != "zero" {
		t.Fatalf("zero weight = 1 and negative weight excluded, got %+v", ordered)
	}

	all := orderProviderCands([]providerCand{neg}, "m2")
	if all != nil {
		t.Fatalf("all-negative candidate set must return nil, got %+v", all)
	}
}
