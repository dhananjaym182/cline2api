package main

import (
	"encoding/json"
	"io"
	"net/http"
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
