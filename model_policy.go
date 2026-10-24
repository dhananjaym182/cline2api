package main

import (
	"log"
	"strings"
	"time"
)

// Client-visible model policy: administrator aliases, wildcard exclusions, and
// per-model capability metadata. All three entry points (chat, Responses,
// Anthropic) funnel the requested id through resolveClientModel before it
// reaches routing, so the policy is enforced in the backend rather than in UI
// labels.

// resolveClientModel applies alias rewrites and exclusion patterns.
// It returns the id routing should use and a non-empty reason when the request
// must be rejected. The reserved "free" alias and named combos are never
// rewritten or excluded by policy (they are explicit admin constructs).
func resolveClientModel(raw string) (resolved string, reject string) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return id, ""
	}
	cfg := getProxyConfig()

	if _, isCombo := findCombo(id); !isCombo && id != "free" {
		if alias, ok := cfg.AliasFor(id); ok && (alias.Force || !catalogHasModel(id)) {
			target := alias.Target
			bare, pin := splitProviderPin(target)
			if pin == "" && alias.Provider != "" {
				target = bare + "@" + alias.Provider
			}
			id = target
		}
	}

	if cfg.ExcludesModel(id) || (id != raw && cfg.ExcludesModel(raw)) {
		return id, "model " + raw + " is excluded by administrator policy"
	}
	return id, ""
}

// catalogHasModel reports whether id is a real catalog model (so a non-forced
// alias only shadows unknown ids).
func catalogHasModel(id string) bool {
	id = strings.TrimPrefix(strings.TrimSpace(id), "opencode/")
	bare, _ := splitProviderPin(id)
	for _, m := range getAllModels() {
		if m.ID == bare || m.ID == id {
			return true
		}
	}
	return false
}

// modelEntryMeta returns the capability metadata stored for a model id.
func modelEntryMeta(id string) (Model, bool) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "opencode/")
	bare, _ := splitProviderPin(id)
	for _, m := range getAllModels() {
		if m.ID == bare || m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// applyThinkingPolicy drops reasoning-effort parameters the resolved model does
// not declare. A model without metadata keeps the client's request untouched.
func applyThinkingPolicy(params map[string]any, model string) {
	effort, _ := params["reasoning_effort"].(string)
	if effort == "" {
		if r, ok := params["reasoning"].(map[string]any); ok {
			effort, _ = r["effort"].(string)
		}
	}
	if effort == "" {
		return
	}
	m, ok := modelEntryMeta(model)
	if !ok || len(m.ThinkingLevels) == 0 {
		return
	}
	for _, level := range m.ThinkingLevels {
		if strings.EqualFold(level, effort) {
			return
		}
	}
	log.Printf("  thinking policy: model %q does not support effort %q, parameter dropped", model, effort)
	delete(params, "reasoning_effort")
	if r, ok := params["reasoning"].(map[string]any); ok {
		delete(r, "effort")
		if len(r) == 0 {
			delete(params, "reasoning")
		}
	}
}

// excludedForListing filters pool models, combos, and alias entries for
// /v1/models and admin listings.
func excludedForListing(id string) bool {
	return getProxyConfig().ExcludesModel(id)
}

// aliasListingEntries returns client-visible alias entries whose targets are
// not themselves excluded or disabled.
func aliasListingEntries() []map[string]any {
	cfg := getProxyConfig()
	out := make([]map[string]any, 0, len(cfg.ModelAliases))
	for _, a := range cfg.ModelAliases {
		if excludedForListing(a.Alias) || excludedForListing(a.Target) {
			continue
		}
		if isModelDisabled(a.Target) {
			continue
		}
		out = append(out, map[string]any{
			"id":       a.Alias,
			"object":   "model",
			"created":  time.Now().UnixMilli(),
			"owned_by": "alias",
			"alias_of": a.Target,
		})
	}
	return out
}
