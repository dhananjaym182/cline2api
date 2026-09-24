package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// newStreamTestServer 是流式用例的公共前置：单账号池 + 指定降级链 + 测试传输层。
// 返回可用的 baseURL，并在清理时恢复池 / 配置 / 传输层。
func newStreamTestServer(t *testing.T, id string, chain ...string) string {
	t.Helper()
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	account := &Account{
		AccountID:   id,
		Email:       id + "@example.com",
		AccessToken: id + "-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{account}}
	config := defaultProxyConfig()
	config.Strategy = "fill"
	if len(chain) > 0 {
		config.ModelChain = chain
	}
	setProxyConfig(config)
	return protocolTestServer(t)
}

// 复现日志中的真实断裂场景（proxy.log 2026/09/19 11:06:31）：上游 200 建流后，
// 在产出任何内容前发送带内 504 错误（"Upstream idle timeout exceeded"）。
// 预提交设计下这次失败应被透明重试：客户端在一次 HTTP 响应内拿到完整成功流，
// 看不到 error 事件，也看不到重复的 message_start。
func TestAnthropicStreamRetriesTransparentlyBeforeFirstToken(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	account := &Account{
		AccountID:   "stream-retry",
		Email:       "stream-retry@example.com",
		AccessToken: "stream-retry-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{account}}
	config := defaultProxyConfig()
	config.Strategy = "fill"
	setProxyConfig(config)

	var upstreamCalls int
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		upstreamCalls++
		if upstreamCalls == 1 {
			// 排队阶段被网关掐断的真实形态：200 + SSE 带内 504 错误
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: {\"error\":{\"code\":504,\"message\":\"Upstream idle timeout exceeded\",\"metadata\":{\"error_type\":\"timeout\"}}}\n\n")),
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("data: {\"id\":\"retry-ok\",\"model\":\"" + freeModelPrimary + "\",\"choices\":[{\"delta\":{\"content\":\"recovered\"}}]}\n\ndata: [DONE]\n\n")),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Request:    req,
		}, nil
	})

	baseURL := protocolTestServer(t)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/messages", strings.NewReader(`{"model":"free","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d, want 200: %s", resp.StatusCode, body)
	}
	if upstreamCalls != 2 {
		t.Fatalf("upstream calls = %d, want 2 (1 failed pre-commit + 1 successful retry)", upstreamCalls)
	}
	if got := strings.Count(string(body), "event: message_start"); got != 1 {
		t.Fatalf("message_start count = %d, want 1: %s", got, body)
	}
	if strings.Contains(string(body), "event: error") {
		t.Fatalf("client saw error event, want transparent retry: %s", body)
	}
	if !strings.Contains(string(body), "recovered") {
		t.Fatalf("client missing text delta: %s", body)
	}
	if !strings.Contains(string(body), "event: message_stop") {
		t.Fatalf("stream incomplete, missing message_stop: %s", body)
	}
}

// 已提交后上游带内错误：客户端必须收到 error 事件，且不能像旧行为那样
// 在 error 之后又收到伪造的 end_turn message_delta / message_stop（这会让客户端
// 把断流当成一条「成功的空消息」）。提交后也无法重试，上游只应被调用一次。
func TestAnthropicStreamMidStreamErrorNotMaskedAsEndTurn(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	account := &Account{
		AccountID:   "stream-mid-err",
		Email:       "stream-mid-err@example.com",
		AccessToken: "stream-mid-err-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{account}}
	config := defaultProxyConfig()
	config.Strategy = "fill"
	setProxyConfig(config)

	var upstreamCalls int
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		upstreamCalls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				"data: {\"id\":\"mid-err\",\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n" +
					"data: {\"error\":{\"code\":504,\"message\":\"Upstream idle timeout exceeded\"}}\n\n")),
			Header:  http.Header{"Content-Type": []string{"text/event-stream"}},
			Request: req,
		}, nil
	})

	baseURL := protocolTestServer(t)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/messages", strings.NewReader(`{"model":"free","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d, want 200: %s", resp.StatusCode, body)
	}
	if upstreamCalls != 1 {
		t.Fatalf("upstream calls = %d, want 1 (no retry after commit)", upstreamCalls)
	}
	if !strings.Contains(string(body), "partial") {
		t.Fatalf("client missing content emitted before error: %s", body)
	}
	if !strings.Contains(string(body), "event: error") {
		t.Fatalf("client missing error event: %s", body)
	}
	if strings.Contains(string(body), "message_delta") || strings.Contains(string(body), "event: message_stop") {
		t.Fatalf("fake end_turn tail emitted after error: %s", body)
	}
}

// 关键回归：上游产出首个 token 之前，客户端就必须持续收到真实事件
// （message_start 信封 + 周期性 ping 事件），而不是只有注释行。
// 背景：大上下文（~119k tokens / 93 tools）下上游排队与预填充实测 TTFT 可达 3~4 分钟；
// 期间若只有注释行 —— 中间层看不到字节会 524，Claude Code 的流空闲看门狗也会把连接
// 当成卡死（表现为「Combobulating…」很久后 timeout，并退化为必定失败的非流式重试）。
func TestAnthropicStreamStartsEnvelopeBeforeFirstToken(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	account := &Account{
		AccountID:   "stream-keepalive",
		Email:       "stream-keepalive@example.com",
		AccessToken: "stream-keepalive-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{account}}
	config := defaultProxyConfig()
	config.Strategy = "fill"
	setProxyConfig(config)

	// 上游在首 token 之前保持静默：这段窗口里客户端只应看到 `: ping` 注释行。
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		pr, pw := io.Pipe()
		go func() {
			time.Sleep(200 * time.Millisecond)
			pw.Write([]byte("data: {\"id\":\"slow-first-token\",\"choices\":[{\"delta\":{\"content\":\"late\"}}]}\n\n"))
			pw.Write([]byte("data: [DONE]\n\n"))
			pw.Close()
		}()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       pr,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Request:    req,
		}, nil
	})

	baseURL := protocolTestServer(t)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/messages", strings.NewReader(`{"model":"free","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d, want 200", resp.StatusCode)
	}

	// 逐行读取：首 token 到达前，必须已经收到完整信封 + 至少一个 `: ping` 注释行。
	// 注释行只用于喂中间层的字节级读超时（Cloudflare 120s），故意不发事件级 ping，
	// 以便客户端自己的空闲看门狗仍能结束注定失败的回合（用户偏好断开重连而非长时间静默）。
	reader := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(3 * time.Second)
	var early strings.Builder
	sawEnvelope := false
	sawPingComment := false
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read stream before first token: %v (early=%q)", err, early.String())
		}
		if strings.HasPrefix(line, "event: content_block") {
			break // 上游内容开始：信封与保活必须已经在此之前送达
		}
		early.WriteString(line)
		if strings.HasPrefix(line, "event: message_start") {
			sawEnvelope = true
			continue
		}
		if strings.HasPrefix(line, ": ping") {
			sawPingComment = true
		}
	}
	if !sawEnvelope {
		t.Fatalf("message_start envelope not sent before first token (got %q)", early.String())
	}
	if !sawPingComment {
		t.Fatalf("no `: ping` keep-alive comment sent before first token (got %q)", early.String())
	}
	if strings.Contains(early.String(), "event: ping") {
		t.Fatalf("ping must be a comment, not an event, before the first token (got %q)", early.String())
	}

	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read rest of stream: %v", err)
	}
	body := early.String() + string(rest)
	if got := strings.Count(body, "event: message_start"); got != 1 {
		t.Fatalf("message_start count = %d, want exactly 1: %s", got, body)
	}
	if !strings.Contains(body, "late") {
		t.Fatalf("stream missing content emitted after the keep-alive window: %s", body)
	}
	if !strings.Contains(body, "event: message_stop") {
		t.Fatalf("stream incomplete, missing message_stop: %s", body)
	}
}

// 回归：点名模型在上游产出任何内容之前失败（典型为排队阶段的
// {"code":504,"message":"Upstream idle timeout exceeded"}）时，代理应把该模型短时冷却，
// 并在同一次请求内自动换到配置链上的下一个模型，而不是把错误抛给客户端。
// 这是「glm-5.3-flash 老失败」的直接缓解：坏模型自动让位，用户仍能拿到回答。
func TestAnthropicStreamFailsOverToNextModelBeforeFirstToken(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	account := &Account{
		AccountID:   "stream-failover",
		Email:       "stream-failover@example.com",
		AccessToken: "stream-failover-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{account}}
	config := defaultProxyConfig()
	config.Strategy = "fill"
	config.ModelChain = []string{"backup-model"}
	setProxyConfig(config)

	var attempted []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		attempted = append(attempted, body.Model)
		if body.Model == "primary-model" {
			// 排队阶段被网关掰断的真实形态：200 + SSE 带内 504
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: {\"error\":{\"code\":504,\"message\":\"Upstream idle timeout exceeded\"}}\n\n")),
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("data: {\"id\":\"backup-ok\",\"choices\":[{\"delta\":{\"content\":\"from-backup\"}}]}\n\ndata: [DONE]\n\n")),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Request:    req,
		}, nil
	})

	baseURL := protocolTestServer(t)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/messages", strings.NewReader(`{"model":"primary-model","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d, want 200: %s", resp.StatusCode, body)
	}
	if len(attempted) != 2 {
		t.Fatalf("upstream calls = %d (%v), want 2 (1 failed model + 1 failover)", len(attempted), attempted)
	}
	if attempted[0] != "primary-model" {
		t.Fatalf("first attempt model = %q, want primary-model", attempted[0])
	}
	if attempted[1] != "backup-model" {
		t.Fatalf("failover attempt model = %q, want backup-model", attempted[1])
	}
	if !strings.Contains(string(body), "from-backup") {
		t.Fatalf("client missing failover content: %s", body)
	}
	if strings.Contains(string(body), "event: error") {
		t.Fatalf("client saw error event, want transparent failover: %s", body)
	}
	if got := strings.Count(string(body), "event: message_start"); got != 1 {
		t.Fatalf("message_start count = %d, want exactly 1: %s", got, body)
	}
	if !modelCooldownActive(account, "primary-model") {
		t.Fatal("failed model must be cooling after a pre-content failure")
	}
}

// 回归：上游正常收尾但一个内容事件都没发出（glm-5.3-flash 实测会以 4~5 分钟的空
// end_turn 结束）时，代理应视为该模型失败并换链上下一个模型，而不是把一个空回答
// 交给客户端 —— 空回答对 agent 客户端毫无价值，且用户会以为模型“没反应”。
func TestAnthropicStreamEmptyCompletionFailsOver(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	account := &Account{
		AccountID:   "stream-empty",
		Email:       "stream-empty@example.com",
		AccessToken: "stream-empty-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{account}}
	config := defaultProxyConfig()
	config.Strategy = "fill"
	config.ModelChain = []string{"backup-model"}
	setProxyConfig(config)

	var attempted []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		attempted = append(attempted, body.Model)
		if body.Model == "primary-model" {
			// 正常收尾但没有任何内容：典型的“空 end_turn”
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: {\"id\":\"empty\",\"choices\":[{\"delta\":{},\"finish_reason\":\"end_turn\"}]}\n\ndata: [DONE]\n\n")),
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("data: {\"id\":\"backup-ok\",\"choices\":[{\"delta\":{\"content\":\"from-backup\"}}]}\n\ndata: [DONE]\n\n")),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Request:    req,
		}, nil
	})

	baseURL := protocolTestServer(t)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/messages", strings.NewReader(`{"model":"primary-model","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if len(attempted) != 2 {
		t.Fatalf("upstream calls = %d (%v), want 2 (empty model + failover)", len(attempted), attempted)
	}
	if attempted[1] != "backup-model" {
		t.Fatalf("failover attempt model = %q, want backup-model", attempted[1])
	}
	if !strings.Contains(string(body), "from-backup") {
		t.Fatalf("client missing failover content: %s", body)
	}
	if got := strings.Count(string(body), "event: message_stop"); got != 1 {
		t.Fatalf("message_stop count = %d, want exactly 1 (no empty tail from failed model): %s", got, body)
	}
	if !modelCooldownActive(account, "primary-model") {
		t.Fatal("model that returned an empty completion must be cooling")
	}
}

// postAnthropicStream 向测试服务器发一个流式 /v1/messages 请求，读完整响应体，
// 返回状态码、响应体与耗时（耗时用来断言「没有干等到上游超时」）。
func postAnthropicStream(t *testing.T, baseURL, model string) (int, string, time.Duration) {
	t.Helper()
	payload := `{"model":"` + model + `","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}]}`
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/messages", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, string(raw), time.Since(start)
}

// 关键回归（对应 proxy.log 2026/09/22 12:37:49 → 12:47:44 的真实卡死）：
// 上游 200 建流后只送了一条空 delta（仅 role），随后静默数分钟。
// 旧逻辑以「首条 data 行」作为可重试边界，于是这 10 分钟只能干等，直到上游 EOF 才把
// error 甩给客户端（客户端再退化成必定失败的非流式重试）—— 用户看到的就是「thinking 很久然后超时」。
// 新逻辑按「有无内容事件」判定：静默超过 firstContentTimeout 即冷却该模型并换链上下一个。
func TestAnthropicStreamStallsWithoutContentFailsOver(t *testing.T) {
	t.Setenv("CLINE2API_FIRST_CONTENT_TIMEOUT_MS", "150")
	baseURL := newStreamTestServer(t, "stream-stall", "backup-model")

	var attempted []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		attempted = append(attempted, body.Model)
		if body.Model == "primary-model" {
			pr, pw := io.Pipe()
			go func() {
				// 只有 role 的空 delta：真实上游「建流成功但还没有产出」的形态。
				pw.Write([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"))
				time.Sleep(5 * time.Second) // 远超 firstContentTimeout：旧逻辑会一直干等
				pw.Close()
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       pr,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("data: {\"id\":\"backup-ok\",\"choices\":[{\"delta\":{\"content\":\"from-backup\"}}]}\n\ndata: [DONE]\n\n")),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Request:    req,
		}, nil
	})

	status, body, elapsed := postAnthropicStream(t, baseURL, "primary-model")
	if status != http.StatusOK {
		t.Fatalf("response status = %d, want 200: %s", status, body)
	}
	if len(attempted) != 2 {
		t.Fatalf("upstream calls = %d (%v), want 2 (stalled model + failover)", len(attempted), attempted)
	}
	if attempted[0] != "primary-model" || attempted[1] != "backup-model" {
		t.Fatalf("attempted models = %v, want [primary-model backup-model]", attempted)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("failover took %s, want <3s: the stall must be cut short instead of waited out", elapsed)
	}
	if !strings.Contains(body, "from-backup") {
		t.Fatalf("client missing failover content: %s", body)
	}
	if strings.Contains(body, "event: error") {
		t.Fatalf("client saw error event, want transparent failover: %s", body)
	}
	if got := strings.Count(body, "event: message_start"); got != 1 {
		t.Fatalf("message_start count = %d, want exactly 1: %s", got, body)
	}
	if !strings.Contains(body, "event: message_stop") {
		t.Fatalf("stream incomplete, missing message_stop: %s", body)
	}
}

// 回归：上游建流后还没发出任何内容就断开（日志里的 "unexpected EOF"）时，也必须换模型，
// 而不是把 error 事件交给客户端 —— 那会让 Claude Code 退化成注定超时的非流式重试。
func TestAnthropicStreamBreakBeforeContentFailsOver(t *testing.T) {
	baseURL := newStreamTestServer(t, "stream-early-break", "backup-model")

	var attempted []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		attempted = append(attempted, body.Model)
		if body.Model == "primary-model" {
			pr, pw := io.Pipe()
			go func() {
				pw.Write([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"))
				time.Sleep(50 * time.Millisecond)
				pw.CloseWithError(io.ErrUnexpectedEOF)
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       pr,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("data: {\"id\":\"backup-ok\",\"choices\":[{\"delta\":{\"content\":\"from-backup\"}}]}\n\ndata: [DONE]\n\n")),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Request:    req,
		}, nil
	})

	status, body, _ := postAnthropicStream(t, baseURL, "primary-model")
	if status != http.StatusOK {
		t.Fatalf("response status = %d, want 200: %s", status, body)
	}
	if len(attempted) != 2 {
		t.Fatalf("upstream calls = %d (%v), want 2 (broken model + failover)", len(attempted), attempted)
	}
	if attempted[1] != "backup-model" {
		t.Fatalf("failover attempt model = %q, want backup-model", attempted[1])
	}
	if !strings.Contains(body, "from-backup") {
		t.Fatalf("client missing failover content: %s", body)
	}
	if strings.Contains(body, "event: error") {
		t.Fatalf("client saw error event, want transparent failover: %s", body)
	}
	if got := strings.Count(body, "event: message_start"); got != 1 {
		t.Fatalf("message_start count = %d, want exactly 1: %s", got, body)
	}
}

// 回归：客户端的 128k 输出预留会把「输入 + 输出」顶出上游上下文窗口，实测导致
// "maximum context length is 262144 tokens, however you requested about 263503 tokens
// (... 128000 in the output)" 这类 400，必须夹到安全上限。
func TestBuildUpstreamBodyMaxTokensEnvCap(t *testing.T) {
	body := buildUpstreamBody(map[string]any{"max_tokens": float64(128000)}, true)
	if got := body["max_tokens"].(int); got != defaultMaxTokens {
		t.Fatalf("oversized max_tokens = %d, want clamped to %d", got, defaultMaxTokens)
	}

	body = buildUpstreamBody(map[string]any{"max_tokens": float64(1024)}, true)
	if got := body["max_tokens"].(int); got != 1024 {
		t.Fatalf("small max_tokens = %d, want 1024 (unchanged)", got)
	}

	// 未指定时也用上限而不是 128k
	body = buildUpstreamBody(map[string]any{}, true)
	if got := body["max_tokens"].(int); got != defaultMaxTokens {
		t.Fatalf("default max_tokens = %d, want %d", got, defaultMaxTokens)
	}

	t.Setenv("CLINE2API_MAX_TOKENS", "64000")
	body = buildUpstreamBody(map[string]any{"max_tokens": float64(128000)}, true)
	if got := body["max_tokens"].(int); got != 64000 {
		t.Fatalf("CLINE2API_MAX_TOKENS override ignored: max_tokens = %d, want 64000", got)
	}
}
