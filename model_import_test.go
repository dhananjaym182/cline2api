package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func importJSONBody(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

func f64(v float64) *float64 { return &v }

func TestClassifyUpstreamModel(t *testing.T) {
	cases := []struct {
		name         string
		entry        upstreamModelEntry
		providerFree bool
		wantTier     string
		wantSignal   string
	}{
		{"pricing zero strings", upstreamModelEntry{ID: "vendor/big-model", PricingPrompt: f64(0), PricingComplet: f64(0)}, false, "free", tierSignalPricingZero},
		{"pricing paid outranks provider-free", upstreamModelEntry{ID: "vendor/big-model", PricingPrompt: f64(0.1), PricingComplet: f64(0.4)}, true, "pass", tierSignalPricingPaid},
		{"variable -1 pricing is paid", upstreamModelEntry{ID: "kilo-auto/efficient", PricingPrompt: f64(-1), PricingComplet: f64(-1)}, true, "pass", tierSignalPricingPaid},
		{"one-sided nonzero pricing is paid", upstreamModelEntry{ID: "vendor/half", PricingPrompt: f64(0), PricingComplet: f64(0.2)}, true, "pass", tierSignalPricingPaid},
		{"suffix colon free", upstreamModelEntry{ID: "deepseek/deepseek-chat:free"}, false, "free", tierSignalSuffixFree},
		{"suffix dash free", upstreamModelEntry{ID: "some-host/nemotron-3-ultra-free"}, false, "free", tierSignalSuffixFree},
		{"tag free", upstreamModelEntry{ID: "host/model-x", Tags: []string{"image-input", "Free"}}, false, "free", tierSignalAllowlist},
		{"allowlist seed", upstreamModelEntry{ID: "deepseek-v4-flash"}, false, "free", tierSignalAllowlist},
		{"nonzero pricing → paid with provenance", upstreamModelEntry{ID: "openai/gpt-5-turbo", PricingPrompt: f64(1.25), PricingComplet: f64(10)}, false, "pass", tierSignalPricingPaid},
		{"unknown payload → paid", upstreamModelEntry{ID: "brand/new-thing"}, false, "pass", tierSignalPaidDefault},
		{"free bool flag", upstreamModelEntry{ID: "vendor/x", FreeFlag: true}, false, "free", tierSignalFlagFree},
		{"mode free field (Cline catalog style)", upstreamModelEntry{ID: "vendor/y", Mode: "Free"}, false, "free", tierSignalFlagFree},
		{"free leading token", upstreamModelEntry{ID: "vendor/free-gpt-mini"}, false, "free", tierSignalNameToken},
		{"cline-free prefix", upstreamModelEntry{ID: "cline-free/gemini-3.8-flash"}, false, "free", tierSignalNameToken},
		{"free mid segment", upstreamModelEntry{ID: "vendor/model:free:vision"}, false, "free", tierSignalNameToken},
		{"freemap substring must not match", upstreamModelEntry{ID: "vendor/freemap-search", PricingPrompt: f64(0.2), PricingComplet: f64(0.2)}, false, "pass", tierSignalPricingPaid},
		{"explicit :free outranks list pricing", upstreamModelEntry{ID: "deepseek/deepseek-v4.1-flash:free", PricingPrompt: f64(0.2), PricingComplet: f64(0.6)}, false, "free", tierSignalSuffixFree},
		{"-free suffix outranks list pricing", upstreamModelEntry{ID: "deepseek/deepseek-v4.1-flash-free", PricingPrompt: f64(0.2), PricingComplet: f64(0.6)}, false, "free", tierSignalSuffixFree},
		{"free/ namespace outranks list pricing", upstreamModelEntry{ID: "free/deepseek-v4.1-flash", PricingPrompt: f64(0.2), PricingComplet: f64(0.6)}, false, "free", tierSignalNameToken},
		{"display name free outranks list pricing", upstreamModelEntry{ID: "deepseek/deepseek-v4.1-flash", Name: "DeepSeek V4.1 Flash (Free)", PricingPrompt: f64(0.2), PricingComplet: f64(0.6)}, false, "free", tierSignalDisplayName},
		{"freemium name must not match", upstreamModelEntry{ID: "vendor/freemium-plus", Name: "Freemium Plus"}, false, "pass", tierSignalPaidDefault},
		{"per-request fee is paid", upstreamModelEntry{ID: "vendor/per-call", PricingPrompt: f64(0), PricingComplet: f64(0), PricingRequest: f64(0.001)}, false, "pass", tierSignalPricingPaid},
		{"zero pricing incl. request fee is free", upstreamModelEntry{ID: "vendor/zero-all", PricingPrompt: f64(0), PricingComplet: f64(0), PricingRequest: f64(0)}, false, "free", tierSignalPricingZero},
		{"pricing zero outranks flag", upstreamModelEntry{ID: "vendor/z", PricingPrompt: f64(0), PricingComplet: f64(0), FreeFlag: true}, false, "free", tierSignalPricingZero},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tier, signal := classifyUpstreamModel(c.entry, c.providerFree)
			if tier != c.wantTier || signal != c.wantSignal {
				t.Errorf("got %s/%s want %s/%s", tier, signal, c.wantTier, c.wantSignal)
			}
		})
	}
}

// 上游列表解析：OpenAI data 数组 + OpenRouter 风格 pricing（字符串数字）+ 顶层 models 数组去重。
func TestDiscoverModelEntriesParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"models":[{"id":"dup/x"}]}`))
			return
		}
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer k123" {
			t.Errorf("auth header missing")
		}
		w.Write([]byte(`{"data":[{"id":"a/b","pricing":{"prompt":"0","completion":"0"}},{"id":"c/d","pricing":{"prompt":"2.5","completion":"10"}},{"id":"e/f"},{"id":"name/one","name":"Model (Free)","pricing":{"prompt":"1","completion":"2"}},{"id":"fee/x","pricing":{"prompt":"0","completion":"0","request":"0.001"}}],"models":[{"id":"dup/x"},{"id":"dup/x"},{"id":"tag/y","tags":["free"]},{"id":"a/b"},{"id":"flag/z","free":true},{"id":"mode/w","mode":"free"}]}`))
	}))
	defer srv.Close()
	entries, err := discoverModelEntries(srv.URL, "k123", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 9 {
		t.Fatalf("want 9 unique entries (data+models merged, dupes dropped), got %d: %+v", len(entries), entries)
	}
	byID := map[string]upstreamModelEntry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	if tier, sig := classifyUpstreamModel(byID["flag/z"], false); tier != "free" || sig != tierSignalFlagFree {
		t.Errorf("flag/z should classify free/flag-free, got %s/%s", tier, sig)
	}
	if tier, sig := classifyUpstreamModel(byID["mode/w"], false); tier != "free" || sig != tierSignalFlagFree {
		t.Errorf("mode/w should classify free/flag-free, got %s/%s", tier, sig)
	}
	if tier, _ := classifyUpstreamModel(byID["a/b"], false); tier != "free" {
		t.Errorf("a/b should classify free from string prices")
	}
	if tier, _ := classifyUpstreamModel(byID["c/d"], false); tier != "pass" {
		t.Errorf("c/d should classify paid")
	}
	if len(byID["tag/y"].Tags) != 1 || byID["tag/y"].Tags[0] != "free" {
		t.Errorf("tags not parsed: %+v", byID["tag/y"].Tags)
	}
	if tier, sig := classifyUpstreamModel(byID["name/one"], false); tier != "free" || sig != tierSignalDisplayName {
		t.Errorf("name/one should classify free/display-name-free, got %s/%s", tier, sig)
	}
	if byID["name/one"].Name != "Model (Free)" {
		t.Errorf("display name not parsed: %+v", byID["name/one"])
	}
	if tier, sig := classifyUpstreamModel(byID["fee/x"], false); tier != "pass" || sig != tierSignalPricingPaid {
		t.Errorf("fee/x (per-request fee) should classify paid/pricing-paid, got %s/%s", tier, sig)
	}
}

// preview → apply：add 进池并写入 provider.ModelIDs；tier_change 仅批准项生效；
// manual/跨源条目 protected；再次 preview 全为 nochange（幂等）。
func TestModelImportPreviewApplyFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[
			{"id":"free/model-a:free"},
			{"id":"paid/model-b","pricing":{"prompt":"1","completion":"3"}},
			{"id":"legacy/manual-c"},
			{"id":"legacy/remote-d"}
		]}`))
	}))
	defer srv.Close()

	oldPool := pool
	defer func() { pool = oldPool }()
	provider := &CustomProvider{ID: "imp1", Name: "ImpTest", BaseURL: srv.URL, Enabled: true, ModelIDs: []string{"legacy/manual-c", "legacy/remote-d"}}
	oldProvs, oldLoaded := customProviders, providersLoaded
	defer func() { customProviders, providersLoaded = oldProvs, oldLoaded }()
	customProviders = []*CustomProvider{provider}
	providersLoaded = true

	pool = &AccountPool{Models: []Model{
		{ID: "legacy/manual-c", Provider: "ImpTest", Cost: "free", Status: "active", Source: "provider", TierSignal: "manual"},
		{ID: "legacy/remote-d", Provider: "Cline", Cost: "pass", Status: "active", Source: "remote"},
	}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/models/import/preview", importJSONBody(map[string]any{"id": "imp1"}))
	handleModelImportPreview(rec, req)
	var prev struct {
		Data struct {
			Batch importBatch `json:"batch"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &prev); err != nil {
		t.Fatal(err)
	}
	batch := prev.Data.Batch
	if len(batch.Items) != 4 {
		t.Fatalf("items: %+v", batch.Items)
	}
	actions := map[string]string{}
	for _, it := range batch.Items {
		actions[it.ModelID] = it.Action
	}
	if actions["free/model-a:free"] != "add" || actions["paid/model-b"] != "add" {
		t.Errorf("new models should be add: %v", actions)
	}
	if actions["legacy/manual-c"] != "protected" || actions["legacy/remote-d"] != "protected" {
		t.Errorf("manual/remote entries must be protected: %v", actions)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/admin/api/models/import/apply", importJSONBody(map[string]any{
		"batchId": batch.ID,
		"approve": []string{"free/model-a:free", "paid/model-b", "legacy/manual-c", "legacy/remote-d"},
	}))
	handleModelImportApply(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("apply status %d body %s", rec2.Code, rec2.Body.String())
	}
	var curPool *AccountPool
	poolMu.Lock()
	curPool = pool
	poolMu.Unlock()
	got := map[string]Model{}
	for _, m := range curPool.Models {
		got[m.ID] = m
	}
	if got["free/model-a:free"].Cost != "free" || got["free/model-a:free"].TierSignal != tierSignalSuffixFree {
		t.Errorf("model-a not imported free with provenance: %+v", got["free/model-a:free"])
	}
	if got["paid/model-b"].Cost != "pass" || got["paid/model-b"].TierSignal != tierSignalPricingPaid {
		t.Errorf("model-b should import paid with pricing-paid provenance: %+v", got["paid/model-b"])
	}
	if got["legacy/manual-c"].Cost != "free" || got["legacy/manual-c"].TierSignal != "manual" {
		t.Errorf("manual tier must survive apply: %+v", got["legacy/manual-c"])
	}
	if got["legacy/remote-d"].Cost != "pass" || got["legacy/remote-d"].Source != "remote" {
		t.Errorf("remote model must not be touched: %+v", got["legacy/remote-d"])
	}
	inProv := false
	for _, id := range provider.ModelIDs {
		if id == "free/model-a:free" {
			inProv = true
		}
	}
	if !inProv {
		t.Errorf("approved model missing from provider.ModelIDs (sync would delete it): %v", provider.ModelIDs)
	}

	// 幂等：第二次 preview 不应再出现 add/tier_change
	rec3 := httptest.NewRecorder()
	handleModelImportPreview(rec3, httptest.NewRequest(http.MethodPost, "/p", importJSONBody(map[string]any{"id": "imp1"})))
	json.Unmarshal(rec3.Body.Bytes(), &prev)
	for _, it := range prev.Data.Batch.Items {
		if it.Action == "add" || it.Action == "tier_change" {
			t.Errorf("re-preview should be stable, got %+v", it)
		}
	}
}

// syncProviderModels 不得覆盖分级器批准过的逐模型层级（整源 Free=false 时）。
func TestSyncProviderModelsPreservesTierSignal(t *testing.T) {
	oldPool := pool
	defer func() { pool = oldPool }()
	pool = &AccountPool{Models: []Model{
		{ID: "mixed/free-one", Provider: "Mix", Cost: "free", Status: "active", Source: "provider", TierSignal: tierSignalPricingZero},
		{ID: "mixed/legacy", Provider: "Mix", Cost: "pass", Status: "active", Source: "provider"},
	}}
	provider := &CustomProvider{ID: "mix1", Name: "Mix", Enabled: true, ModelIDs: []string{"mixed/free-one", "mixed/legacy"}}
	// Free=false → claimed cost=pass；tiered 条目应保持 free
	provider.Free = false
	oldProvs, oldLoaded := customProviders, providersLoaded
	defer func() { customProviders, providersLoaded = oldProvs, oldLoaded }()
	customProviders = []*CustomProvider{provider}
	providersLoaded = true

	syncProviderModels()

	poolMu.Lock()
	got := map[string]Model{}
	for _, m := range pool.Models {
		got[m.ID] = m
	}
	poolMu.Unlock()
	if got["mixed/free-one"].Cost != "free" {
		t.Errorf("tier-signal model was overridden by provider-level cost: %+v", got["mixed/free-one"])
	}
	if got["mixed/legacy"].Cost != "pass" {
		t.Errorf("untiered model should still follow provider cost: %+v", got["mixed/legacy"])
	}
}

// syncProviderModels seeds id-only free signals without the import wizard, and
// retroactively repairs untiered provider entries; approved/per-model tiers and
// cross-source entries stay untouched.
func TestSyncProviderModelsSeedsFreeSignals(t *testing.T) {
	oldPool := pool
	defer func() { pool = oldPool }()
	pool = &AccountPool{Models: []Model{
		{ID: "old/declared-chat:free", Provider: "Seed", Cost: "pass", Status: "active", Source: "provider"},
		{ID: "approved/by-manual", Provider: "Seed", Cost: "pass", Status: "active", Source: "provider", TierSignal: "manual"},
		{ID: "cross/remote-free", Provider: "Other", Cost: "free", Status: "active", Source: "remote"},
	}}
	provider := &CustomProvider{ID: "seed1", Name: "Seed", Enabled: true, Free: false, ModelIDs: []string{
		"old/declared-chat:free", "approved/by-manual", "new/free-token-model", "new/plain-paid",
	}}
	oldProvs, oldLoaded := customProviders, providersLoaded
	defer func() { customProviders, providersLoaded = oldProvs, oldLoaded }()
	customProviders = []*CustomProvider{provider}
	providersLoaded = true

	syncProviderModels()

	poolMu.Lock()
	got := map[string]Model{}
	for _, m := range pool.Models {
		got[m.ID] = m
	}
	poolMu.Unlock()

	if m := got["old/declared-chat:free"]; m.Cost != "free" || m.TierSignal != tierSignalSuffixFree || m.TierDecidedAt == 0 {
		t.Errorf("untiered :free entry not repaired: %+v", m)
	}
	if m := got["new/free-token-model"]; m.Cost != "free" || m.TierSignal != tierSignalNameToken {
		t.Errorf("new free-token model not seeded free: %+v", m)
	}
	if m := got["new/plain-paid"]; m.Cost != "pass" || m.TierSignal != "" {
		t.Errorf("no-signal model should stay paid-untiered: %+v", m)
	}
	if m := got["approved/by-manual"]; m.Cost != "pass" || m.TierSignal != "manual" {
		t.Errorf("approved tier must not be overridden: %+v", m)
	}
	if m := got["cross/remote-free"]; m.Cost != "free" || m.Source != "remote" {
		t.Errorf("cross-source entry must not be touched: %+v", m)
	}
}
