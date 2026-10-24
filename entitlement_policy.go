package main

import (
	"fmt"
	"strings"

	"cline-go-proxy/internal/dispatch"
	"cline-go-proxy/internal/domain"
)

// modelRouteAccess resolves the access class of a model's current route from
// catalog metadata. Unknown models are unknown: they never silently qualify for
// Free-only routing.
func modelRouteAccess(id string) dispatch.RouteAccess {
	id = strings.TrimSpace(id)
	if id == "" {
		return dispatch.RouteAccess{Class: domain.AccessUnknown}
	}
	bare := strings.TrimPrefix(id, "opencode/")
	p := loadPool()
	poolMu.Lock()
	for _, m := range p.Models {
		if m.ID == bare {
			class := dispatch.ModelAccessClass(m.Cost, m.TierSignal, m.Source)
			poolMu.Unlock()
			return dispatch.RouteAccess{Class: class}
		}
	}
	poolMu.Unlock()
	if zm, ok := resolveZenInfo(id); ok {
		return dispatch.RouteAccess{Class: dispatch.ModelAccessClass(zm.Cost, zm.TierSignal, zm.Source)}
	}
	return dispatch.RouteAccess{Class: domain.AccessUnknown}
}

// modelFreeOnlyEligible reports whether a model may be used while Free-only
// routing is enabled. The "free" alias resolves to the explicit free chain.
func modelFreeOnlyEligible(id string) bool {
	if strings.TrimSpace(id) == "free" {
		return true
	}
	return dispatch.FreeOnlyEligible(modelRouteAccess(id))
}

// freeOnlyRejection returns a user-facing error when the requested model may
// not be served under Free-only routing. Named combos are validated per entry
// during execution instead.
func freeOnlyRejection(model string) string {
	if !getProxyConfig().OnlyFree || model == "" {
		return ""
	}
	if _, ok := findCombo(model); ok {
		return ""
	}
	if modelFreeOnlyEligible(model) {
		return ""
	}
	class := modelRouteAccess(model).Class
	return fmt.Sprintf("model %q is not eligible while onlyFree is enabled (access class: %s)", model, class)
}
