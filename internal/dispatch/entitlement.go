// Package dispatch owns routing decisions. This file holds the Free-only
// entitlement policy: an access class is attached to a route, not to a model
// ID globally, and unverified routes never qualify silently.
package dispatch

import "cline-go-proxy/internal/domain"

// RouteAccess describes one concrete route candidate: the account/provider
// route through which a model would be served.
type RouteAccess struct {
	Class domain.AccessClass
	// AllowanceVerified is true only when the selected account or route has a
	// verified active free-tier allowance.
	AllowanceVerified bool
}

// FreeOnlyEligible reports whether a route may serve a request while Free-only
// routing is enabled. free is always eligible; free_tier only with a verified
// allowance; every other class is ineligible.
func FreeOnlyEligible(r RouteAccess) bool {
	if !r.Class.Valid() {
		return false
	}
	return r.Class.EligibleForFreeOnly(r.AllowanceVerified)
}

// ModelAccessClass maps catalog metadata to an access class. A provider-level
// free flag ("provider-free" signal) is metadata, not per-model entitlement, so
// it stays unknown. Zen models are free by construction; other paid costs are
// treated as paid API access.
func ModelAccessClass(cost, tierSignal, source string) domain.AccessClass {
	if tierSignal == "provider-free" {
		return domain.AccessUnknown
	}
	switch cost {
	case "free":
		return domain.AccessFree
	case "pass":
		if source == "remote" {
			return domain.AccessSubscription
		}
		return domain.AccessPaidAPI
	default:
		return domain.AccessUnknown
	}
}
