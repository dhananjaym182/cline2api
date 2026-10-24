package main

// Phase 1 —— 具名模型组合 + 有序回退的测试。
// 覆盖：校验、持久化、有序回退、可重试/永久错误分类、跳过停用条目、不触发全局
// free 链、显式应急回退、跨源派发、同模型 provider 冗余、发现层暴露、请求日志归因、
// 流式预提交回退与「有内容后不再重开」。另含并发配置访问的竞态用例（-race）。

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// ============================ 测试辅助 ============================

func comboTestAccount(id string) *Account {
	return &Account{
		AccountID: id, Email: id + "@example.com", AccessToken: id + "-token",
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), Status: "active",
	}
}

func installPool(t *testing.T, models []Model, accounts ...*Account) {
	t.Helper()
	old := pool
	t.Cleanup(func() { pool = old })
	if len(accounts) == 0 {
		accounts = []*Account{comboTestAccount("acc1")}
	}
	pool = &AccountPool{Accounts: accounts, Models: models}
}

func installConfig(t *testing.T, cfg *proxyConfigData) {
	t.Helper()
	t.Setenv(dataDirEnv, t.TempDir()) // 配置落盘隔离到临时目录
	old := getProxyConfig()
	t.Cleanup(func() { setProxyConfig(old) })
	setProxyConfig(cfg)
}

func installTransport(t *testing.T, fn func(req *http.Request) *http.Response) {
	t.Helper()
	old := httpClient.Transport
	t.Cleanup(func() { httpClient.Transport = old })
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		return fn(req), nil
	})
}

func stubResp(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}
}

func stubSSE(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Request:    req,
	}
}

func readReqModel(req *http.Request) string {
	if req == nil || req.Body == nil {
		return ""
	}
	b, _ := io.ReadAll(req.Body)
	var p map[string]any
	_ = json.Unmarshal(b, &p)
	m, _ := p["model"].(string)
	return m
}

func chatParams(model string) map[string]any {
	return map[string]any{
		"model":      model,
		"max_tokens": 16,
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
	}
}

const okCompletion = `{"id":"ok","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`

// ============================ 校验 ============================

func TestComboValidate(t *testing.T) {
	installPool(t, []Model{
		{ID: "m-a", Cost: "free", Status: "active", Source: "remote"},
		{ID: "m-b", Cost: "free", Status: "active", Source: "remote"},
	})

	valid := ModelCombo{ID: "premium", Name: "Premium", Enabled: true,
		Entries: []ComboEntry{{Model: "m-a"}, {Model: "m-b", Disabled: true}}}
	if err := validateCombo(valid, nil); err != nil {
		t.Fatalf("valid combo rejected: %v", err)
	}

	cases := []struct {
		name     string
		c        ModelCombo
		existing []ModelCombo
	}{
		{"empty id", ModelCombo{Name: "x", Entries: []ComboEntry{{Model: "m-a"}}}, nil},
		{"bad chars", ModelCombo{ID: "bad/id", Name: "x", Entries: []ComboEntry{{Model: "m-a"}}}, nil},
		{"reserved alias free", ModelCombo{ID: "free", Name: "x", Entries: []ComboEntry{{Model: "m-a"}}}, nil},
		{"collides with model id", ModelCombo{ID: "m-a", Name: "x", Entries: []ComboEntry{{Model: "m-b"}}}, nil},
		{"empty name", ModelCombo{ID: "x", Entries: []ComboEntry{{Model: "m-a"}}}, nil},
		{"duplicate id", ModelCombo{ID: "premium", Name: "x", Entries: []ComboEntry{{Model: "m-a"}}},
			[]ModelCombo{{ID: "premium", Name: "Other"}}},
		{"duplicate name", ModelCombo{ID: "other", Name: "Premium", Entries: []ComboEntry{{Model: "m-a"}}},
			[]ModelCombo{{ID: "premium", Name: "Premium"}}},
		{"no entries", ModelCombo{ID: "x", Name: "x"}, nil},
		{"duplicate entry", ModelCombo{ID: "x", Name: "x", Entries: []ComboEntry{{Model: "m-a"}, {Model: "m-a"}}}, nil},
		{"unknown reference", ModelCombo{ID: "x", Name: "x", Entries: []ComboEntry{{Model: "nope"}}}, nil},
		{"free entry", ModelCombo{ID: "x", Name: "x", Entries: []ComboEntry{{Model: "free"}}}, nil},
		{"enabled with no enabled entry", ModelCombo{ID: "x", Name: "x", Enabled: true,
			Entries: []ComboEntry{{Model: "m-a", Disabled: true}}}, nil},
	}
	for _, tc := range cases {
		if err := validateCombo(tc.c, tc.existing); err == nil {
			t.Errorf("%s: expected validation error, got nil", tc.name)
		}
	}
}

// ============================ 持久化 ============================

func TestComboPersistenceAndReload(t *testing.T) {
	installPool(t, []Model{{ID: "m-a", Cost: "free", Status: "active", Source: "remote"}})
	t.Setenv(dataDirEnv, t.TempDir())
	old := getProxyConfig()
	t.Cleanup(func() { setProxyConfig(old) })

	proxyConfigMu.Lock()
	proxyConfig.Combos = nil
	proxyConfigMu.Unlock()
	saveProxyConfigLocked()

	if _, err := upsertCombo(ModelCombo{ID: "c1", Name: "C1", Enabled: true, Entries: []ComboEntry{{Model: "m-a"}}}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	loaded := loadProxyConfigFromDisk()
	if len(loaded.Combos) != 1 || loaded.Combos[0].ID != "c1" {
		t.Fatalf("reload got %+v, want combo c1", loaded.Combos)
	}
	// 重新加载（模拟重启）后仍可解析
	if c, ok := findEnabledCombo("c1"); !ok || c.Name != "C1" {
		t.Fatalf("findEnabledCombo after reload = %+v ok=%v", c, ok)
	}
	// 删除后持久化
	if !deleteCombo("c1") {
		t.Fatal("deleteCombo returned false")
	}
	if got := loadProxyConfigFromDisk(); len(got.Combos) != 0 {
		t.Fatalf("after delete reload got %+v", got.Combos)
	}
}

// ============================ 有序回退 ============================

func TestRunComboFirstEntrySucceeds(t *testing.T) {
	installPool(t, nil)
	installConfig(t, defaultProxyConfig())
	var seen []string
	installTransport(t, func(req *http.Request) *http.Response {
		seen = append(seen, readReqModel(req))
		return stubResp(req, http.StatusOK, okCompletion)
	})

	combo := ModelCombo{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "combo-a"}, {Model: "combo-b"}}}
	run := runCombo(chatParams("c"), combo, false, "")
	if run.Err != nil {
		t.Fatalf("run err: %v", run.Err)
	}
	if run.Model != "combo-a" {
		t.Fatalf("served %q, want combo-a", run.Model)
	}
	if len(seen) != 1 || seen[0] != "combo-a" {
		t.Fatalf("upstream attempts = %v, want [combo-a] only", seen)
	}
}

func TestRunComboRetryableFallback(t *testing.T) {
	installPool(t, nil)
	installConfig(t, defaultProxyConfig())
	installTransport(t, func(req *http.Request) *http.Response {
		if readReqModel(req) == "combo-a" {
			return stubResp(req, http.StatusInternalServerError, `{"error":"boom"}`)
		}
		return stubResp(req, http.StatusOK, okCompletion)
	})

	combo := ModelCombo{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "combo-a"}, {Model: "combo-b"}}}
	run := runCombo(chatParams("c"), combo, false, "")
	if run.Err != nil {
		t.Fatalf("run err: %v", run.Err)
	}
	if run.Model != "combo-b" {
		t.Fatalf("served %q, want combo-b", run.Model)
	}
	if len(run.Trace) != 2 {
		t.Fatalf("trace = %+v, want 2 entries", run.Trace)
	}
	if run.Trace[0].Outcome != "failed" || run.Trace[0].Status != 500 {
		t.Fatalf("trace[0] = %+v, want failed/500", run.Trace[0])
	}
	if run.Trace[1].Outcome != "success" {
		t.Fatalf("trace[1] = %+v, want success", run.Trace[1])
	}
	// 请求日志归因
	var rl RequestLog
	applyComboTrace(&rl, combo, run)
	if rl.RequestedModel != "c" || rl.Model != "combo-b" {
		t.Fatalf("log requested=%q actual=%q, want c/combo-b", rl.RequestedModel, rl.Model)
	}
	if !rl.FallbackUsed || rl.FallbackCount != 1 {
		t.Fatalf("fallback used=%v count=%d, want true/1", rl.FallbackUsed, rl.FallbackCount)
	}
}

func TestRunComboPermanentErrorStops(t *testing.T) {
	installPool(t, nil)
	installConfig(t, defaultProxyConfig())
	var seen []string
	installTransport(t, func(req *http.Request) *http.Response {
		seen = append(seen, readReqModel(req))
		return stubResp(req, http.StatusBadRequest, `{"error":"bad request"}`)
	})

	combo := ModelCombo{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "combo-a"}, {Model: "combo-b"}}}
	run := runCombo(chatParams("c"), combo, false, "")
	if run.Err == nil {
		t.Fatal("expected error for permanent failure")
	}
	if len(seen) != 1 || seen[0] != "combo-a" {
		t.Fatalf("upstream attempts = %v, want only combo-a (no pointless retry)", seen)
	}
}

func TestRunComboPreservesConfiguredOrder(t *testing.T) {
	acc := comboTestAccount("acc1")
	acc.ModelStats = map[string]*ModelStat{
		"combo-a": {UsageCount: 1000},
		"combo-b": {UsageCount: 0},
	}
	installPool(t, nil, acc)
	installConfig(t, defaultProxyConfig())
	var seen []string
	installTransport(t, func(req *http.Request) *http.Response {
		seen = append(seen, readReqModel(req))
		return stubResp(req, http.StatusOK, okCompletion)
	})

	// combo-b 看起来「更优」（用量低），但配置顺序 combo-a 在前 —— 不得重排。
	combo := ModelCombo{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "combo-a"}, {Model: "combo-b"}}}
	run := runCombo(chatParams("c"), combo, false, "")
	if run.Model != "combo-a" {
		t.Fatalf("served %q, want combo-a (configured order must win)", run.Model)
	}
	if len(seen) != 1 {
		t.Fatalf("attempts = %v, want only combo-a", seen)
	}
}

func TestRunComboSkipsDisabledEntries(t *testing.T) {
	installPool(t, []Model{
		{ID: "combo-b", Cost: "free", Status: "active", Source: "remote", Disabled: true},
	})
	installConfig(t, defaultProxyConfig())
	installTransport(t, func(req *http.Request) *http.Response {
		return stubResp(req, http.StatusOK, okCompletion)
	})

	combo := ModelCombo{ID: "c", Name: "C", Enabled: true, Entries: []ComboEntry{
		{Model: "combo-a", Disabled: true}, // 条目停用
		{Model: "combo-b"},                 // 池内模型被管理员停用
		{Model: "combo-c"},                 // 可用
	}}
	run := runCombo(chatParams("c"), combo, false, "")
	if run.Err != nil {
		t.Fatalf("run err: %v", run.Err)
	}
	if run.Model != "combo-c" {
		t.Fatalf("served %q, want combo-c", run.Model)
	}
	skipped := 0
	for _, a := range run.Trace {
		if a.Outcome == "skipped" {
			skipped++
		}
	}
	if skipped != 2 {
		t.Fatalf("skipped = %d, want 2; trace=%+v", skipped, run.Trace)
	}
}

func TestRunComboDoesNotInvokeGlobalChainByDefault(t *testing.T) {
	installPool(t, []Model{{ID: "combo-free", Cost: "free", Status: "active", Source: "remote"}})
	cfg := defaultProxyConfig()
	cfg.ModelChain = []string{"combo-free"} // 全局回退链存在，但组合默认不得使用
	installConfig(t, cfg)
	var seen []string
	installTransport(t, func(req *http.Request) *http.Response {
		m := readReqModel(req)
		seen = append(seen, m)
		if m == "combo-a" {
			return stubResp(req, http.StatusInternalServerError, `{"error":"boom"}`)
		}
		return stubResp(req, http.StatusOK, okCompletion)
	})

	combo := ModelCombo{ID: "c", Name: "C", Enabled: true, Entries: []ComboEntry{{Model: "combo-a"}}}
	run := runCombo(chatParams("c"), combo, false, "")
	if run.Err == nil {
		t.Fatal("expected failure (no emergency fallback enabled)")
	}
	if len(seen) != 1 || seen[0] != "combo-a" {
		t.Fatalf("upstream attempts = %v, want only combo-a (global free chain must NOT run)", seen)
	}
}

func TestRunComboEmergencyFallbackOptIn(t *testing.T) {
	installPool(t, []Model{{ID: "combo-free", Cost: "free", Status: "active", Source: "remote"}})
	cfg := defaultProxyConfig()
	cfg.ModelChain = []string{"combo-free"}
	installConfig(t, cfg)
	installTransport(t, func(req *http.Request) *http.Response {
		if readReqModel(req) == "combo-a" {
			return stubResp(req, http.StatusInternalServerError, `{"error":"boom"}`)
		}
		return stubResp(req, http.StatusOK, okCompletion)
	})

	combo := ModelCombo{ID: "c", Name: "C", Enabled: true, EmergencyFallback: true,
		Entries: []ComboEntry{{Model: "combo-a"}}}
	run := runCombo(chatParams("c"), combo, false, "")
	if run.Err != nil {
		t.Fatalf("run err: %v", run.Err)
	}
	if run.Model != "combo-free" {
		t.Fatalf("served %q, want combo-free (emergency fallback)", run.Model)
	}
}

// ============================ 跨源 / provider 冗余 ============================

func TestComboEntryUsesProviderPath(t *testing.T) {
	installPool(t, nil)
	installProviders(t, &CustomProvider{
		ID: "prov_combo1", Name: "P1", BaseURL: "http://prov1.test/v1",
		APIKey: "sk", ModelIDs: []string{"prov-x"}, Enabled: true, Priority: 10,
	})
	installConfig(t, defaultProxyConfig())

	providerCalls, clineCalls := 0, 0
	installTransport(t, func(req *http.Request) *http.Response {
		if req.URL.Host == "prov1.test" {
			providerCalls++
			return stubResp(req, http.StatusOK, okCompletion)
		}
		clineCalls++
		return stubResp(req, http.StatusOK, okCompletion)
	})

	combo := ModelCombo{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "prov-x"}, {Model: "cline-y"}}}
	run := runCombo(chatParams("c"), combo, false, "")
	if run.Err != nil {
		t.Fatalf("run err: %v", run.Err)
	}
	if run.Model != "prov-x" || run.Upstream != upstreamProvider {
		t.Fatalf("served model=%q upstream=%q, want prov-x/provider", run.Model, run.Upstream)
	}
	if providerCalls != 1 || clineCalls != 0 {
		t.Fatalf("providerCalls=%d clineCalls=%d, want 1/0", providerCalls, clineCalls)
	}
}

func TestComboSpansSourcesFallback(t *testing.T) {
	installPool(t, nil)
	installProviders(t, &CustomProvider{
		ID: "prov_combo2", Name: "P2", BaseURL: "http://prov2.test/v1",
		APIKey: "sk", ModelIDs: []string{"prov-x"}, Enabled: true, Priority: 10,
	})
	installConfig(t, defaultProxyConfig())

	providerCalls := 0
	installTransport(t, func(req *http.Request) *http.Response {
		m := readReqModel(req)
		if req.URL.Host == "prov2.test" {
			providerCalls++
			return stubResp(req, http.StatusInternalServerError, `{"error":"down"}`)
		}
		// cline 侧：第一个条目 prov-x 也失败，第二个条目 cline-y 成功
		if m == "prov-x" {
			return stubResp(req, http.StatusInternalServerError, `{"error":"down"}`)
		}
		return stubResp(req, http.StatusOK, okCompletion)
	})

	combo := ModelCombo{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "prov-x"}, {Model: "cline-y"}}}
	run := runCombo(chatParams("c"), combo, false, "")
	if run.Err != nil {
		t.Fatalf("run err: %v", run.Err)
	}
	if run.Model != "cline-y" || run.Upstream != upstreamCline {
		t.Fatalf("served model=%q upstream=%q, want cline-y/cline", run.Model, run.Upstream)
	}
	if providerCalls != 1 {
		t.Fatalf("providerCalls=%d, want 1 (provider path attempted for entry 1)", providerCalls)
	}
}

func TestComboSameModelProviderRedundancy(t *testing.T) {
	installPool(t, nil)
	installProviders(t,
		&CustomProvider{ID: "prov_r1", Name: "R1", BaseURL: "http://r1.test/v1",
			APIKey: "sk", ModelIDs: []string{"m-r"}, Enabled: true, Priority: 1},
		&CustomProvider{ID: "prov_r2", Name: "R2", BaseURL: "http://r2.test/v1",
			APIKey: "sk", ModelIDs: []string{"m-r"}, Enabled: true, Priority: 2},
	)
	installConfig(t, defaultProxyConfig())

	installTransport(t, func(req *http.Request) *http.Response {
		if req.URL.Host == "r1.test" {
			return stubResp(req, http.StatusInternalServerError, `{"error":"down"}`)
		}
		return stubResp(req, http.StatusOK, okCompletion)
	})

	combo := ModelCombo{ID: "c", Name: "C", Enabled: true, Entries: []ComboEntry{{Model: "m-r"}}}
	run := runCombo(chatParams("c"), combo, false, "")
	if run.Err != nil {
		t.Fatalf("run err: %v", run.Err)
	}
	if run.Model != "m-r" || run.Upstream != upstreamProvider {
		t.Fatalf("served model=%q upstream=%q, want m-r/provider via redundancy group", run.Model, run.Upstream)
	}
}

// ============================ 发现层 ============================

func TestBuildModelsListExposesCombos(t *testing.T) {
	installPool(t, []Model{
		{ID: "m-a", Cost: "free", Status: "active", Source: "remote"},
		{ID: "m-b", Cost: "free", Status: "active", Source: "remote", Disabled: true},
	})
	cfg := defaultProxyConfig()
	cfg.Combos = []ModelCombo{
		{ID: "c-on", Name: "On", Enabled: true, Entries: []ComboEntry{{Model: "m-a"}}},
		{ID: "c-off", Name: "Off", Enabled: false, Entries: []ComboEntry{{Model: "m-a"}}},
	}
	installConfig(t, cfg)

	list := buildModelsList(false)
	byID := map[string]map[string]any{}
	for _, m := range list {
		byID[m["id"].(string)] = m
	}
	if _, ok := byID["m-a"]; !ok {
		t.Fatal("enabled model m-a missing from list")
	}
	if _, ok := byID["m-b"]; ok {
		t.Fatal("disabled model m-b must not be listed")
	}
	c, ok := byID["c-on"]
	if !ok {
		t.Fatal("enabled combo c-on missing from list")
	}
	if c["owned_by"] != "combo" {
		t.Fatalf("combo owned_by = %v, want combo", c["owned_by"])
	}
	if _, ok := byID["c-off"]; ok {
		t.Fatal("disabled combo c-off must not be listed")
	}
}

func TestComboDisabledResolution(t *testing.T) {
	installPool(t, nil)
	cfg := defaultProxyConfig()
	cfg.Combos = []ModelCombo{{ID: "c-off", Name: "Off", Enabled: false, Entries: []ComboEntry{{Model: "m-a"}}}}
	installConfig(t, cfg)

	if _, ok := findCombo("c-off"); !ok {
		t.Fatal("findCombo should find disabled combo")
	}
	if _, ok := findEnabledCombo("c-off"); ok {
		t.Fatal("findEnabledCombo must not return a disabled combo")
	}
}

// ============================ 请求日志归因 ============================

func TestApplyComboLogFields(t *testing.T) {
	rl := RequestLog{Model: "c"}
	combo := ModelCombo{ID: "c", Name: "Combo Display"}
	trace := []AttemptLog{
		{Model: "combo-a", Order: 1, Upstream: upstreamCline, Status: 500, Outcome: "failed", Reason: "API 500: boom"},
		{Model: "combo-b", Order: 2, Upstream: upstreamOpenCode, Outcome: "success"},
	}
	applyComboLog(&rl, combo, trace, "combo-b")

	if rl.RequestedModel != "c" || rl.ComboID != "c" || rl.ComboName != "Combo Display" {
		t.Fatalf("combo attribution = %+v", rl)
	}
	if rl.Model != "combo-b" {
		t.Fatalf("actual model = %q, want combo-b", rl.Model)
	}
	if !rl.FallbackUsed || rl.FallbackCount != 1 {
		t.Fatalf("fallback used=%v count=%d, want true/1", rl.FallbackUsed, rl.FallbackCount)
	}
	if len(rl.Attempts) != 2 || rl.Attempts[0].Reason == "" {
		t.Fatalf("attempts not recorded: %+v", rl.Attempts)
	}
}

// ============================ 并发 ============================

func TestComboConcurrentConfigAccess(t *testing.T) {
	installPool(t, []Model{{ID: "m-a", Cost: "free", Status: "active", Source: "remote"}})
	t.Setenv(dataDirEnv, t.TempDir())
	old := getProxyConfig()
	t.Cleanup(func() { setProxyConfig(old) })

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := "c" + string(rune('a'+n))
			for j := 0; j < 20; j++ {
				_, _ = upsertCombo(ModelCombo{ID: id, Name: id, Enabled: true, Entries: []ComboEntry{{Model: "m-a"}}})
				_ = listCombos()
				_, _ = findCombo(id)
			}
		}(i)
	}
	wg.Wait()
	if got := listCombos(); len(got) != 8 {
		t.Fatalf("after concurrency got %d combos, want 8", len(got))
	}
}

// ============================ 回归（review 修复） ============================

// 回归 H1：zen 故障转移窗口内，组合条目的分类必须是 zen-degraded（跳过），
// 不能沿用 routeModel 的 cline 降级（否则会把 zen 模型 ID 原样发给 Cline → 400）。
func TestComboRouteKindZenDegraded(t *testing.T) {
	installPool(t, []Model{{ID: "deepseek-v4-flash-free", Source: "zen", Cost: "free", Status: "active"}})

	zenConfigMu.Lock()
	oldZC := zenConfig
	zenConfig = defaultZenConfig() // Enabled/Failover=true
	zenConfigMu.Unlock()
	t.Cleanup(func() {
		zenConfigMu.Lock()
		zenConfig = oldZC
		zenConfigMu.Unlock()
	})

	zenStateMu.Lock()
	oldCount, oldUntil := zenFailCount, zenFailUntil
	zenStateMu.Unlock()
	t.Cleanup(func() {
		zenStateMu.Lock()
		zenFailCount, zenFailUntil = oldCount, oldUntil
		zenStateMu.Unlock()
	})

	const model = "deepseek-v4-flash-free"

	// 健康：routeModel 判 zen，组合分类也是 zen。
	zenStateMu.Lock()
	zenFailCount, zenFailUntil = 0, time.Time{}
	zenStateMu.Unlock()
	if k := comboRouteKind(model); k != "zen" {
		t.Fatalf("healthy comboRouteKind = %q, want zen", k)
	}

	// 降级窗口：routeModel 降级为 cline，但组合必须判 zen-degraded。
	zenStateMu.Lock()
	zenFailCount = 99
	zenFailUntil = time.Now().Add(time.Minute)
	zenStateMu.Unlock()
	if got := routeModel(model); got != "cline" {
		t.Fatalf("routeModel in failover window = %q, want cline", got)
	}
	if k := comboRouteKind(model); k != "zen-degraded" {
		t.Fatalf("degraded comboRouteKind = %q, want zen-degraded", k)
	}
}

// 回归 M1：启用中的组合若唯一条目的模型被管理员停用，保存时必须报错
// （否则组合可以保存成功但永远不可服务）。
func TestComboValidateRejectsEnabledComboWithOnlyDisabledModel(t *testing.T) {
	installPool(t, []Model{{ID: "m-off", Cost: "free", Status: "active", Source: "remote", Disabled: true}})
	err := validateCombo(ModelCombo{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "m-off"}}}, nil)
	if err == nil {
		t.Fatal("expected error: enabled combo whose only entry model is admin-disabled")
	}
}

// 回归 L5：对 64 字符 ID 执行克隆不得生成超长 ID 而被校验拒绝。
func TestComboCloneTruncatesLongID(t *testing.T) {
	installPool(t, []Model{{ID: "m-a", Cost: "free", Status: "active", Source: "remote"}})
	t.Setenv(dataDirEnv, t.TempDir())
	old := getProxyConfig()
	t.Cleanup(func() { setProxyConfig(old) })

	longID := strings.Repeat("a", comboIDMaxLen) // 64 字符
	if len(longID) != comboIDMaxLen {
		t.Fatalf("setup: len=%d", len(longID))
	}
	if _, err := upsertCombo(ModelCombo{ID: longID, Name: "Long", Enabled: true,
		Entries: []ComboEntry{{Model: "m-a"}}}); err != nil {
		t.Fatalf("upsert long-id combo: %v", err)
	}
	cp, err := cloneCombo(longID)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if len(cp.ID) > comboIDMaxLen {
		t.Fatalf("cloned id length %d exceeds max %d (%q)", len(cp.ID), comboIDMaxLen, cp.ID)
	}
}

// ==== 确定性 payload 400 快速失败：invalid-request 不该在账号池/模型链上空转 ====
// 回归：客户端工具声明缺 description 时上游网关对每个账号/模型都返回同一 400
// "Invalid input"，此前逐账号/逐模型各撞 ~7 次注定失败的往返。

func TestCallSingleModelDeterministic400StopsAccountLoop(t *testing.T) {
	acc1, acc2 := comboTestAccount("acc-1"), comboTestAccount("acc-2")
	installPool(t, []Model{{ID: "combo-a", Cost: "free", Status: "active", Source: "remote"}}, acc1, acc2)
	installConfig(t, defaultProxyConfig())
	attempts := 0
	installTransport(t, func(req *http.Request) *http.Response {
		attempts++
		return stubResp(req, http.StatusBadRequest,
			`{"error":{"message":"Invalid input: expected string, received null","param":"tools.0.function.description"}}`)
	})
	_, _, _, _, err := callSingleModel(chatParams("combo-a"), ComboEntry{Model: "combo-a"}, false, "")
	if err == nil {
		t.Fatal("expected upstream error to surface")
	}
	if attempts != 1 {
		t.Fatalf("upstream attempts = %d, want 1 (deterministic 400 must not rotate accounts)", attempts)
	}
}

func TestCallFreeClineAPIDeterministic400FailsFast(t *testing.T) {
	installPool(t, []Model{
		{ID: "free-a", Cost: "free", Status: "active", Source: "remote"},
		{ID: "free-b", Cost: "free", Status: "active", Source: "remote"},
	}, comboTestAccount("free-1"), comboTestAccount("free-2"))
	cfg := defaultProxyConfig()
	cfg.ModelChain = []string{"free-a", "free-b"}
	installConfig(t, cfg)
	attempts := 0
	installTransport(t, func(req *http.Request) *http.Response {
		attempts++
		return stubResp(req, http.StatusBadRequest,
			`{"error":"Invalid input: expected string, received null","param":"tools.0.function.description"}`)
	})
	_, _, err := callFreeClineAPI(chatParams("free"), false)
	if err == nil {
		t.Fatal("expected upstream error to surface")
	}
	if attempts != 1 {
		t.Fatalf("upstream attempts = %d, want 1 (deterministic 400 must abort the whole free chain)", attempts)
	}
}

func TestIsDeterministicRequestErrorClassification(t *testing.T) {
	det := &clineAPIError{statusCode: http.StatusBadRequest,
		message: `{"error":"Invalid input: expected string, received null","param":"tools.0.function.description"}`}
	if !isDeterministicRequestError(det) {
		t.Error("invalid-input 400 must be classified deterministic")
	}
	ir := &clineAPIError{statusCode: http.StatusBadRequest,
		message: `{"error":{"type":"invalid_request_error","message":"bad tool schema"}}`}
	if !isDeterministicRequestError(ir) {
		t.Error("invalid_request_error 400 must be classified deterministic")
	}
	not400 := &clineAPIError{statusCode: http.StatusInternalServerError, message: "Invalid input"}
	if isDeterministicRequestError(not400) {
		t.Error("only status-400 errors count as deterministic")
	}
	// 上下文超限 400 显式排除：必须保留 truncate+retry/continue 行为
	cts := &clineAPIError{statusCode: http.StatusBadRequest,
		message: `prompt is too long: maximum context length exceeded`}
	if isDeterministicRequestError(cts) {
		t.Error("context-length 400 must NOT be deterministic (truncate/retry must stay available)")
	}
	if isDeterministicRequestError(nil) {
		t.Error("nil error must not be deterministic")
	}
}

// 对照组：上下文超限 400 仍走「截断后继续下一个条目」路径，不能被快速失败误伤。
func TestRunComboContextLength400ContinuesToNextEntry(t *testing.T) {
	installPool(t, nil)
	installConfig(t, defaultProxyConfig())
	installTransport(t, func(req *http.Request) *http.Response {
		if readReqModel(req) == "combo-a" {
			return stubResp(req, http.StatusBadRequest, `{"error":"request context length exceeded"}`)
		}
		return stubResp(req, http.StatusOK, okCompletion)
	})
	combo := ModelCombo{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "combo-a"}, {Model: "combo-b"}}}
	run := runCombo(chatParams("c"), combo, false, "")
	if run.Err != nil {
		t.Fatalf("context-length 400 must continue to next entry, got err: %v", run.Err)
	}
	if run.Model != "combo-b" {
		t.Fatalf("served %q, want combo-b", run.Model)
	}
}
