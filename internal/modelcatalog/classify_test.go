package modelcatalog

import "testing"

func f(v float64) *float64 { return &v }

func TestFreeWord(t *testing.T) {
	cases := map[string]bool{
		"free":              true,
		"cline-free/x":      true,
		"x-free":            true,
		"x:free:y":          true,
		"Flash (Free)":      true,
		"freemap":           false,
		"freemium-model":    false,
		"freedom":           false,
		"":                  false,
		"model-with-free-x": true,
	}
	for in, want := range cases {
		if got := FreeWord(in); got != want {
			t.Errorf("FreeWord(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestClassifySignals(t *testing.T) {
	allow := map[string]bool{"seed/model-a": true}
	cases := []struct {
		name         string
		entry        Entry
		providerFree bool
		wantTier     string
		wantSignal   string
	}{
		{"zero pricing", Entry{ID: "paid-ish", PricingPrompt: f(0), PricingComplet: f(0)}, false, "free", TierSignalPricingZero},
		{"zero pricing with request fee", Entry{ID: "x", PricingPrompt: f(0), PricingComplet: f(0), PricingRequest: f(0.5)}, false, "pass", TierSignalPricingPaid},
		{"free flag", Entry{ID: "x", FreeFlag: true}, false, "free", TierSignalFlagFree},
		{"mode free", Entry{ID: "x", Mode: "FREE"}, false, "free", TierSignalFlagFree},
		{"suffix colon", Entry{ID: "x:free"}, false, "free", TierSignalSuffixFree},
		{"suffix dash", Entry{ID: "x-FREE"}, false, "free", TierSignalSuffixFree},
		{"name token", Entry{ID: "ns/free-model"}, false, "free", TierSignalNameToken},
		{"display name", Entry{ID: "x", Name: "Model (Free)"}, false, "free", TierSignalDisplayName},
		{"tag", Entry{ID: "x", Tags: []string{"FREE"}}, false, "free", TierSignalAllowlist},
		{"allowlist", Entry{ID: "SEED/MODEL-A"}, false, "free", TierSignalAllowlist},
		{"pricing beats provider free", Entry{ID: "x", PricingPrompt: f(1)}, true, "pass", TierSignalPricingPaid},
		{"provider free flag", Entry{ID: "x"}, true, "free", TierSignalProviderFree},
		{"unknown defaults to pass", Entry{ID: "x"}, false, "pass", TierSignalPaidDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, signal := Classify(tc.entry, tc.providerFree, allow)
			if tier != tc.wantTier || signal != tc.wantSignal {
				t.Fatalf("got %q/%q, want %q/%q", tier, signal, tc.wantTier, tc.wantSignal)
			}
		})
	}
}

func TestClassifyDoesNotTreatFreemiumAsFree(t *testing.T) {
	if tier, _ := Classify(Entry{ID: "freemium-model"}, false, nil); tier != "pass" {
		t.Fatalf("tier = %q, want pass", tier)
	}
}
