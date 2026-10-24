package dispatch

import (
	"testing"

	"cline-go-proxy/internal/domain"
)

func TestFreeOnlyEligible(t *testing.T) {
	cases := []struct {
		name  string
		route RouteAccess
		want  bool
	}{
		{"free", RouteAccess{Class: domain.AccessFree}, true},
		{"free with allowance", RouteAccess{Class: domain.AccessFree, AllowanceVerified: true}, true},
		{"free_tier unverified", RouteAccess{Class: domain.AccessFreeTier}, false},
		{"free_tier verified", RouteAccess{Class: domain.AccessFreeTier, AllowanceVerified: true}, true},
		{"subscription", RouteAccess{Class: domain.AccessSubscription, AllowanceVerified: true}, false},
		{"paid api", RouteAccess{Class: domain.AccessPaidAPI, AllowanceVerified: true}, false},
		{"trial", RouteAccess{Class: domain.AccessTrial, AllowanceVerified: true}, false},
		{"unknown", RouteAccess{Class: domain.AccessUnknown, AllowanceVerified: true}, false},
		{"invalid class", RouteAccess{Class: domain.AccessClass("free-ish"), AllowanceVerified: true}, false},
	}
	for _, tc := range cases {
		if got := FreeOnlyEligible(tc.route); got != tc.want {
			t.Errorf("%s: FreeOnlyEligible = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestModelAccessClass(t *testing.T) {
	cases := []struct {
		name   string
		cost   string
		signal string
		source string
		want   domain.AccessClass
	}{
		{"free model", "free", "suffix-free", "provider", domain.AccessFree},
		{"legacy free model", "free", "", "", domain.AccessFree},
		{"provider-level free flag", "free", "provider-free", "provider", domain.AccessUnknown},
		{"cline pass", "pass", "", "remote", domain.AccessSubscription},
		{"provider paid", "pass", "", "provider", domain.AccessPaidAPI},
		{"zen paid", "pass", "", "zen", domain.AccessPaidAPI},
		{"missing cost", "", "", "", domain.AccessUnknown},
	}
	for _, tc := range cases {
		if got := ModelAccessClass(tc.cost, tc.signal, tc.source); got != tc.want {
			t.Errorf("%s: class = %q, want %q", tc.name, got, tc.want)
		}
	}
}
