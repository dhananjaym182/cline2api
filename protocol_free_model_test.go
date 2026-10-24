package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var protocolModelSyncOnce sync.Once

func protocolTestServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve protocol test port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	protocolModelSyncOnce.Do(func() {
		modelSyncMu.Lock()
		modelSyncRan = true
		modelSyncMu.Unlock()
	})

	zenConfigMu.Lock()
	oldZenConfig := zenConfig
	zenConfig = &zenConfigData{BaseURL: zenAPIBase, Key: "public"}
	zenConfigMu.Unlock()

	oldServerMux := serverMux
	serverMu.Lock()
	oldServer := currentServer
	serverMu.Unlock()
	t.Cleanup(func() {
		serverMu.Lock()
		server := currentServer
		currentServer = oldServer
		serverMu.Unlock()
		if server != nil && server != oldServer {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = server.Shutdown(ctx)
			cancel()
		}
		serverMux = oldServerMux
		zenConfigMu.Lock()
		zenConfig = oldZenConfig
		zenConfigMu.Unlock()
	})

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- startProxy("127.0.0.1", port)
	}()

	baseURL := "http://127.0.0.1:" + strconv.Itoa(port)
	client := &http.Client{Transport: &http.Transport{}, Timeout: 2 * time.Second}
	// 整套测试运行时进程压力大，服务器可能启动超过 2s：deadline 给足余量，
	// 避免把服务器启动慢误判为路由失败（flaky）。
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, requestErr := client.Get(baseURL + "/health")
		if requestErr == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case startErr := <-serverErr:
			t.Fatalf("start protocol test server: %v", startErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for protocol test server")
		}
		time.Sleep(5 * time.Millisecond)
	}

	return baseURL
}

func TestOpenAIChatCompletionsFreeFallsBackToDS(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	first := &Account{
		AccountID:   "chat-glm",
		Email:       "chat-glm@example.com",
		AccessToken: "chat-glm-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	second := &Account{
		AccountID:   "chat-ds",
		Email:       "chat-ds@example.com",
		AccessToken: "chat-ds-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{first, second}}
	config := defaultProxyConfig()
	config.Strategy = "fill"
	setProxyConfig(config)

	var attempts []string
	var models []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		attempts = append(attempts, token)
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var params map[string]any
		if err := json.Unmarshal(body, &params); err != nil {
			return nil, err
		}
		model, _ := params["model"].(string)
		models = append(models, model)
		if model == freeModelPrimary {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{"error":"quota","message":"Try again in 1h"}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"chat-ok","model":"deepseek/deepseek-v4-flash","choices":[{"message":{"role":"assistant","content":"fallback"},"finish_reason":"stop"}]}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	baseURL := protocolTestServer(t)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/chat/completions", strings.NewReader(`{"model":"free","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	responseClient := &http.Client{Transport: &http.Transport{}, Timeout: 2 * time.Second}
	resp, err := responseClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d, want %d: %s", resp.StatusCode, http.StatusOK, responseBody)
	}
	var response map[string]any
	if err := json.Unmarshal(responseBody, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	message, ok := response["choices"].([]any)
	if !ok || len(message) != 1 {
		t.Fatalf("response choices = %v, want one choice", response["choices"])
	}
	choice, _ := message[0].(map[string]any)
	content, _ := choice["message"].(map[string]any)["content"].(string)
	if content != "fallback" {
		t.Fatalf("response content = %q, want fallback", content)
	}
	if got, want := strings.Join(attempts, ","), "chat-glm-token,chat-ds-token,chat-glm-token"; got != want {
		t.Fatalf("attempts = %q, want %q", got, want)
	}
	if got, want := strings.Join(models, ","), freeModelPrimary+","+freeModelPrimary+","+freeModelFallback; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
}

func TestOpenAIResponsesFreeFallsBackToDSAndPreservesResponseFormat(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	oldLogData, oldLogErr := os.ReadFile(requestLogsPath)
	requestLogsMu.Lock()
	oldLogs := requestLogs
	requestLogs = nil
	requestLogsMu.Unlock()
	_ = os.Remove(requestLogsPath)
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
		requestLogsMu.Lock()
		requestLogs = oldLogs
		requestLogsMu.Unlock()
		if oldLogErr != nil {
			_ = os.Remove(requestLogsPath)
		} else {
			_ = os.WriteFile(requestLogsPath, oldLogData, 0600)
		}
	})

	first := &Account{
		AccountID:   "responses-glm",
		Email:       "responses-glm@example.com",
		AccessToken: "responses-glm-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	second := &Account{
		AccountID:   "responses-ds",
		Email:       "responses-ds@example.com",
		AccessToken: "responses-ds-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{first, second}}
	config := defaultProxyConfig()
	config.Strategy = "fill"
	setProxyConfig(config)

	var attempts []string
	var models []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		attempts = append(attempts, token)
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var params map[string]any
		if err := json.Unmarshal(body, &params); err != nil {
			return nil, err
		}
		model, _ := params["model"].(string)
		models = append(models, model)
		if model == freeModelPrimary {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{"error":"quota","message":"Try again in 1h"}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"responses-ok","model":"deepseek/deepseek-v4-flash","choices":[{"message":{"role":"assistant","content":"responses fallback"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	baseURL := protocolTestServer(t)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/responses", strings.NewReader(`{"model":"free","input":"hello"}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	responseClient := &http.Client{Transport: &http.Transport{}, Timeout: 2 * time.Second}
	resp, err := responseClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d, want %d: %s", resp.StatusCode, http.StatusOK, responseBody)
	}
	var response map[string]any
	if err := json.Unmarshal(responseBody, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["object"] != "response" || response["status"] != "completed" {
		t.Fatalf("response envelope = %v, want completed Responses response", response)
	}
	if response["model"] != freeModelFallback {
		t.Fatalf("response model = %v, want %q", response["model"], freeModelFallback)
	}
	if response["output_text"] != "responses fallback" {
		t.Fatalf("output_text = %v, want responses fallback", response["output_text"])
	}
	if _, ok := response["choices"]; ok {
		t.Fatalf("Responses response unexpectedly contains choices: %v", response)
	}
	if got, want := strings.Join(attempts, ","), "responses-glm-token,responses-ds-token,responses-glm-token"; got != want {
		t.Fatalf("attempts = %q, want %q", got, want)
	}
	if got, want := strings.Join(models, ","), freeModelPrimary+","+freeModelPrimary+","+freeModelFallback; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}

	requestLogsMu.Lock()
	defer requestLogsMu.Unlock()
	if len(requestLogs) != 1 {
		t.Fatalf("request log count = %d, want 1", len(requestLogs))
	}
	if requestLogs[0].Model != freeModelFallback {
		t.Fatalf("request log model = %q, want %q", requestLogs[0].Model, freeModelFallback)
	}
}

func TestAnthropicMessagesFreeFallsBackToDSAndPreservesResponseFormat(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	first := &Account{
		AccountID:   "anthropic-glm",
		Email:       "anthropic-glm@example.com",
		AccessToken: "anthropic-glm-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	second := &Account{
		AccountID:   "anthropic-ds",
		Email:       "anthropic-ds@example.com",
		AccessToken: "anthropic-ds-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{first, second}}
	config := defaultProxyConfig()
	config.Strategy = "fill"
	setProxyConfig(config)

	var attempts []string
	var models []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		attempts = append(attempts, token)
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var params map[string]any
		if err := json.Unmarshal(body, &params); err != nil {
			return nil, err
		}
		model, _ := params["model"].(string)
		models = append(models, model)
		if model == freeModelPrimary {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{"error":"quota","message":"Try again in 1h"}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"anthropic-ok","model":"deepseek/deepseek-v4-flash","choices":[{"message":{"role":"assistant","content":"anthropic fallback"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	baseURL := protocolTestServer(t)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/messages", strings.NewReader(`{"model":"free","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	responseClient := &http.Client{Transport: &http.Transport{}, Timeout: 2 * time.Second}
	resp, err := responseClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d, want %d: %s", resp.StatusCode, http.StatusOK, responseBody)
	}
	var response map[string]any
	if err := json.Unmarshal(responseBody, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["type"] != "message" || response["role"] != "assistant" {
		t.Fatalf("response envelope = %v, want Anthropic message", response)
	}
	if response["model"] != freeModelFallback {
		t.Fatalf("response model = %v, want %q", response["model"], freeModelFallback)
	}
	content, ok := response["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("response content = %v, want one block", response["content"])
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "text" || block["text"] != "anthropic fallback" {
		t.Fatalf("response content block = %v", block)
	}
	if response["stop_reason"] != "end_turn" {
		t.Fatalf("stop_reason = %v, want end_turn", response["stop_reason"])
	}
	usage, _ := response["usage"].(map[string]any)
	if usage["input_tokens"] != float64(4) || usage["output_tokens"] != float64(2) {
		t.Fatalf("usage = %v, want input=4 output=2", usage)
	}
	if got, want := strings.Join(attempts, ","), "anthropic-glm-token,anthropic-ds-token,anthropic-glm-token"; got != want {
		t.Fatalf("attempts = %q, want %q", got, want)
	}
	if got, want := strings.Join(models, ","), freeModelPrimary+","+freeModelPrimary+","+freeModelFallback; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
}

func TestOpenAIChatCompletionsFreeStreamFallsBackBeforeResponseHeaders(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	first := &Account{
		AccountID:   "chat-stream-glm",
		Email:       "chat-stream-glm@example.com",
		AccessToken: "chat-stream-glm-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	second := &Account{
		AccountID:   "chat-stream-ds",
		Email:       "chat-stream-ds@example.com",
		AccessToken: "chat-stream-ds-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{first, second}}
	config := defaultProxyConfig()
	config.Strategy = "fill"
	setProxyConfig(config)

	var attempts []string
	var models []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		attempts = append(attempts, token)
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var params map[string]any
		if err := json.Unmarshal(body, &params); err != nil {
			return nil, err
		}
		model, _ := params["model"].(string)
		models = append(models, model)
		if model == freeModelPrimary {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{"error":"quota","message":"Try again in 1h"}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("data: {\"id\":\"stream-ok\",\"model\":\"deepseek/deepseek-v4-flash\",\"choices\":[{\"delta\":{\"content\":\"stream fallback\"}}]}\n\ndata: [DONE]\n\n")),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Request:    req,
		}, nil
	})

	baseURL := protocolTestServer(t)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/chat/completions", strings.NewReader(`{"model":"free","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	responseClient := &http.Client{Transport: &http.Transport{}, Timeout: 2 * time.Second}
	resp, err := responseClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d, want %d: %s", resp.StatusCode, http.StatusOK, responseBody)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(responseBody), "stream fallback") {
		t.Fatalf("response body = %q, want fallback event", responseBody)
	}
	if got, want := strings.Join(attempts, ","), "chat-stream-glm-token,chat-stream-ds-token,chat-stream-glm-token"; got != want {
		t.Fatalf("attempts = %q, want %q", got, want)
	}
	if got, want := strings.Join(models, ","), freeModelPrimary+","+freeModelPrimary+","+freeModelFallback; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
}

func TestOpenAIChatCompletionsFreeStreamDoesNotRetryAfterResponseStarts(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	first := &Account{
		AccountID:   "chat-started-glm",
		Email:       "chat-started-glm@example.com",
		AccessToken: "chat-started-glm-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	second := &Account{
		AccountID:   "chat-started-second",
		Email:       "chat-started-second@example.com",
		AccessToken: "chat-started-second-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{first, second}}
	config := defaultProxyConfig()
	config.Strategy = "fill"
	setProxyConfig(config)

	var attempts []string
	var models []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		attempts = append(attempts, token)
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var params map[string]any
		if err := json.Unmarshal(body, &params); err != nil {
			return nil, err
		}
		model, _ := params["model"].(string)
		models = append(models, model)
		if len(attempts) > 1 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{"error":"unexpected retry"}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("data: {\"id\":\"stream-started\",\"model\":\"z-ai/glm-5.3-flash\",\"choices\":[{\"delta\":{\"content\":\"stream started\"}}]}\n\ndata: [DONE]\n\n")),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Request:    req,
		}, nil
	})

	baseURL := protocolTestServer(t)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/chat/completions", strings.NewReader(`{"model":"free","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	responseClient := &http.Client{Transport: &http.Transport{}, Timeout: 2 * time.Second}
	resp, err := responseClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d, want %d: %s", resp.StatusCode, http.StatusOK, responseBody)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(responseBody), "stream started") {
		t.Fatalf("response body = %q, want first stream event", responseBody)
	}
	if got, want := strings.Join(attempts, ","), first.AccessToken; got != want {
		t.Fatalf("attempts = %q, want %q", got, want)
	}
	if got, want := strings.Join(models, ","), freeModelPrimary; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
}
