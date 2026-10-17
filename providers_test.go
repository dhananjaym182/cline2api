package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 自定义 provider 命中时优先走 provider 上游。
func TestCustomProviderServesModel(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
		_ = deleteProvider("prov_test1")
		resetRoutingState()
	})

	pool = &AccountPool{Accounts: []*Account{{
		AccountID: "a", Email: "a@x.com", AccessToken: "t",
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), Status: "active",
	}}}
	setProxyConfig(defaultProxyConfig())

	upstreamModel := ""
	providerCalls := 0
	clineCalls := 0
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "provider.test" {
			providerCalls++
			body, _ := io.ReadAll(req.Body)
			var params map[string]any
			json.Unmarshal(body, &params)
			upstreamModel, _ = params["model"].(string)
			// 校验自定义头
			if req.Header.Get("X-Custom") != "abc" {
				t.Fatalf("custom header missing, got %q", req.Header.Get("X-Custom"))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"id":"p1","choices":[{"message":{"role":"assistant","content":"from-provider"}}]}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		clineCalls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"c1","choices":[{"message":{"role":"assistant","content":"from-cline"}}]}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	upsertProvider(&CustomProvider{
		ID: "prov_test1", Name: "TestProv", BaseURL: "http://provider.test/v1",
		APIKey: "sk-x", ModelIDs: []string{"custom-model-1"},
		Headers: map[string]string{"X-Custom": "abc"}, Enabled: true, Priority: 10,
	})

	params := map[string]any{"model": "custom-model-1", "max_tokens": 16, "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	resp, acc, err := callClineAPI(params, false)
	if err != nil {
		t.Fatalf("expected provider success, got %v", err)
	}
	defer resp.Body.Close()
	if acc != nil {
		t.Fatal("provider-served request should not consume a cline account")
	}
	if providerCalls != 1 || clineCalls != 0 {
		t.Fatalf("providerCalls=%d clineCalls=%d, want 1/0", providerCalls, clineCalls)
	}
	if upstreamModel != "custom-model-1" {
		t.Fatalf("upstream model = %q", upstreamModel)
	}
}

// provider 失败（冷却）后自动降级到回退链 → cline 池，客户端无感知。
func TestCustomProviderFailureFallsBackToChain(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
		_ = deleteProvider("prov_test2")
		resetRoutingState()
	})

	pool = &AccountPool{Accounts: []*Account{{
		AccountID: "a", Email: "a@x.com", AccessToken: "t",
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), Status: "active",
	}}}
	setProxyConfig(defaultProxyConfig())

	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "provider.test" {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader(`{"error":"boom"}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"c1","choices":[{"message":{"role":"assistant","content":"from-cline"}}]}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	upsertProvider(&CustomProvider{
		ID: "prov_test2", Name: "BrokenProv", BaseURL: "http://provider.test/v1",
		APIKey: "sk-x", ModelIDs: []string{"z-ai/glm-5.3-flash"},
		Enabled: true, Priority: 10,
	})
	defer setProviderCooldown("prov_test2", "z-ai/glm-5.3-flash", time.Time{})

	params := map[string]any{"model": "z-ai/glm-5.3-flash", "max_tokens": 16, "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	resp, _, err := callClineAPI(params, false)
	if err != nil {
		t.Fatalf("expected fallback success, got %v", err)
	}
	defer resp.Body.Close()
}

// 优先级：同模型多 provider 时取 priority 最小的可用者。
func TestProviderPrioritySelection(t *testing.T) {
	t.Cleanup(func() {
		_ = deleteProvider("prov_p1")
		_ = deleteProvider("prov_p2")
	})
	upsertProvider(&CustomProvider{ID: "prov_p1", Name: "Low", BaseURL: "http://a/v1", ModelIDs: []string{"m"}, Enabled: true, Priority: 5})
	upsertProvider(&CustomProvider{ID: "prov_p2", Name: "High", BaseURL: "http://b/v1", ModelIDs: []string{"m"}, Enabled: true, Priority: 1})
	p := resolveProviderForModel("m")
	if p == nil || p.Name != "High" {
		t.Fatalf("want High (priority 1), got %+v", p)
	}
	// 冷却 High 后选 Low
	setProviderCooldown("prov_p2", "m", time.Now().Add(time.Minute))
	p = resolveProviderForModel("m")
	if p == nil || p.Name != "Low" {
		t.Fatalf("after cooldown want Low, got %+v", p)
	}
}

// syncProviderModels：provider 声明的模型同步进池（列表可见/可启停）；
// provider 删除后条目清理；同 ID 已有其他来源条目时不重复添加；
// 用户改过的 Disabled / MetaLocked 不被覆盖。
func TestSyncProviderModels(t *testing.T) {
	oldPool, oldPath := pool, poolPath
	oldProvPath := providersFilePath
	oldProviders := append([]*CustomProvider(nil), customProviders...)
	oldLoaded := providersLoaded
	t.Cleanup(func() {
		pool, poolPath = oldPool, oldPath
		providersFilePath = oldProvPath
		customProviders = oldProviders
		providersLoaded = oldLoaded
	})
	// provider 文件也指向临时目录：upsertProvider/deleteProvider 会落盘，
	// 不能污染仓库根目录里真实的 .cline-providers.json
	providersFilePath = filepath.Join(t.TempDir(), ".cline-providers.json")
	poolPath = filepath.Join(t.TempDir(), ".cline-accounts.json")
	pool = &AccountPool{Models: []Model{
		{ID: "z-ai/glm-5.3-flash", Provider: "z-ai", Cost: "free", Status: "active", Source: "remote"},
	}, DefaultModel: "prov-only"}
	customProviders = nil
	providersLoaded = true // 阻止读真实 .cline-providers.json

	upsertProvider(&CustomProvider{
		ID: "prov_sync", Name: "SyncProv", BaseURL: "http://sync.test/v1",
		ModelIDs: []string{"prov-only", "z-ai/glm-5.3-flash"},
		Enabled:  true, Free: true, Priority: 10,
	})
	syncProviderModels()

	p := loadPool()
	var provOnly, shared *Model
	for i := range p.Models {
		switch p.Models[i].ID {
		case "prov-only":
			provOnly = &p.Models[i]
		case "z-ai/glm-5.3-flash":
			if p.Models[i].Source == "provider" {
				t.Error("shared model should not be duplicated with Source=provider")
			}
			shared = &p.Models[i]
		}
	}
	if provOnly == nil {
		t.Fatal("prov-only model should be synced into pool")
	}
	if provOnly.Source != "provider" || provOnly.Provider != "SyncProv" || provOnly.Cost != "free" {
		t.Errorf("synced entry = %+v, want Source=provider Provider=SyncProv Cost=free", provOnly)
	}
	if shared == nil || shared.Source != "remote" {
		t.Error("existing remote entry should be kept untouched")
	}

	// 用户停用 + 锁定元数据：再次同步不覆盖
	for i := range pool.Models {
		if pool.Models[i].ID == "prov-only" {
			pool.Models[i].Disabled = true
			pool.Models[i].Context = 42
			pool.Models[i].MetaLocked = true
		}
	}
	syncProviderModels()
	p = loadPool()
	for _, m := range p.Models {
		if m.ID == "prov-only" && (m.Disabled != true || m.Context != 42 || m.MetaLocked != true) {
			t.Errorf("user edits overwritten: %+v", m)
		}
	}

	// 重新启用后，免费 provider 模型参与默认回退链
	for i := range pool.Models {
		if pool.Models[i].ID == "prov-only" {
			pool.Models[i].Disabled = false
		}
	}
	chain := defaultFreeChain()
	found := false
	for _, id := range chain {
		if id == "prov-only" {
			found = true
		}
	}
	if !found {
		t.Errorf("defaultFreeChain %v should include free provider model prov-only", chain)
	}

	// provider 删除 → 池内条目清理；指向它的默认模型清空
	deleteProvider("prov_sync")
	syncProviderModels()
	p = loadPool()
	for _, m := range p.Models {
		if m.ID == "prov-only" {
			t.Error("provider model should be removed after provider deletion")
		}
	}
	if p.DefaultModel != "" {
		t.Errorf("DefaultModel = %q, want cleared after its model is removed", p.DefaultModel)
	}
}

// stampUpstream 归因优先级：provider 标记 > zen 模型 > 默认 cline。
func TestStampUpstreamAttribution(t *testing.T) {
	var rl RequestLog

	stampUpstream(&rl, map[string]any{"model": "custom-model-1", servedByParam: upstreamProvider})
	if rl.Upstream != upstreamProvider {
		t.Fatalf("provider-marked upstream = %q, want %q", rl.Upstream, upstreamProvider)
	}

	stampUpstream(&rl, map[string]any{"model": "z-ai/glm-5.3-flash"})
	if rl.Upstream != upstreamCline {
		t.Fatalf("plain model upstream = %q, want %q", rl.Upstream, upstreamCline)
	}

	// 无标记时绝不误判为 provider
	stampUpstream(&rl, map[string]any{})
	if rl.Upstream != upstreamCline {
		t.Fatalf("empty params upstream = %q, want %q", rl.Upstream, upstreamCline)
	}
}

// model="free" 别名链必须与显式链一致：命中自定义 provider 时优先走 provider，
// 而不是无条件只用 Cline 账号池；成功后 params 留下归因标记。
func TestFreeAliasUsesCustomProvider(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
		_ = deleteProvider("prov_free1")
		resetRoutingState()
	})

	pool = &AccountPool{Accounts: []*Account{{
		AccountID: "a", Email: "a@x.com", AccessToken: "t",
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), Status: "active",
	}}}
	cfg := defaultProxyConfig()
	cfg.ModelChain = []string{"custom-model-free"}
	setProxyConfig(cfg)

	providerCalls := 0
	clineCalls := 0
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "provider.test" {
			providerCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"id":"p1","choices":[{"message":{"role":"assistant","content":"from-provider"}}]}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		clineCalls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"c1","choices":[{"message":{"role":"assistant","content":"from-cline"}}]}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	upsertProvider(&CustomProvider{
		ID: "prov_free1", Name: "FreeChainProv", BaseURL: "http://provider.test/v1",
		APIKey: "sk-x", ModelIDs: []string{"custom-model-free"}, Enabled: true, Priority: 10,
	})

	params := map[string]any{"model": "free", "max_tokens": 16, "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	resp, acc, err := callClineAPI(params, false)
	if err != nil {
		t.Fatalf("expected provider success via free chain, got %v", err)
	}
	defer resp.Body.Close()
	if providerCalls != 1 || clineCalls != 0 {
		t.Fatalf("providerCalls=%d clineCalls=%d, want 1/0", providerCalls, clineCalls)
	}
	if acc != nil {
		t.Fatal("provider-served free request should not consume a cline account")
	}
	if params[servedByParam] != upstreamProvider {
		t.Fatalf("servedBy marker = %v, want %q", params[servedByParam], upstreamProvider)
	}
	var rl RequestLog
	stampUpstream(&rl, params)
	if rl.Upstream != upstreamProvider {
		t.Fatalf("log upstream = %q, want %q", rl.Upstream, upstreamProvider)
	}
}

// 换取测试用隔离 provider 注册表：临时文件路径 + 空内存表，防止真实
// .cline-providers.json / 冷却落盘渗入测试。并附带清空路由痕迹状态
// （粘贴会话 / 实测成绩 / 预算计数），用后恢复。
func useIsolatedProviderRegistry(t *testing.T) {
	t.Helper()
	oldPath := providersFilePath
	oldProviders := append([]*CustomProvider(nil), customProviders...)
	oldLoaded := providersLoaded
	oldCooldowns := make(map[string]time.Time, len(providerCooldowns))
	for k, v := range providerCooldowns {
		oldCooldowns[k] = v
	}
	providersFilePath = filepath.Join(t.TempDir(), ".cline-providers.json")
	customProviders = nil
	providersLoaded = true
	for k := range providerCooldowns {
		delete(providerCooldowns, k)
	}
	isoCooldownFile(t)
	resetRoutingState()
	t.Cleanup(func() {
		providersFilePath = oldPath
		customProviders = oldProviders
		providersLoaded = oldLoaded
		for k := range providerCooldowns {
			delete(providerCooldowns, k)
		}
		for k, v := range oldCooldowns {
			providerCooldowns[k] = v
		}
		resetRoutingState()
	})
}

// isoCooldownFile 把冷却落盘路径指到临时目录，测试期间不污染真实文件。
func isoCooldownFile(t *testing.T) {
	t.Helper()
	old := providersCooldownsPath
	providersCooldownsPath = filepath.Join(t.TempDir(), ".cline-providers-cooldowns.json")
	t.Cleanup(func() { providersCooldownsPath = old })
}

// resetRoutingState 清空粘贴会话 / 实测成绩 / 预算计数三张路由痕迹表。
func resetRoutingState() {
	stickyMu.Lock()
	stickySessions = map[string]stickyPin{}
	stickyMu.Unlock()
	scoreMu.Lock()
	providerScores = map[string]*providerScore{}
	scoreMu.Unlock()
	budgetMu.Lock()
	providerRPM = map[string][]time.Time{}
	providerDaily = map[string]map[string]int64{}
	budgetMu.Unlock()
}

// providerCooldownUntilFor 在锁内读某 provider:model 的冷却截止时间（无条目返回零值）。
func providerCooldownUntilFor(providerID, model string) time.Time {
	providersMu.Lock()
	defer providersMu.Unlock()
	return providerCooldowns[providerID+":"+model]
}

func stubProviderResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    &http.Request{},
	}
}

// 同一模型多个 provider 形成冗余组：首选失败立即换组内下一家，而不是把请求
// 降级到回退链上的另一个模型。421（网关改道）尤其不该冷却首选 —— 组内换下
// 家重试通常立刻命中，客户端拿到的始终是它点名的模型。
func TestProviderGroupFailover(t *testing.T) {
	useIsolatedProviderRegistry(t)
	oldTransport := httpClient.Transport
	t.Cleanup(func() { httpClient.Transport = oldTransport })

	upsertProvider(&CustomProvider{ID: "prov_g1", Name: "Primary", BaseURL: "http://a.test/v1", APIKey: "k1", ModelIDs: []string{"m"}, Enabled: true, Priority: 1})
	upsertProvider(&CustomProvider{ID: "prov_g2", Name: "Backup", BaseURL: "http://b.test/v1", APIKey: "k2", ModelIDs: []string{"m"}, Enabled: true, Priority: 2})
	t.Cleanup(func() {
		_ = deleteProvider("prov_g1")
		_ = deleteProvider("prov_g2")
	})

	aCalls, bCalls := 0, 0
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "a.test":
			aCalls++
			return stubProviderResponse(http.StatusMisdirectedRequest, `421 misdirected`), nil
		case "b.test":
			bCalls++
			return stubProviderResponse(http.StatusOK, `{"id":"b1","choices":[{"message":{"role":"assistant","content":"from-backup"}}]}`), nil
		}
		return stubProviderResponse(http.StatusTeapot, "unexpected host"), nil
	})

	params := map[string]any{"model": "m", "max_tokens": 16, "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	resp, err, attempted := callCustomProviderAPI(params, false)
	if !attempted || err != nil {
		t.Fatalf("expected backup success, attempted=%v err=%v", attempted, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(raw), "from-backup") {
		t.Fatalf("served by wrong provider: %s", raw)
	}
	if aCalls != 1 || bCalls != 1 {
		t.Fatalf("aCalls=%d bCalls=%d, want 1/1", aCalls, bCalls)
	}
	if until := providerCooldownUntilFor("prov_g1", "m"); !until.IsZero() {
		t.Errorf("421 should not cool the primary (got until %v)", until)
	}
	if until := providerCooldownUntilFor("prov_g2", "m"); !until.IsZero() {
		t.Errorf("healthy backup should not be cooled (got until %v)", until)
	}

	// 组内部分失败不影响下一次请求的重新选择：首选未被冷却，下轮仍先试首选
	if first := resolveProviderForModel("m"); first == nil || first.Name != "Primary" {
		t.Fatalf("next request should try primary first, got %+v", first)
	}
}

// 冷却按错误类别区分：重试会自愈的失败只短冻结，配置类错误长冻结，
// 重试永不自愈的错误不该把健康 provider 冻在冰里。
func TestProviderCooldownClassification(t *testing.T) {
	useIsolatedProviderRegistry(t)
	oldTransport := httpClient.Transport
	t.Cleanup(func() { httpClient.Transport = oldTransport })

	newProv := func(id string) *CustomProvider {
		p := CustomProvider{ID: id, Name: id, BaseURL: "http://c.test/v1", APIKey: "k", ModelIDs: []string{"m"}, Enabled: true, Priority: 1}
		cs := append([]*CustomProvider(nil), customProviders...)
		customProviders = append(cs, &p)
		return &p
	}

	cases := []struct {
		name             string
		status           int
		retryAfter       string
		body             string
		wantMin, wantMax time.Duration // 冷却时长落在该区间；零值区间表示不应冷却
	}{
		{"429 with Retry-After", 429, "5", "rate limited", 4 * time.Second, 7 * time.Second},
		{"429 without Retry-After", 429, "", "rate limited", 1 * time.Minute, 3 * time.Minute},
		{"421 plain misdirect", 421, "", "misdirected", 0, 0},
		{"421 with quota body", 421, "", "quota exceeded for billing", 25 * time.Second, 35 * time.Second},
		{"500 transient", 500, "", "boom", 25 * time.Second, 35 * time.Second},
		{"401 config", 401, "", "unauthorized", 29 * time.Minute, 31 * time.Minute},
	}

	for i, tc := range cases {
		p := newProv(fmt.Sprintf("prov_cls_%d", i))
		t.Cleanup(func() {
			_ = deleteProvider(p.ID)
			setProviderCooldown(p.ID, "m", time.Time{})
		})
		httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
			resp := stubProviderResponse(tc.status, tc.body)
			if tc.retryAfter != "" {
				resp.Header.Set("Retry-After", tc.retryAfter)
			}
			return resp, nil
		})

		params := map[string]any{"model": "m", "max_tokens": 16, "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
		_, _ = callProvider(p, params, false)

		until := providerCooldownUntilFor(p.ID, "m")
		if tc.wantMin == 0 && tc.wantMax == 0 {
			if !until.IsZero() && until.After(time.Now()) {
				t.Errorf("%s: expected no cooldown, got until %v", tc.name, until)
			}
			continue
		}
		if until.IsZero() {
			t.Errorf("%s: expected cooldown, got none", tc.name)
			continue
		}
		d := time.Until(until)
		if d < tc.wantMin || d > tc.wantMax {
			t.Errorf("%s: cooldown %v not in [%v, %v]", tc.name, d, tc.wantMin, tc.wantMax)
		}
	}
}

// 粘贴会话：同一会话在 TTL 内钉在同一家健康 provider 上，即使更高优先级的
// 冷却恢复后也不中途换源（避免会话里模型风格突变）；唯一例外是被钉的源
// 失效（冷却/停用）时立即解钉转移，新会话不受影响。
func TestProviderStickySession(t *testing.T) {
	useIsolatedProviderRegistry(t)
	cfg := defaultProxyConfig()
	setProxyConfig(cfg)

	upsertProvider(&CustomProvider{ID: "prov_s1", Name: "StickyPrimary", BaseURL: "http://sa.test/v1", APIKey: "k", ModelIDs: []string{"m"}, Enabled: true, Priority: 1})
	upsertProvider(&CustomProvider{ID: "prov_s2", Name: "StickyBackup", BaseURL: "http://sb.test/v1", APIKey: "k", ModelIDs: []string{"m"}, Enabled: true, Priority: 2})
	t.Cleanup(func() {
		_ = deleteProvider("prov_s1")
		_ = deleteProvider("prov_s2")
	})

	saCalls, sbCalls := 0, 0
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "sa.test":
			saCalls++
			return stubProviderResponse(http.StatusOK, `{"id":"a","choices":[{"message":{"role":"assistant","content":"from-a"}}]}`), nil
		case "sb.test":
			sbCalls++
			return stubProviderResponse(http.StatusOK, `{"id":"b","choices":[{"message":{"role":"assistant","content":"from-b"}}]}`), nil
		}
		return stubProviderResponse(http.StatusTeapot, "unexpected"), nil
	})

	// 同一「会话」：首条 user 消息内容一致
	makeParams := func(msg string) map[string]any {
		return map[string]any{"model": "m", "max_tokens": 16, "messages": []any{map[string]any{"role": "user", "content": msg}}}
	}
	// 不同内容 → 不同会话键
	call := func(params map[string]any) (*http.Response, error) {
		resp, err, attempted := callCustomProviderAPI(params, false)
		if !attempted {
			t.Fatal("expected provider attempt")
		}
		return resp, err
	}

	// 第 1 拍：优先级最高的 Primary 服务并上钉
	resp, err := call(makeParams("sess-alpha"))
	if err != nil {
		t.Fatalf("req1 failed: %v", err)
	}
	resp.Body.Close()
	if saCalls != 1 || sbCalls != 0 {
		t.Fatalf("req1 calls=%d/%d, want 1/0", saCalls, sbCalls)
	}
	sk := sessionKeyFromParams(makeParams("sess-alpha"))
	if pin := stickyGet(sk); pin == nil || pin.providerID != "prov_s1" {
		t.Fatalf("req1 should pin prov_s1, got %+v", pin)
	}

	// 第 2 拍：Primary 被冷却 → 立即解钉，Backup 服务并重新上钉
	setProviderCooldown("prov_s1", "m", time.Now().Add(time.Minute))
	defer setProviderCooldown("prov_s1", "m", time.Time{})
	resp, err = call(makeParams("sess-alpha"))
	if err != nil {
		t.Fatalf("req2 failed: %v", err)
	}
	resp.Body.Close()
	if saCalls != 1 || sbCalls != 1 {
		t.Fatalf("req2 calls=%d/%d, want 1/1", saCalls, sbCalls)
	}
	if pin := stickyGet(sk); pin == nil || pin.providerID != "prov_s2" {
		t.Fatalf("req2 should re-pin prov_s2, got %+v", pin)
	}

	// 第 3 拍：Primary 冷却已解除，但会话仍钉在 Backup 上 —— 不换源
	delete(providerCooldowns, "prov_s1:m")
	resp, err = call(makeParams("sess-alpha"))
	if err != nil {
		t.Fatalf("req3 failed: %v", err)
	}
	resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(raw), "from-b") {
		t.Fatalf("req3 should stay pinned to backup, got %s", raw)
	}
	if saCalls != 1 || sbCalls != 2 {
		t.Fatalf("req3 calls=%d/%d, want 1/2 (no switch back mid-session)", saCalls, sbCalls)
	}

	// 新会话（不同首条消息）不受旧钉影响：仍然先试优先级最高的 Primary
	resp, err = call(makeParams("sess-beta"))
	if err != nil {
		t.Fatalf("req4 failed: %v", err)
	}
	resp.Body.Close()
	if saCalls != 2 || sbCalls != 2 {
		t.Fatalf("req4 calls=%d/%d, want 2/2", saCalls, sbCalls)
	}
}

// quality 策略：实测成功率高的 provider 优先于高优先级但失败率高的。
func TestQualityStrategySorting(t *testing.T) {
	useIsolatedProviderRegistry(t)
	oldConfig := getProxyConfig()
	cfg := defaultProxyConfig()
	cfg.Strategy = "quality"
	setProxyConfig(cfg)
	t.Cleanup(func() { setProxyConfig(oldConfig) })

	upsertProvider(&CustomProvider{ID: "prov_q1", Name: "BadButFirst", BaseURL: "http://qa.test/v1", ModelIDs: []string{"m"}, Enabled: true, Priority: 1})
	upsertProvider(&CustomProvider{ID: "prov_q2", Name: "GoodButSecond", BaseURL: "http://qb.test/v1", ModelIDs: []string{"m"}, Enabled: true, Priority: 2})
	t.Cleanup(func() {
		_ = deleteProvider("prov_q1")
		_ = deleteProvider("prov_q2")
	})

	// 无样本：中性值 → 配置优先级决胜
	if first := resolveProviderForModel("m"); first == nil || first.Name != "BadButFirst" {
		t.Fatalf("no samples: should keep priority order, got %+v", first)
	}
	// BadButFirst 连续失败、GoodButSecond 全成功 → 成绩决胜负
	for i := 0; i < 4; i++ {
		recordProviderOutcome("prov_q1", "m", false, 10)
	}
	for i := 0; i < 4; i++ {
		recordProviderOutcome("prov_q2", "m", true, 30)
	}
	if first := resolveProviderForModel("m"); first == nil || first.Name != "GoodButSecond" {
		t.Fatalf("quality strategy should promote the healthy provider, got %+v", first)
	}
}

// 预算上限：超限 provider 被路由剔除，不再拿 429 试错。
func TestProviderBudgetExcluded(t *testing.T) {
	useIsolatedProviderRegistry(t)

	upsertProvider(&CustomProvider{ID: "prov_b1", Name: "Capped", BaseURL: "http://capped.test/v1", ModelIDs: []string{"m"}, Enabled: true, Priority: 1, RpmLimit: 1})
	t.Cleanup(func() { _ = deleteProvider("prov_b1") })

	if cs := resolveProvidersForModel("m"); len(cs) != 1 {
		t.Fatalf("fresh provider should be eligible, got %d candidates", len(cs))
	}
	p := &CustomProvider{ID: "prov_b1", Name: "Capped", BaseURL: "http://capped.test/v1", ModelIDs: []string{"m"}, Enabled: true, Priority: 1, RpmLimit: 1}
	params := map[string]any{"model": "m", "max_tokens": 16, "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		return stubProviderResponse(http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`), nil
	})
	resp, err := callProvider(p, params, false)
	if err != nil {
		t.Fatalf("first call under cap should pass, got %v", err)
	}
	resp.Body.Close()
	// 已发出 1 次 ≥ RpmLimit：不该再入候选
	if cs := resolveProvidersForModel("m"); len(cs) != 0 {
		t.Fatalf("provider over RPM cap must be excluded, got %d candidates", len(cs))
	}

	// 日预算同理
	p2 := &CustomProvider{ID: "prov_b2", Name: "DailyCapped", BaseURL: "http://capped2.test/v1", ModelIDs: []string{"m"}, Enabled: true, Priority: 1, DailyLimit: 1}
	t.Cleanup(func() { _ = deleteProvider("prov_b2") })
	resp, err = callProvider(p2, params, false)
	if err != nil {
		t.Fatalf("daily first call should pass, got %v", err)
	}
	resp.Body.Close()
	if cs := resolveProvidersForModel("m"); len(cs) != 0 {
		t.Fatalf("provider over daily cap must be excluded, got %d candidates", len(cs))
	}
}

// 冷却落盘：写入的冷却重启后仍在（读盘校验），过期条目不落盘。
func TestProviderCooldownPersistence(t *testing.T) {
	useIsolatedProviderRegistry(t)

	future := time.Now().Add(2 * time.Minute)
	setProviderCooldown("prov_px", "m1", future)
	setProviderCooldown("prov_px", "m2", time.Now().Add(-time.Minute)) // 已过期，不应落盘

	data, err := os.ReadFile(providersCooldownsPath)
	if err != nil {
		t.Fatalf("cooldown file missing: %v", err)
	}
	var raw map[string]int64
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("cooldown file corrupt: %v", err)
	}
	until, ok := raw["prov_px:m1"]
	if !ok {
		t.Fatal("fresh cooldown should be persisted")
	}
	if got := time.UnixMicro(until); got.Sub(future).Abs() > time.Second {
		t.Errorf("persisted until = %v, want ≈ %v", got, future)
	}
	if _, ok := raw["prov_px:m2"]; ok {
		t.Error("expired cooldown must not be persisted")
	}

	// 内存态与落盘一致：重启场景 = 清空内存后读盘
	delete(providerCooldowns, "prov_px:m1")
	providersMu.Lock()
	loadProviderCooldownsLocked()
	providersMu.Unlock()
	if until := providerCooldownUntilFor("prov_px", "m1"); until.IsZero() || until.After(time.Now().Add(3*time.Minute)) {
		t.Errorf("reload should restore cooldown for m1, got %v", until)
	}
}
