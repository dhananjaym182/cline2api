package main

// 组合条目的上游固定（provider-pinned combo entries）测试。
// 覆盖：entryRouteKind 各 Source 取值（含空=自动）、固定 provider 时命中指定上游、
// 同一模型不同固定不被去重、validateCombo 拒绝非法固定，以及空固定行为不变。

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// installProviders 把 provider registry 隔离到临时目录并安装给定 provider。
func installProviders(t *testing.T, provs ...*CustomProvider) {
	t.Helper()
	oldPath := providersFilePath
	oldCool := providersCooldownsPath
	providersFilePath = filepath.Join(t.TempDir(), ".cline-providers.json")
	providersCooldownsPath = filepath.Join(t.TempDir(), ".cline-providers-cooldowns.json")
	oldProvs := customProviders
	oldLoaded := providersLoaded
	customProviders = nil
	providersLoaded = true
	t.Cleanup(func() {
		providersFilePath = oldPath
		providersCooldownsPath = oldCool
		customProviders = oldProvs
		providersLoaded = oldLoaded
		resetRoutingState()
	})
	for _, p := range provs {
		upsertProvider(p)
	}
}

// ============================ entryRouteKind ============================

func TestEntryRouteKind(t *testing.T) {
	installPool(t, []Model{
		{ID: "m-a", Cost: "free", Status: "active", Source: "remote"},
		{ID: "zen-free-x", Source: "zen", Cost: "free", Status: "active"},
	})

	zenConfigMu.Lock()
	oldZC := zenConfig
	zenConfig = defaultZenConfig() // Enabled/Failover=true
	zenConfigMu.Unlock()
	zenStateMu.Lock()
	oldCount, oldUntil := zenFailCount, zenFailUntil
	zenFailCount, zenFailUntil = 0, time.Time{}
	zenStateMu.Unlock()
	t.Cleanup(func() {
		zenConfigMu.Lock()
		zenConfig = oldZC
		zenConfigMu.Unlock()
		zenStateMu.Lock()
		zenFailCount, zenFailUntil = oldCount, oldUntil
		zenStateMu.Unlock()
	})

	// 空固定 = 自动：与 comboRouteKind 完全一致。
	if got, want := entryRouteKind(ComboEntry{Model: "m-a"}), comboRouteKind("m-a"); got != want || got != "cline" {
		t.Fatalf("empty source route = %q (comboRouteKind=%q), want cline", got, want)
	}
	if got := entryRouteKind(ComboEntry{Model: "zen-free-x"}); got != "zen" {
		t.Fatalf("empty source on zen model = %q, want zen", got)
	}

	// 显式 cline / provider 都落到 cline 分支（provider 名称在分支内强制）。
	if got := entryRouteKind(ComboEntry{Model: "m-a", Source: "cline"}); got != "cline" {
		t.Fatalf("source=cline route = %q, want cline", got)
	}
	if got := entryRouteKind(ComboEntry{Model: "m-a", Source: "provider", Provider: "P"}); got != "cline" {
		t.Fatalf("source=provider route = %q, want cline", got)
	}

	// 健康 zen：source=zen 走 zen。
	if got := entryRouteKind(ComboEntry{Model: "zen-free-x", Source: "zen"}); got != "zen" {
		t.Fatalf("source=zen healthy route = %q, want zen", got)
	}

	// 降级窗口：source=zen 仍指向 zen，但运行时不可用 → zen-degraded（跳过）。
	zenStateMu.Lock()
	zenFailCount, zenFailUntil = 99, time.Now().Add(time.Minute)
	zenStateMu.Unlock()
	if got := entryRouteKind(ComboEntry{Model: "zen-free-x", Source: "zen"}); got != "zen-degraded" {
		t.Fatalf("source=zen degraded route = %q, want zen-degraded", got)
	}
	zenStateMu.Lock()
	zenFailCount, zenFailUntil = 0, time.Time{}
	zenStateMu.Unlock()

	// 非 zen 模型固定到 zen → 不可用。
	if got := entryRouteKind(ComboEntry{Model: "m-a", Source: "zen"}); got != "unavailable" {
		t.Fatalf("source=zen on non-zen model route = %q, want unavailable", got)
	}
}

// ============================ 固定 provider 派发 ============================

// 两个 provider 暴露同一模型 ID，条目固定到优先级较低的那家时必须命中它。
func TestRunComboPinsProvider(t *testing.T) {
	installPool(t, nil)
	installConfig(t, defaultProxyConfig())
	installProviders(t,
		&CustomProvider{ID: "prov_pin1", Name: "P1", BaseURL: "http://p1.test/v1",
			APIKey: "sk", ModelIDs: []string{"m-r"}, Enabled: true, Priority: 1},
		&CustomProvider{ID: "prov_pin2", Name: "P2", BaseURL: "http://p2.test/v1",
			APIKey: "sk", ModelIDs: []string{"m-r"}, Enabled: true, Priority: 2},
	)

	var hosts []string
	installTransport(t, func(req *http.Request) *http.Response {
		hosts = append(hosts, req.URL.Host)
		return stubResp(req, http.StatusOK, okCompletion)
	})

	combo := ModelCombo{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "m-r", Source: "provider", Provider: "P2"}}}
	run := runCombo(chatParams("c"), combo, false, "")
	if run.Err != nil {
		t.Fatalf("run err: %v", run.Err)
	}
	if run.Model != "m-r" || run.Upstream != upstreamProvider {
		t.Fatalf("served model=%q upstream=%q, want m-r/provider", run.Model, run.Upstream)
	}
	if len(hosts) != 1 || hosts[0] != "p2.test" {
		t.Fatalf("upstream hosts = %v, want [p2.test] (pinned P2, not priority P1)", hosts)
	}
	if len(run.Trace) != 1 || run.Trace[0].Source != "provider" || run.Trace[0].Provider != "P2" {
		t.Fatalf("trace = %+v, want Source=provider Provider=P2", run.Trace)
	}
}

// 同一模型固定到两家不同 provider → 两条目都不被去重；首条失败后第二条仍被尝试。
func TestRunComboSameModelDifferentPinsNotDeduped(t *testing.T) {
	installPool(t, nil)
	installConfig(t, defaultProxyConfig())
	installProviders(t,
		&CustomProvider{ID: "prov_dd1", Name: "P1", BaseURL: "http://d1.test/v1",
			APIKey: "sk", ModelIDs: []string{"m-r"}, Enabled: true, Priority: 1},
		&CustomProvider{ID: "prov_dd2", Name: "P2", BaseURL: "http://d2.test/v1",
			APIKey: "sk", ModelIDs: []string{"m-r"}, Enabled: true, Priority: 2},
	)

	var hosts []string
	installTransport(t, func(req *http.Request) *http.Response {
		hosts = append(hosts, req.URL.Host)
		if req.URL.Host == "d1.test" {
			return stubResp(req, http.StatusInternalServerError, `{"error":"down"}`)
		}
		return stubResp(req, http.StatusOK, okCompletion)
	})

	combo := ModelCombo{ID: "c", Name: "C", Enabled: true, Entries: []ComboEntry{
		{Model: "m-r", Source: "provider", Provider: "P1"},
		{Model: "m-r", Source: "provider", Provider: "P2"},
	}}
	run := runCombo(chatParams("c"), combo, false, "")
	if run.Err != nil {
		t.Fatalf("run err: %v", run.Err)
	}
	if len(hosts) != 2 || hosts[0] != "d1.test" || hosts[1] != "d2.test" {
		t.Fatalf("upstream hosts = %v, want [d1.test d2.test] (different pins must not dedupe)", hosts)
	}
	if len(run.Trace) != 2 || run.Trace[0].Outcome != "failed" || run.Trace[1].Outcome != "success" {
		t.Fatalf("trace = %+v, want failed then success", run.Trace)
	}
}

// 同一模型 + 完全相同固定 → 第二条按重复跳过。
// 注意：成功条目会立即短路返回，故去重只能在首条失败、循环继续时才可观察。
func TestRunComboSameModelSamePinDeduped(t *testing.T) {
	installPool(t, nil)
	installConfig(t, defaultProxyConfig())
	installProviders(t,
		&CustomProvider{ID: "prov_sd", Name: "P1", BaseURL: "http://s1.test/v1",
			APIKey: "sk", ModelIDs: []string{"m-r"}, Enabled: true, Priority: 1},
	)

	var calls int
	installTransport(t, func(req *http.Request) *http.Response {
		calls++
		return stubResp(req, http.StatusInternalServerError, `{"error":"down"}`)
	})

	combo := ModelCombo{ID: "c", Name: "C", Enabled: true, Entries: []ComboEntry{
		{Model: "m-r", Source: "provider", Provider: "P1"},
		{Model: "m-r", Source: "provider", Provider: "P1"},
	}}
	run := runCombo(chatParams("c"), combo, false, "")
	if calls != 1 {
		t.Fatalf("upstream calls = %d, want 1 (identical pin must be deduped, not retried)", calls)
	}
	if len(run.Trace) != 2 || run.Trace[0].Outcome != "failed" ||
		run.Trace[1].Outcome != "skipped" || run.Trace[1].Reason != "duplicate entry" {
		t.Fatalf("trace = %+v, want [failed, skipped duplicate]", run.Trace)
	}
	if run.Err == nil {
		t.Fatalf("want non-nil error when every entry fails")
	}
}

// ============================ validateCombo 固定校验 ============================

func TestValidateComboPins(t *testing.T) {
	installPool(t, []Model{
		{ID: "m-a", Cost: "free", Status: "active", Source: "remote"},
		{ID: "zen-free-x", Source: "zen", Cost: "free", Status: "active"},
	})
	installProviders(t,
		&CustomProvider{ID: "prov_v", Name: "P-serves", BaseURL: "http://v.test/v1",
			APIKey: "sk", ModelIDs: []string{"m-a"}, Enabled: true, Priority: 1},
		&CustomProvider{ID: "prov_v2", Name: "P-disabled", BaseURL: "http://v2.test/v1",
			APIKey: "sk", ModelIDs: []string{"m-a"}, Enabled: false, Priority: 1},
	)

	// 合法固定：cline（池内非 zen 模型）/ provider（启用且服务该模型）。
	if err := validateCombo(ModelCombo{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "m-a", Source: "cline"}}}, nil); err != nil {
		t.Fatalf("valid cline pin rejected: %v", err)
	}
	if err := validateCombo(ModelCombo{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "m-a", Source: "provider", Provider: "P-serves"}}}, nil); err != nil {
		t.Fatalf("valid provider pin rejected: %v", err)
	}
	// 空固定行为不变。
	if err := validateCombo(ModelCombo{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "m-a"}}}, nil); err != nil {
		t.Fatalf("empty-pin combo rejected: %v", err)
	}
	// provider 名称大小写/空白不敏感。
	if err := validateCombo(ModelCombo{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "m-a", Source: "provider", Provider: "  p-serves  "}}}, nil); err != nil {
		t.Fatalf("case-insensitive provider pin rejected: %v", err)
	}

	reject := []struct {
		name  string
		entry ComboEntry
	}{
		{"unknown source", ComboEntry{Model: "m-a", Source: "bogus"}},
		{"provider missing name", ComboEntry{Model: "m-a", Source: "provider"}},
		{"provider serves nothing", ComboEntry{Model: "m-a", Source: "provider", Provider: "Nope"}},
		{"provider disabled", ComboEntry{Model: "m-a", Source: "provider", Provider: "P-disabled"}},
		{"zen pin on non-zen model", ComboEntry{Model: "m-a", Source: "zen"}},
	}
	for _, tc := range reject {
		err := validateCombo(ModelCombo{ID: "c", Name: "C", Enabled: true,
			Entries: []ComboEntry{tc.entry}}, nil)
		if err == nil {
			t.Errorf("%s: expected validation error, got nil", tc.name)
		}
	}
}

// ============================ 回归：review 修复 ============================

// CRITICAL 修复：provider-only 模型固定到 cline 必须被拒绝，运行时也必须按
// 不可用跳过（而不是把模型 ID 原样发给 Cline → 400 → 整条组合中止）。
func TestValidateComboRejectsProviderOnlyClinePin(t *testing.T) {
	// 池内条目来源为 provider（provider 同步进来的模型），并非 Cline 账号池可服务。
	installPool(t, []Model{
		{ID: "m-p", Provider: "P-only", Source: "provider", Cost: "free", Status: "active"},
	})
	installProviders(t,
		&CustomProvider{ID: "prov_only", Name: "P-only", BaseURL: "http://only.test/v1",
			APIKey: "sk", ModelIDs: []string{"m-p"}, Enabled: true, Priority: 1},
	)

	// 老的非严格判定仍会认为它「存在」（保证 helper 语义差异被测试锁定）。
	if !clineModelWithIDExists("m-p") {
		t.Fatal("setup: clineModelWithIDExists should treat provider rows as non-zen")
	}
	if clinePoolModelExists("m-p") {
		t.Fatal("setup: clinePoolModelExists must exclude provider-source rows")
	}

	err := validateCombo(ModelCombo{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "m-p", Source: "cline"}}}, nil)
	if err == nil {
		t.Fatal("expected validation error: source=cline pin on provider-only model")
	}
	if got := entryRouteKind(ComboEntry{Model: "m-p", Source: "cline"}); got != "unavailable" {
		t.Fatalf("entryRouteKind(provider-only, source=cline) = %q, want unavailable", got)
	}
}

// HIGH 修复：显式 zen 固定压过 AUTO 的同名 cline 冲突降级 —— pin 必须生效。
func TestEntryRouteKindZenPinWinsOverClineCollision(t *testing.T) {
	installPool(t, []Model{
		{ID: "dual-free", Source: "zen", Cost: "free", Status: "active"},
		// 同名非 zen（provider）行：AUTO 会把 zen 让给 cline。
		{ID: "dual-free", Source: "provider", Provider: "P", Cost: "free", Status: "active"},
	})

	zenConfigMu.Lock()
	oldZC := zenConfig
	zenConfig = defaultZenConfig() // Enabled/Failover=true
	zenConfigMu.Unlock()
	zenStateMu.Lock()
	oldCount, oldUntil := zenFailCount, zenFailUntil
	zenFailCount, zenFailUntil = 0, time.Time{}
	zenStateMu.Unlock()
	t.Cleanup(func() {
		zenConfigMu.Lock()
		zenConfig = oldZC
		zenConfigMu.Unlock()
		zenStateMu.Lock()
		zenFailCount, zenFailUntil = oldCount, oldUntil
		zenStateMu.Unlock()
	})

	// AUTO（空固定）仍让给 cline（既有行为不变）。
	if got := comboRouteKind("dual-free"); got != "cline" {
		t.Fatalf("auto comboRouteKind = %q, want cline (collision diversion unchanged)", got)
	}
	// 显式 zen 固定：pin 胜出，走 zen。
	if got := entryRouteKind(ComboEntry{Model: "dual-free", Source: "zen"}); got != "zen" {
		t.Fatalf("entryRouteKind(zen pin, collision) = %q, want zen", got)
	}
}

// MEDIUM 修复：固定 provider 返回 401（如被轮换的 API key）不得中止整条组合，
// 而应作为可重试跳过，继续尝试下一个条目。
func TestRunComboPinnedProvider401DoesNotAbort(t *testing.T) {
	installPool(t, []Model{{ID: "m-ok", Cost: "free", Status: "active", Source: "remote"}})
	installConfig(t, defaultProxyConfig())
	installProviders(t,
		&CustomProvider{ID: "prov_badkey", Name: "P-bad", BaseURL: "http://bad.test/v1",
			APIKey: "rotated", ModelIDs: []string{"m-auth"}, Enabled: true, Priority: 1},
	)
	installTransport(t, func(req *http.Request) *http.Response {
		if req.URL.Host == "bad.test" {
			return stubResp(req, http.StatusUnauthorized, `{"error":"invalid api key"}`)
		}
		return stubResp(req, http.StatusOK, okCompletion)
	})

	combo := ModelCombo{ID: "c", Name: "C", Enabled: true, Entries: []ComboEntry{
		{Model: "m-auth", Source: "provider", Provider: "P-bad"},
		{Model: "m-ok"},
	}}
	run := runCombo(chatParams("c"), combo, false, "")
	if run.Err != nil {
		t.Fatalf("run err (401 from pinned provider must not abort): %v", run.Err)
	}
	if run.Model != "m-ok" {
		t.Fatalf("served %q, want m-ok (fall through to next entry)", run.Model)
	}
	if len(run.Trace) < 2 {
		t.Fatalf("trace = %+v, want at least 2 entries", run.Trace)
	}
	if run.Trace[0].Outcome != "failed" || run.Trace[0].Source != "provider" || run.Trace[0].Provider != "P-bad" {
		t.Fatalf("trace[0] = %+v, want failed pinned provider attempt", run.Trace[0])
	}
	if run.Trace[1].Outcome != "success" {
		t.Fatalf("trace[1] = %+v, want success", run.Trace[1])
	}
}

// LOW 修复：skipped 轨迹行也要带固定信息，便于后台展示被跳过的固定条目。
func TestRunComboSkippedTraceCarriesPin(t *testing.T) {
	installPool(t, nil)
	installConfig(t, defaultProxyConfig())
	installProviders(t) // 隔离 provider registry：不安装任何 provider
	installTransport(t, func(req *http.Request) *http.Response {
		return stubResp(req, http.StatusOK, okCompletion)
	})

	combo := ModelCombo{ID: "c", Name: "C", Enabled: true, Entries: []ComboEntry{
		{Model: "m-a", Source: "provider", Provider: "P1", Disabled: true},
		{Model: "m-a", Source: "provider", Provider: "P1"},
		{Model: "m-a", Source: "provider", Provider: "P1"},
	}}
	run := runCombo(chatParams("c"), combo, false, "")
	if len(run.Trace) != 3 {
		t.Fatalf("trace = %+v, want 3 entries", run.Trace)
	}
	if run.Trace[0].Outcome != "skipped" || run.Trace[0].Source != "provider" || run.Trace[0].Provider != "P1" {
		t.Fatalf("disabled skip row missing pin: %+v", run.Trace[0])
	}
	if run.Trace[2].Outcome != "skipped" || run.Trace[2].Reason != "duplicate entry" ||
		run.Trace[2].Source != "provider" || run.Trace[2].Provider != "P1" {
		t.Fatalf("duplicate skip row missing pin: %+v", run.Trace[2])
	}
}
