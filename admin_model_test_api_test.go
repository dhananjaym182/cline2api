package main

// 本地测试（按仓库策略 *_test.go 不提交）：EP1 provider 未保存测试 / EP2 模型发现
// / EP3 单模型测试 / EP4 组合测试。全部走假 transport，不触网。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// installZenClient 把 zen 出站 client 换成假 client（zen 走独立 transport，
// installTransport 拦不到）；用完还原，杜绝测试触网。
func installZenClient(t *testing.T, fn func(req *http.Request) *http.Response) {
	t.Helper()
	old := zenHTTPClient
	t.Cleanup(func() { zenHTTPClient = old })
	zenHTTPClient = &http.Client{Transport: freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		return fn(req), nil
	})}
}

// postAdminJSON 直接调用 handler，返回解析后的 apiResponse 与状态码。
func postAdminJSON(t *testing.T, h http.HandlerFunc, body string) (int, apiResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/api/test", strings.NewReader(body))
	h(rec, req)
	var resp apiResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rec.Body.String())
	}
	return rec.Code, resp
}

func dataMap(t *testing.T, resp apiResponse) map[string]any {
	t.Helper()
	m, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("data is not an object: %v (%T)", resp.Data, resp.Data)
	}
	return m
}

// ============================ EP1 providers/test ============================

func TestProviderTestUnsavedOK(t *testing.T) {
	installPool(t, nil)
	installConfig(t, defaultProxyConfig())
	var gotPath, gotAuth, gotModel string
	installTransport(t, func(req *http.Request) *http.Response {
		gotPath = req.URL.Path
		gotAuth = req.Header.Get("Authorization")
		gotModel = readReqModel(req)
		return stubResp(req, http.StatusOK, okCompletion)
	})
	code, resp := postAdminJSON(t, handleProviderTest,
		`{"baseURL":"http://up.test/v1","apiKey":"sk-test","model":"unsaved-m"}`)
	if code != 200 || !resp.Success {
		t.Fatalf("status=%d success=%v err=%s", code, resp.Success, resp.Error)
	}
	d := dataMap(t, resp)
	if d["ok"] != true {
		t.Fatalf("ok = %v, want true (d=%v)", d["ok"], d)
	}
	if d["model"] != "unsaved-m" || gotModel != "unsaved-m" {
		t.Fatalf("model = %v (upstream saw %q), want unsaved-m", d["model"], gotModel)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth = %q, want Bearer sk-test", gotAuth)
	}
	if _, hasTok := d["inputTokens"].(float64); !hasTok {
		t.Fatalf("usage not parsed: %v", d)
	}
}

func TestProviderTestUnsavedModelRequired(t *testing.T) {
	installPool(t, nil)
	installConfig(t, defaultProxyConfig())
	installTransport(t, func(req *http.Request) *http.Response {
		t.Fatalf("no upstream call expected")
		return stubResp(req, 500, "")
	})
	code, resp := postAdminJSON(t, handleProviderTest, `{"baseURL":"http://up.test/v1"}`)
	if code != 200 || !resp.Success {
		t.Fatalf("status=%d success=%v", code, resp.Success)
	}
	d := dataMap(t, resp)
	if d["ok"] != false || !strings.Contains(d["error"].(string), "model is required") {
		t.Fatalf("expected ok=false with model-required error, got %v", d)
	}
}

func TestProviderTestSavedModelOverride(t *testing.T) {
	installPool(t, nil)
	installConfig(t, defaultProxyConfig())
	installProviders(t, &CustomProvider{ID: "p1", Name: "P1", BaseURL: "http://p.test/v1",
		ModelIDs: []string{"first-m", "second-m"}, Enabled: true})
	var gotModel string
	installTransport(t, func(req *http.Request) *http.Response {
		gotModel = readReqModel(req)
		return stubResp(req, http.StatusOK, okCompletion)
	})
	_, resp := postAdminJSON(t, handleProviderTest, `{"id":"p1","model":"second-m"}`)
	if d := dataMap(t, resp); d["model"] != "second-m" || gotModel != "second-m" {
		t.Fatalf("model = %v (upstream saw %q), want second-m", d["model"], gotModel)
	}
}

func TestProviderTestSavedUpstreamErrorIsOKEnvelope(t *testing.T) {
	installPool(t, nil)
	installConfig(t, defaultProxyConfig())
	installProviders(t, &CustomProvider{ID: "p1", Name: "P1", BaseURL: "http://p.test/v1",
		ModelIDs: []string{"m"}, Enabled: true})
	installTransport(t, func(req *http.Request) *http.Response {
		return stubResp(req, http.StatusUnauthorized, `{"error":"bad key"}`)
	})
	code, resp := postAdminJSON(t, handleProviderTest, `{"id":"p1"}`)
	if code != 200 || !resp.Success {
		t.Fatalf("status=%d success=%v", code, resp.Success)
	}
	d := dataMap(t, resp)
	if d["ok"] != false || !strings.Contains(d["error"].(string), "401") {
		t.Fatalf("expected ok=false error mentioning 401, got %v", d)
	}
}

// ============================ EP2 providers/models ========================

func TestProviderModelsDiscovery(t *testing.T) {
	installConfig(t, defaultProxyConfig())
	// 场景一：baseURL 已含 /v1，直接命中 /v1/models
	var gotAuth string
	installTransport(t, func(req *http.Request) *http.Response {
		gotAuth = req.Header.Get("Authorization")
		return stubResp(req, http.StatusOK, `{"data":[{"id":"b"},{"id":"a"},{"id":"b"}]}`)
	})
	code, resp := postAdminJSON(t, handleProviderModels,
		`{"baseURL":"http://m.test/v1","apiKey":"sk-k"}`)
	if code != 200 || !resp.Success || resp.Error != "" {
		t.Fatalf("status=%d success=%v err=%s", code, resp.Success, resp.Error)
	}
	d := dataMap(t, resp)
	if d["count"] != float64(2) {
		t.Fatalf("count = %v, want 2 (d=%v)", d["count"], d)
	}
	if gotAuth != "Bearer sk-k" {
		t.Fatalf("auth = %q, want Bearer sk-k", gotAuth)
	}
	models, _ := d["models"].([]any)
	if len(models) != 2 || models[0].(map[string]any)["id"] != "a" {
		t.Fatalf("models not deduped/sorted: %v", models)
	}

	// 场景二：baseURL 不含 /v1 → 首选 /models 404 后补一发 /v1/models
	installTransport(t, func(req *http.Request) *http.Response {
		if req.URL.Path == "/models" {
			return stubResp(req, http.StatusNotFound, `not found`)
		}
		return stubResp(req, http.StatusOK, `{"data":[{"id":"z"}]}`)
	})
	_, resp = postAdminJSON(t, handleProviderModels, `{"baseURL":"http://m.test"}`)
	d = dataMap(t, resp)
	if d["count"] != float64(1) {
		t.Fatalf("/v1/models fallback failed: %v", d)
	}

	// 场景三：鉴权失败 → 200 包裹可解析错误 + models 空
	installTransport(t, func(req *http.Request) *http.Response {
		return stubResp(req, http.StatusUnauthorized, `bad key`)
	})
	_, resp = postAdminJSON(t, handleProviderModels, `{"baseURL":"http://m.test/v1","apiKey":"sk-bad"}`)
	nl, _ := resp.Data.(map[string]any)["models"].([]any)
	if !resp.Success || resp.Data == nil || len(nl) != 0 {
		t.Fatalf("expected models empty on auth failure: %v", resp.Data)
	}
	if e := dataMap(t, resp)["error"].(string); !strings.Contains(e, "HTTP 401") || !strings.Contains(e, "bad key") {
		t.Fatalf("error not parseable (want status + body): %q", e)
	}
}

func TestProviderModelsById(t *testing.T) {
	installPool(t, nil)
	installConfig(t, defaultProxyConfig())
	installProviders(t, &CustomProvider{ID: "pd", Name: "PD", BaseURL: "http://pd.test/v1",
		APIKey: "sk-pd", Enabled: true})
	installTransport(t, func(req *http.Request) *http.Response {
		return stubResp(req, http.StatusOK, `{"data":[{"id":"pd-1"}]}`)
	})
	_, resp := postAdminJSON(t, handleProviderModels, `{"id":"pd"}`)
	d := dataMap(t, resp)
	if d["count"] != float64(1) {
		t.Fatalf("by-id discovery failed: %v", d)
	}
}

// ============================ EP3 models/test =============================

func TestQuickModelTestRoutes(t *testing.T) {
	installPool(t, []Model{
		{ID: "mprov", Cost: "pass", Status: "active"},
		{ID: "mcline", Cost: "pass", Status: "active", Source: "remote"},
		{ID: "zen-free-x", Source: "zen", Cost: "free", Status: "active"},
	})
	installConfig(t, defaultProxyConfig())
	installProviders(t, &CustomProvider{ID: "pp", Name: "PP", BaseURL: "http://pp.test/v1",
		ModelIDs: []string{"mprov"}, Enabled: true})
	installZenClient(t, func(req *http.Request) *http.Response {
		return stubResp(req, http.StatusOK, okCompletion)
	})
	installTransport(t, func(req *http.Request) *http.Response {
		if req.URL.Host == "pp.test" {
			return stubResp(req, http.StatusOK, okCompletion)
		}
		// cline 账号池上游按 200 小补全应答
		return stubResp(req, http.StatusOK, okCompletion)
	})

	res := quickModelTest("mprov", nil)
	if res["ok"] != true || res["upstream"] != upstreamProvider {
		t.Fatalf("provider route: %v", res)
	}
	res = quickModelTest("mcline", nil)
	if res["ok"] != true || res["upstream"] != upstreamCline {
		t.Fatalf("pool route: %v", res)
	}
	res = quickModelTest("zen-free-x", nil)
	if res["ok"] != true || res["upstream"] != upstreamOpenCode {
		t.Fatalf("zen route: %v", res)
	}
	if _, ok := res["durationMs"]; !ok {
		t.Fatalf("durationMs missing: %v", res)
	}
	// 空模型（ Defensive ）
	res = quickModelTest("", nil)
	if res["ok"] != false {
		t.Fatalf("empty model: %v", res)
	}
}

func TestQuickModelTestPinnedSemantics(t *testing.T) {
	installPool(t, []Model{
		{ID: "mprov", Cost: "pass", Status: "active"},
		{ID: "zen-free-x", Source: "zen", Cost: "free", Status: "active"},
	})
	installConfig(t, defaultProxyConfig())
	installProviders(t, &CustomProvider{ID: "pp", Name: "PP", BaseURL: "http://pp.test/v1",
		ModelIDs: []string{"mprov"}, Enabled: true})
	installZenClient(t, func(req *http.Request) *http.Response {
		return stubResp(req, http.StatusOK, okCompletion)
	})
	installTransport(t, func(req *http.Request) *http.Response {
		return stubResp(req, http.StatusOK, okCompletion)
	})

	// 固定 provider：直接走该家（不看冷却/预算解析）
	prov := findEnabledProviderByName("PP")
	res := runQuickModelTest("mprov", prov, "provider", 0)
	if res["ok"] != true || res["upstream"] != upstreamProvider {
		t.Fatalf("pinned provider: %v", res)
	}
	// 固定 cline：provider-served 模型也不给 provider
	res = runQuickModelTest("mprov", nil, "cline", quickTestPoolTimeout)
	if res["upstream"] != upstreamCline {
		t.Fatalf("pinned cline went %v not cline", res["upstream"])
	}
	// 固定 zen 但模型不是 zen → 明确失败
	res = runQuickModelTest("mprov", nil, "zen", quickTestPoolTimeout)
	if res["ok"] != false || res["upstream"] != upstreamOpenCode {
		t.Fatalf("pinned zen on non-zen: %v", res)
	}
	// zen 模型固定 zen → ok
	res = runQuickModelTest("zen-free-x", nil, "zen", quickTestPoolTimeout)
	if res["ok"] != true || res["upstream"] != upstreamOpenCode {
		t.Fatalf("pinned zen on zen: %v", res)
	}
}

func TestQuickModelTestProviderFailureFallsToPool(t *testing.T) {
	installPool(t, []Model{{ID: "mprov", Cost: "pass", Status: "active", Source: "remote"}})
	installConfig(t, defaultProxyConfig())
	installProviders(t, &CustomProvider{ID: "pp", Name: "PP", BaseURL: "http://pp.test/v1",
		ModelIDs: []string{"mprov"}, Enabled: true})
	installTransport(t, func(req *http.Request) *http.Response {
		if req.URL.Host == "pp.test" {
			return stubResp(req, http.StatusInternalServerError, `{"error":"boom"}`)
		}
		return stubResp(req, http.StatusOK, okCompletion)
	})
	res := quickModelTest("mprov", nil)
	if res["ok"] != true || res["upstream"] != upstreamCline {
		t.Fatalf("expected provider failed → pool success: %v", res)
	}
}

func TestAdminModelTestHandler(t *testing.T) {
	installPool(t, nil)
	installConfig(t, defaultProxyConfig())
	code, resp := postAdminJSON(t, handleAdminModelTest, `{"model":""}`)
	if code != 400 || resp.Success {
		t.Fatalf("empty model: status=%d success=%v", code, resp.Success)
	}
	installTransport(t, func(req *http.Request) *http.Response {
		return stubResp(req, http.StatusTeapot, `{"error":"nope"}`)
	})
	// 池内账号存在但上游 418 → 200 + ok:false（上游失败 ≠ API 失败）
	res := quickModelTest("nope-404", nil)
	if res["ok"] != false || res["upstream"] != upstreamCline {
		t.Fatalf("unexpected result %v", res)
	}
}

// ============================ EP4 combos/test ==============================

func TestHandleComboTest(t *testing.T) {
	installPool(t, []Model{
		{ID: "mprov", Cost: "pass", Status: "active", Source: "remote"},
		{ID: "mcline", Cost: "pass", Status: "active", Source: "remote"},
	})
	installConfig(t, defaultProxyConfig())
	installProviders(t, &CustomProvider{ID: "pp", Name: "PP", BaseURL: "http://pp.test/v1",
		ModelIDs: []string{"mprov"}, Enabled: true})
	installTransport(t, func(req *http.Request) *http.Response {
		if req.URL.Host == "pp.test" {
			return stubResp(req, http.StatusOK, okCompletion)
		}
		return stubResp(req, http.StatusOK, okCompletion)
	})
	if _, err := upsertCombo(ModelCombo{ID: "ct", Name: "CT", Enabled: true, Entries: []ComboEntry{
		{Model: "mprov", Source: "provider", Provider: "PP"},
		{Model: "mcline", Disabled: true},
		{Model: "mcline", Source: "cline"},
	}}); err != nil {
		t.Fatalf("upsertCombo: %v", err)
	}

	// 全量测试：禁用条目跳过 → 2 条结果
	code, resp := postAdminJSON(t, handleComboTest, `{"id":"ct"}`)
	if code != 200 || !resp.Success {
		t.Fatalf("status=%d success=%v err=%s", code, resp.Success, resp.Error)
	}
	d := dataMap(t, resp)
	results, _ := d["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results = %v, want 2", results)
	}
	for _, it := range results {
		m := it.(map[string]any)
		if _, ok := m["source"]; !ok {
			t.Fatalf("result missing source: %v", m)
		}
		if _, ok := m["durationMs"]; !ok {
			t.Fatalf("result missing durationMs: %v", m)
		}
	}
	first := results[0].(map[string]any)
	if first["model"] != "mprov" || first["upstream"] != upstreamProvider || first["source"] != "provider" {
		t.Fatalf("provider entry result wrong: %v", first)
	}
	if d["allOk"] != true || d["comboId"] != "ct" {
		t.Fatalf("comboId/allOk wrong: %v", d)
	}

	// 单条目测试
	_, resp = postAdminJSON(t, handleComboTest, `{"id":"ct","model":"mcline"}`)
	results, _ = dataMap(t, resp)["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["model"] != "mcline" {
		t.Fatalf("single-entry result wrong: %v", results)
	}

	// 未匹配条目
	code, resp = postAdminJSON(t, handleComboTest, `{"id":"ct","model":"zzz"}`)
	if code != 404 {
		t.Fatalf("missing entry: status=%d", code)
	}
	// 组合不存在
	code, resp = postAdminJSON(t, handleComboTest, `{"id":"nope"}`)
	if code != 404 || resp.Error == "" {
		t.Fatalf("missing combo: status=%d", code)
	}
}

func TestComboTestDoesNotMutateCombo(t *testing.T) {
	installPool(t, []Model{{ID: "mcline", Cost: "pass", Status: "active", Source: "remote"}})
	installConfig(t, defaultProxyConfig())
	installTransport(t, func(req *http.Request) *http.Response {
		return stubResp(req, http.StatusOK, okCompletion)
	})
	if _, err := upsertCombo(ModelCombo{ID: "cm", Name: "CM", Enabled: true,
		Entries: []ComboEntry{{Model: "mcline"}}}); err != nil {
		t.Fatalf("upsertCombo: %v", err)
	}
	_ = quickModelTest("mcline", nil)
	c, ok := findCombo("cm")
	if !ok || len(c.Entries) != 1 || c.Entries[0].Model != "mcline" {
		t.Fatalf("combo mutated by test: %+v", c)
	}
}

// issue #32 回归：探测预算不能是 16。推理模型会把 16 token 整份耗在
// reasoning 上，api.cline.bot 必然回 500「empty response content」，管理页
// 把可用模型误报为不可用（且 16 恰好等于 minUpstreamMaxTokens，不会被夹大）。
func TestQuickModelTestTokenBudget(t *testing.T) {
	installPool(t, []Model{
		{ID: "mprov", Cost: "pass", Status: "active"},
		{ID: "mcline", Cost: "pass", Status: "active", Source: "remote"},
	})
	installConfig(t, defaultProxyConfig())
	installProviders(t, &CustomProvider{ID: "pp", Name: "PP", BaseURL: "http://pp.test/v1",
		ModelIDs: []string{"mprov"}, Enabled: true})
	readMT := func(req *http.Request) float64 {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Errorf("decode upstream body: %v", err)
		}
		mt, _ := body["max_tokens"].(float64)
		return mt
	}
	var gotMT float64
	installTransport(t, func(req *http.Request) *http.Response {
		gotMT = readMT(req)
		return stubResp(req, http.StatusOK, okCompletion)
	})
	if res := quickModelTest("mcline", nil); res["ok"] != true {
		t.Fatalf("pool route: %v", res)
	}
	if gotMT < 1024 {
		t.Fatalf("cline pool probe max_tokens = %v, want >= 1024", gotMT)
	}
	if res := quickModelTest("mprov", nil); res["ok"] != true {
		t.Fatalf("provider route: %v", res)
	}
	if gotMT < 1024 {
		t.Fatalf("provider probe max_tokens = %v, want >= 1024", gotMT)
	}
}
