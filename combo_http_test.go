package main

// Phase 1 —— 模型组合的端到端（HTTP）与流式回归测试。
// 覆盖：/v1/chat/completions 组合首个条目成功 / 可重试回退 / 停用组合拒绝；
// /v1/models 暴露启用组合；/v1/responses 组合；Anthropic /v1/messages 流式的
// 预提交回退（首个条目在产出内容前失败 → 透明切换下一个条目）与「已有内容后
// 绝不重开」。

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const comboOkCompletion = `{"id":"ok","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`

// comboHTTPTransport 安装测试传输层并统计每次请求命中的模型。
func comboHTTPTransport(t *testing.T, fn func(req *http.Request, model string) *http.Response) *[]string {
	t.Helper()
	seen := &[]string{}
	old := httpClient.Transport
	t.Cleanup(func() { httpClient.Transport = old })
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		m := readReqModel(req)
		*seen = append(*seen, m)
		return fn(req, m), nil
	})
	return seen
}

func comboPost(t *testing.T, url, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// 组合在 /v1/chat/completions 上：首个条目成功，不试第二个条目。
func TestComboChatEndToEndFirstSucceeds(t *testing.T) {
	installPool(t, nil)
	cfg := defaultProxyConfig()
	cfg.Combos = []ModelCombo{{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "combo-a"}, {Model: "combo-b"}}}}
	installConfig(t, cfg)
	seen := comboHTTPTransport(t, func(req *http.Request, model string) *http.Response {
		return stubResp(req, http.StatusOK, comboOkCompletion)
	})

	baseURL := protocolTestServer(t)
	status, body := comboPost(t, baseURL+"/v1/chat/completions",
		`{"model":"c","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	if len(*seen) != 1 || (*seen)[0] != "combo-a" {
		t.Fatalf("upstream models = %v, want only combo-a", *seen)
	}
}

// 组合：首个条目上游 5xx（可重试）→ 第二个条目成功，客户端看到 200。
func TestComboChatEndToEndFallback(t *testing.T) {
	installPool(t, nil)
	cfg := defaultProxyConfig()
	cfg.Combos = []ModelCombo{{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "combo-a"}, {Model: "combo-b"}}}}
	installConfig(t, cfg)
	comboHTTPTransport(t, func(req *http.Request, model string) *http.Response {
		if model == "combo-a" {
			return stubResp(req, http.StatusInternalServerError, `{"error":"boom"}`)
		}
		return stubResp(req, http.StatusOK, comboOkCompletion)
	})

	baseURL := protocolTestServer(t)
	status, body := comboPost(t, baseURL+"/v1/chat/completions",
		`{"model":"c","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	if !strings.Contains(body, "ok") {
		t.Fatalf("body missing served content: %s", body)
	}
}

// 停用的组合：不进 /v1/models，且直接请求得到明确的客户端错误。
func TestComboChatDisabledRejected(t *testing.T) {
	installPool(t, nil)
	cfg := defaultProxyConfig()
	cfg.Combos = []ModelCombo{{ID: "c-off", Name: "Off", Enabled: false,
		Entries: []ComboEntry{{Model: "combo-a"}}}}
	installConfig(t, cfg)

	baseURL := protocolTestServer(t)
	status, body := comboPost(t, baseURL+"/v1/chat/completions",
		`{"model":"c-off","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", status, body)
	}
	if !strings.Contains(body, "disabled") {
		t.Fatalf("error should mention disabled: %s", body)
	}
}

// /v1/models 暴露启用组合、隐藏停用组合。
func TestComboModelsExposedViaHTTP(t *testing.T) {
	installPool(t, nil)
	cfg := defaultProxyConfig()
	cfg.Combos = []ModelCombo{
		{ID: "c-on", Name: "On", Enabled: true, Entries: []ComboEntry{{Model: "combo-a"}}},
		{ID: "c-off", Name: "Off", Enabled: false, Entries: []ComboEntry{{Model: "combo-a"}}},
	}
	installConfig(t, cfg)

	baseURL := protocolTestServer(t)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL + "/v1/models")
	if err != nil {
		t.Fatalf("get models: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	var haveOn, haveOff bool
	for _, m := range out.Data {
		if m.ID == "c-on" && m.OwnedBy == "combo" {
			haveOn = true
		}
		if m.ID == "c-off" {
			haveOff = true
		}
	}
	if !haveOn {
		t.Fatal("enabled combo c-on not exposed via /v1/models (or owned_by != combo)")
	}
	if haveOff {
		t.Fatal("disabled combo c-off must not be exposed via /v1/models")
	}
}

// /v1/responses 组合：非流式，首个条目成功。
func TestComboResponsesEndToEnd(t *testing.T) {
	installPool(t, nil)
	cfg := defaultProxyConfig()
	cfg.Combos = []ModelCombo{{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "combo-a"}, {Model: "combo-b"}}}}
	installConfig(t, cfg)
	seen := comboHTTPTransport(t, func(req *http.Request, model string) *http.Response {
		return stubResp(req, http.StatusOK, comboOkCompletion)
	})

	baseURL := protocolTestServer(t)
	status, body := comboPost(t, baseURL+"/v1/responses", `{"model":"c","input":"hi"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	if len(*seen) != 1 || (*seen)[0] != "combo-a" {
		t.Fatalf("upstream models = %v, want only combo-a", *seen)
	}
}

// Anthropic 流式组合：首个条目在产出任何内容前带内失败 → 透明切换到下一个条目，
// 客户端在一次 HTTP 响应内拿到成功的完整流，只有一次 message_start，无 error 事件。
func TestComboAnthropicStreamFallbackBeforeContent(t *testing.T) {
	installPool(t, nil)
	cfg := defaultProxyConfig()
	cfg.Combos = []ModelCombo{{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "combo-a"}, {Model: "combo-b"}}}}
	installConfig(t, cfg)

	calls := 0
	old := httpClient.Transport
	t.Cleanup(func() { httpClient.Transport = old })
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		m := readReqModel(req)
		if m == "combo-a" {
			// 建流后、首个 token 前带内 504
			return stubSSE(req, "data: {\"error\":{\"code\":504,\"message\":\"Upstream idle timeout exceeded\"}}\n\n"), nil
		}
		return stubSSE(req, "data: {\"id\":\"ok2\",\"model\":\"combo-b\",\"choices\":[{\"delta\":{\"content\":\"recovered\"}}]}\n\ndata: [DONE]\n\n"), nil
	})

	baseURL := protocolTestServer(t)
	status, body := comboPost(t, baseURL+"/v1/messages",
		`{"model":"c","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	if calls < 2 {
		t.Fatalf("upstream calls = %d, want >=2 (fallback to combo-b)", calls)
	}
	if !strings.Contains(body, "recovered") {
		t.Fatalf("client missing content from recovered entry: %s", body)
	}
	if strings.Count(body, "event: message_start") != 1 {
		t.Fatalf("want exactly one message_start event, body=%s", body)
	}
	if strings.Contains(body, "event: error") {
		t.Fatalf("transparent fallback must not surface an error event: %s", body)
	}
}

// Anthropic 流式组合：首个条目已交付内容 → 绝不重开到第二个条目。
func TestComboAnthropicStreamNoRestartAfterContent(t *testing.T) {
	installPool(t, nil)
	cfg := defaultProxyConfig()
	cfg.Combos = []ModelCombo{{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "combo-a"}, {Model: "combo-b"}}}}
	installConfig(t, cfg)

	var seen []string
	old := httpClient.Transport
	t.Cleanup(func() { httpClient.Transport = old })
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		seen = append(seen, readReqModel(req))
		return stubSSE(req, "data: {\"id\":\"ok1\",\"model\":\"combo-a\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"), nil
	})

	baseURL := protocolTestServer(t)
	status, body := comboPost(t, baseURL+"/v1/messages",
		`{"model":"c","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	if len(seen) != 1 || seen[0] != "combo-a" {
		t.Fatalf("upstream models = %v, want only combo-a (no restart after content)", seen)
	}
	if !strings.Contains(body, "hello") {
		t.Fatalf("client missing content: %s", body)
	}
}

// 组合路由成功后，工具调用与 finish_reason 必须原样保留（组合只改变上游选择，
// 复用既有响应处理链路）。
func TestComboChatPreservesToolCalls(t *testing.T) {
	installPool(t, nil)
	cfg := defaultProxyConfig()
	cfg.Combos = []ModelCombo{{ID: "c", Name: "C", Enabled: true,
		Entries: []ComboEntry{{Model: "combo-a"}, {Model: "combo-b"}}}}
	installConfig(t, cfg)
	comboHTTPTransport(t, func(req *http.Request, model string) *http.Response {
		return stubResp(req, http.StatusOK,
			`{"id":"tc","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"SF\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	})

	baseURL := protocolTestServer(t)
	status, body := comboPost(t, baseURL+"/v1/chat/completions",
		`{"model":"c","max_tokens":16,"messages":[{"role":"user","content":"weather?"}]}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	if !strings.Contains(body, "get_weather") || !strings.Contains(body, "tool_calls") {
		t.Fatalf("tool call not preserved through combo routing: %s", body)
	}
}
