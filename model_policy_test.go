package main

import (
	"fmt"
	"strings"
	"testing"

	"cline-go-proxy/internal/config"
	"cline-go-proxy/internal/domain"
)

func policyTestPool(models ...Model) func() {
	oldPool := pool
	pool = &AccountPool{Models: models}
	return func() { pool = oldPool }
}

func TestResolveClientModelAlias(t *testing.T) {
	defer policyTestPool(Model{ID: "gpt-5", Provider: "openai"})()

	cfg := defaultProxyConfig()
	cfg.ModelAliases = []config.ModelAlias{{Alias: "fast", Target: "gpt-5", Provider: "OpenRouter"}}
	setProxyConfig(cfg)
	defer setProxyConfig(defaultProxyConfig())

	got, reject := resolveClientModel("fast")
	if reject != "" || got != "gpt-5@OpenRouter" {
		t.Fatalf("alias rewrite = %q reject=%q, want gpt-5@OpenRouter", got, reject)
	}
}

func TestResolveClientModelAliasShadowing(t *testing.T) {
	defer policyTestPool(Model{ID: "fast", Provider: "openai"}, Model{ID: "gpt-5"})()

	cfg := defaultProxyConfig()
	cfg.ModelAliases = []config.ModelAlias{
		{Alias: "fast", Target: "gpt-5"},
		{Alias: "strong", Target: "gpt-5", Force: true},
	}
	setProxyConfig(cfg)
	defer setProxyConfig(defaultProxyConfig())

	if got, _ := resolveClientModel("fast"); got != "fast" {
		t.Fatalf("non-forced alias must not shadow a real model, got %q", got)
	}
	if got, _ := resolveClientModel("strong"); got != "gpt-5" {
		t.Fatalf("forced alias should rewrite, got %q", got)
	}
}

func TestResolveClientModelExclusion(t *testing.T) {
	defer policyTestPool(Model{ID: "gpt-5"}, Model{ID: "gpt-4-mini"})()

	cfg := defaultProxyConfig()
	cfg.ExcludedModels = []string{"gpt-4*"}
	cfg.ModelAliases = []config.ModelAlias{{Alias: "cheap", Target: "gpt-4-mini"}}
	setProxyConfig(cfg)
	defer setProxyConfig(defaultProxyConfig())

	if _, reject := resolveClientModel("gpt-4o"); reject == "" {
		t.Fatal("wildcard exclusion must reject gpt-4o")
	}
	if _, reject := resolveClientModel("gpt-5"); reject != "" {
		t.Fatalf("allowed model rejected: %v", reject)
	}
	// Exclusion applies after alias resolution too.
	if _, reject := resolveClientModel("cheap"); !strings.Contains(reject, "excluded") {
		t.Fatalf("alias to excluded target must reject, got %q", reject)
	}
}

func TestResolveClientModelCombosAndFreeUntouched(t *testing.T) {
	defer policyTestPool()()

	cfg := defaultProxyConfig()
	cfg.Combos = []domain.ModelCombo{{ID: "my-combo", Name: "x", Enabled: true, Entries: []domain.ComboEntry{{Model: "gpt-5"}}}}
	cfg.ModelAliases = []config.ModelAlias{{Alias: "my-combo", Target: "other", Force: true}}
	setProxyConfig(cfg)
	defer setProxyConfig(defaultProxyConfig())

	if got, _ := resolveClientModel("my-combo"); got != "my-combo" {
		t.Fatalf("combo id must not be alias-rewritten, got %q", got)
	}
	if got, _ := resolveClientModel("free"); got != "free" {
		t.Fatalf("free alias must survive, got %q", got)
	}
}

func TestApplyThinkingPolicy(t *testing.T) {
	defer policyTestPool(Model{ID: "gpt-5", ThinkingLevels: []string{"low", "medium"}})()

	params := map[string]any{"model": "gpt-5", "reasoning_effort": "high"}
	applyThinkingPolicy(params, "gpt-5")
	if _, ok := params["reasoning_effort"]; ok {
		t.Fatal("unsupported effort must be dropped")
	}

	params = map[string]any{"model": "gpt-5", "reasoning": map[string]any{"effort": "ultra"}}
	applyThinkingPolicy(params, "gpt-5")
	if _, ok := params["reasoning"]; ok {
		t.Fatal("nested unsupported effort must be dropped")
	}

	params = map[string]any{"model": "gpt-5", "reasoning_effort": "Low"}
	applyThinkingPolicy(params, "gpt-5")
	if params["reasoning_effort"] != "Low" {
		t.Fatalf("supported effort (case-insensitive) must survive, got %v", params["reasoning_effort"])
	}

	params = map[string]any{"model": "unknown", "reasoning_effort": "high"}
	applyThinkingPolicy(params, "unknown")
	if params["reasoning_effort"] != "high" {
		t.Fatal("models without declared levels must keep the client effort")
	}
}

func TestBuildModelsListAppliesPolicy(t *testing.T) {
	defer policyTestPool(
		Model{ID: "gpt-5", ThinkingLevels: []string{"low"}, InputModalities: []string{"text", "image"}},
		Model{ID: "gpt-4-mini"},
	)()

	cfg := defaultProxyConfig()
	cfg.ExcludedModels = []string{"gpt-4-*"}
	cfg.ModelAliases = []config.ModelAlias{{Alias: "fast", Target: "gpt-5"}}
	setProxyConfig(cfg)
	defer setProxyConfig(defaultProxyConfig())

	list := buildModelsList(false)
	var ids []string
	var entry map[string]any
	for _, e := range list {
		id, _ := e["id"].(string)
		ids = append(ids, id)
		if id == "gpt-5" {
			entry = e
		}
	}
	joined := strings.Join(ids, ",")
	if strings.Contains(joined, "gpt-4-mini") {
		t.Fatalf("excluded model must not be listed: %v", ids)
	}
	if !strings.Contains(joined, "fast") {
		t.Fatalf("alias entry missing: %v", ids)
	}
	if entry == nil || fmt.Sprint(entry["thinking_levels"]) != "[low]" || fmt.Sprint(entry["input_modalities"]) != "[text image]" {
		t.Fatalf("capability metadata not exposed: %+v", entry)
	}
}
