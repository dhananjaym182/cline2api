package domain

import "testing"

func TestAccessClassValid(t *testing.T) {
	for _, c := range []AccessClass{AccessFree, AccessFreeTier, AccessSubscription, AccessPaidAPI, AccessTrial, AccessUnknown} {
		if !c.Valid() {
			t.Errorf("%q should be valid", c)
		}
	}
	if AccessClass("free-ish").Valid() {
		t.Error("unknown class reported valid")
	}
}

func TestEligibleForFreeOnly(t *testing.T) {
	cases := []struct {
		class    AccessClass
		verified bool
		eligible bool
	}{
		{AccessFree, false, true},
		{AccessFree, true, true},
		{AccessFreeTier, false, false},
		{AccessFreeTier, true, true},
		{AccessSubscription, true, false},
		{AccessPaidAPI, true, false},
		{AccessTrial, true, false},
		{AccessUnknown, false, false},
		{AccessUnknown, true, false},
	}
	for _, tc := range cases {
		if got := tc.class.EligibleForFreeOnly(tc.verified); got != tc.eligible {
			t.Errorf("%q verified=%v = %v, want %v", tc.class, tc.verified, got, tc.eligible)
		}
	}
}

func TestAccessClassForSignal(t *testing.T) {
	freeSignals := []string{
		"pricing-zero", "flag-free", "suffix-free", "name-token-free", "display-name-free", "allowlist",
	}
	for _, sig := range freeSignals {
		if got := AccessClassForSignal(sig); got != AccessFree {
			t.Errorf("%q -> %q, want free", sig, got)
		}
	}
	if got := AccessClassForSignal("pricing-paid"); got != AccessPaidAPI {
		t.Errorf("pricing-paid -> %q, want paid_api", got)
	}
	for _, sig := range []string{"provider-free", "paid-default", ""} {
		if got := AccessClassForSignal(sig); got != AccessUnknown {
			t.Errorf("%q -> %q, want unknown", sig, got)
		}
	}
}
