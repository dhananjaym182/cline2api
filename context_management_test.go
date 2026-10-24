package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func cmMockUpstream(t *testing.T, jsonBody, sseBody string) func() {
	t.Helper()
	rt := freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(strings.NewReader(string(b)))
		isStream := strings.Contains(string(b), `"stream":true`) || strings.Contains(string(b), `"stream": true`)
		body := jsonBody
		ct := "application/json"
		if isStream {
			body = sseBody
			ct = "text/event-stream"
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{ct}},
			Request:    req,
		}, nil
	})
	oldTransport := httpClient.Transport
	httpClient.Transport = rt
	zenTransportMu.Lock()
	oldZenClient := zenHTTPClient
	zenHTTPClient = &http.Client{Transport: rt}
	zenTransportMu.Unlock()
	return func() {
		httpClient.Transport = oldTransport
		zenTransportMu.Lock()
		zenHTTPClient = oldZenClient
		zenTransportMu.Unlock()
	}
}

func cmInstallPool(t *testing.T) {
	t.Helper()
	oldPool := pool
	oldConfig := getProxyConfig()
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
	})
	account := &Account{AccountID: "cm-test", Email: "cm@test.com", AccessToken: "cm-token", ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), Status: "active"}
	pool = &AccountPool{Accounts: []*Account{account}}
	config := defaultProxyConfig()
	config.Strategy = "fill"
	setProxyConfig(config)
}

func TestContextManagementCountTokens(t *testing.T) {
	baseURL := protocolTestServer(t)
	payload := `{"model":"free","max_tokens":64,"messages":[{"role":"user","content":"hello world"}]}`
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/messages/count_tokens", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["input_tokens"]; !ok {
		t.Fatalf("missing input_tokens: %v", out)
	}
	cm, _ := out["context_management"].(map[string]any)
	if cm == nil || cm["original_input_tokens"] == nil {
		t.Fatalf("missing context_management.original_input_tokens: %v", out)
	}
}

func cmSetZenCompact(t *testing.T) {
	t.Helper()
	zenConfigMu.Lock()
	if zenConfig == nil {
		zenConfig = &zenConfigData{BaseURL: "http://x", Key: "test-key"}
	}
	zenConfig.Key = "test-key"
	zenConfig.Enabled = true
	zenConfig.Compaction = zenCompactConfig{KeepTokens: 1, MaxSummary: 64, Buffer: 1}
	zenConfigMu.Unlock()
	t.Cleanup(func() {
		zenConfigMu.Lock()
		zenConfig = nil
		zenConfigMu.Unlock()
	})
}

func TestContextManagementParse(t *testing.T) {
	raw := []byte(`{"edits":[{"type":"compact_20260112","trigger":{"type":"input_tokens","value":10},"pause_after_compaction":true},{"type":"clear_tool_uses_20250919","trigger":{"type":"input_tokens","value":1},"keep":{"type":"tool_uses","value":1}},{"type":"clear_thinking_20251015"}]}`)
	cfg, err := parseCMConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || len(cfg.Edits) != 3 {
		t.Fatalf("expected 3 edits, got %v", cfg)
	}
	if cfg.Edits[0].Type != "compact_20260112" || !cfg.Edits[0].PauseAfterCompact {
		t.Fatalf("compact edit parsed wrong: %+v", cfg.Edits[0])
	}
	if cfg.Edits[1].Type != "clear_tool_uses_20250919" {
		t.Fatalf("clear_tool_uses edit parsed wrong: %+v", cfg.Edits[1])
	}
	if cfg.Edits[2].Type != "clear_thinking_20251015" {
		t.Fatalf("clear_thinking edit parsed wrong: %+v", cfg.Edits[2])
	}
}

func TestContextManagementNoCM(t *testing.T) {
	baseURL := protocolTestServer(t)
	cmInstallPool(t)
	restore := cmMockUpstream(t, `{"id":"x","object":"chat.completion","model":"free","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	defer restore()

	payload := `{"model":"free","max_tokens":64,"stream":false,"messages":[{"role":"user","content":"hello"}]}`
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/messages", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if out["stop_reason"] != "end_turn" {
		t.Fatalf("unexpected stop_reason: %v", out["stop_reason"])
	}
	if _, ok := out["context_management"]; ok {
		t.Fatalf("context_management should be absent without CM request: %v", out["context_management"])
	}
}

func TestContextManagementCompactionPaused(t *testing.T) {
	baseURL := protocolTestServer(t)
	cmSetZenCompact(t)
	restore := cmMockUpstream(t, `{"id":"x","object":"chat.completion","model":"free","choices":[{"index":0,"message":{"role":"assistant","content":"summary text"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	defer restore()
	payload := `{"model":"free","max_tokens":64,"stream":false,"messages":[{"role":"user","content":"hello world this is a long message to trigger compaction"},{"role":"assistant","content":"ok noted"}],"context_management":{"edits":[{"type":"compact_20260112","trigger":{"type":"input_tokens","value":1},"pause_after_compaction":true}]}}`
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/messages", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if out["stop_reason"] != "compaction" {
		t.Fatalf("expected stop_reason=compaction, got %v", out["stop_reason"])
	}
	content, _ := out["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("expected compaction block in content: %v", out)
	}
	cb, _ := content[0].(map[string]any)
	if cb["type"] != "compaction" {
		t.Fatalf("first content block not compaction: %v", cb)
	}
}

func TestContextManagementCompactionNonPaused(t *testing.T) {
	baseURL := protocolTestServer(t)
	cmInstallPool(t)
	cmSetZenCompact(t)
	restore := cmMockUpstream(t, `{"id":"x","object":"chat.completion","model":"free","choices":[{"index":0,"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	defer restore()

	payload := `{"model":"free","max_tokens":64,"stream":false,"messages":[{"role":"user","content":"hello world this is a long message to trigger compaction"},{"role":"assistant","content":"ok noted"}],"context_management":{"edits":[{"type":"compact_20260112","trigger":{"type":"input_tokens","value":1}}]}}`
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/messages", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	content, _ := out["content"].([]any)
	if len(content) < 2 {
		t.Fatalf("expected compaction block + answer, got %v", out["content"])
	}
	cb, _ := content[0].(map[string]any)
	if cb["type"] != "compaction" {
		t.Fatalf("first block not compaction: %v", cb)
	}
}

func TestContextManagementClearToolUses(t *testing.T) {
	baseURL := protocolTestServer(t)
	cmInstallPool(t)
	restore := cmMockUpstream(t, `{"id":"x","object":"chat.completion","model":"free","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	defer restore()

	payload := `{"model":"free","max_tokens":64,"stream":false,"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"","tool_calls":[{"id":"t1","type":"function","function":{"name":"foo","arguments":"{}"}}]},{"role":"tool","tool_call_id":"t1","content":"big result 1"},{"role":"assistant","content":"","tool_calls":[{"id":"t2","type":"function","function":{"name":"foo","arguments":"{}"}}]},{"role":"tool","tool_call_id":"t2","content":"big result 2"},{"role":"assistant","content":"","tool_calls":[{"id":"t3","type":"function","function":{"name":"foo","arguments":"{}"}}]},{"role":"tool","tool_call_id":"t3","content":"big result 3"},{"role":"assistant","content":"","tool_calls":[{"id":"t4","type":"function","function":{"name":"foo","arguments":"{}"}}]},{"role":"tool","tool_call_id":"t4","content":"big result 4"}],"context_management":{"edits":[{"type":"clear_tool_uses_20250919","trigger":{"type":"input_tokens","value":1},"keep":{"type":"tool_uses","value":1}}]}}`
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/messages", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	cm, _ := out["context_management"].(map[string]any)
	ae, _ := cm["applied_edits"].([]any)
	if len(ae) == 0 {
		t.Fatalf("expected applied_edits, got %v", out["context_management"])
	}
	// verify at least one cleared
	found := false
	for _, e := range ae {
		if m, ok := e.(map[string]any); ok && m["type"] == "clear_tool_uses_20250919" {
			if v, _ := m["cleared_tool_uses"].(float64); v > 0 {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected cleared tool uses, got %v", ae)
	}
}

func TestContextManagementUnsupportedClearThinking(t *testing.T) {
	baseURL := protocolTestServer(t)
	cmInstallPool(t)
	restore := cmMockUpstream(t, `{"id":"x","object":"chat.completion","model":"free","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	defer restore()

	payload := `{"model":"free","max_tokens":64,"stream":false,"messages":[{"role":"user","content":"hi"}],"context_management":{"edits":[{"type":"clear_thinking_20251015"}]}}`
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/messages", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	cm, _ := out["context_management"].(map[string]any)
	ae, _ := cm["applied_edits"].([]any)
	found := false
	for _, e := range ae {
		if m, ok := e.(map[string]any); ok && m["type"] == "clear_thinking_20251015" && m["status"] == "unsupported" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected clear_thinking reported unsupported, got %v", ae)
	}
}

func TestContextManagementTriggerNotMet(t *testing.T) {
	baseURL := protocolTestServer(t)
	cmInstallPool(t)
	cmSetZenCompact(t)
	restore := cmMockUpstream(t, `{"id":"x","object":"chat.completion","model":"free","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	defer restore()

	payload := `{"model":"free","max_tokens":64,"stream":false,"messages":[{"role":"user","content":"hi"}],"context_management":{"edits":[{"type":"compact_20260112","trigger":{"type":"input_tokens","value":999999}}]}}`
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/messages", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	content, _ := out["content"].([]any)
	if len(content) > 0 {
		if cb, ok := content[0].(map[string]any); ok && cb["type"] == "compaction" {
			t.Fatalf("compaction should not have triggered: %v", out)
		}
	}
}

func TestContextManagementStreamCompaction(t *testing.T) {
	baseURL := protocolTestServer(t)
	cmInstallPool(t)
	cmSetZenCompact(t)
	restore := cmMockUpstream(t, `{"id":"x","object":"chat.completion","model":"free","choices":[{"index":0,"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	defer restore()

	payload := `{"model":"free","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello world this is a long message"}],"context_management":{"edits":[{"type":"compact_20260112","trigger":{"type":"input_tokens","value":1}}]}}`
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/messages", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "compaction") {
		t.Fatalf("expected compaction in stream: %s", s)
	}
	if !strings.Contains(s, "message_stop") {
		t.Fatalf("expected message_stop: %s", s)
	}
}
