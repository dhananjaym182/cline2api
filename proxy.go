package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cline-go-proxy/internal/dispatch"
	"cline-go-proxy/internal/httpapi"
	"cline-go-proxy/internal/httpx"
	"cline-go-proxy/internal/protocol"
	anthropicconv "cline-go-proxy/internal/protocol/anthropic"
	"cline-go-proxy/internal/protocol/openaichat"
	clineupstream "cline-go-proxy/internal/upstream/cline"
)

const (
	// defaultMaxTokens 是上游请求的默认/上限输出 token 数。
	//
	// 从 128000 降到 32000：日志实测 poolside/laguna-s-2.1:free 直接 400 ——
	// "maximum context length is 262144 tokens, however you requested about 263503
	// tokens (88799 text input, 46704 tool input, 128000 in the output)"。
	// 也就是说 128k 的输出预留本身就能把请求顶出上下文窗口，让本来可用的模型直接失败，
	// 并让其余模型陷入超长预填充/静默。可用 CLINE2API_MAX_TOKENS 调整。
	defaultMaxTokens       = 32000
	defaultReasoningEffort = "high"
	fallbackDefaultModel   = "z-ai/glm-5.3-flash"
	freeModelPrimary       = "z-ai/glm-5.3-flash"
	freeModelFallback      = "deepseek/deepseek-v4-flash"
	freeModelLastResort    = "cline-free/longcat-2.0"
	// maxUpstreamToolNameLength is the upstream tool-name limit.
	maxUpstreamToolNameLength = openaichat.MaxToolNameLength
)

// minUpstreamMaxTokens 上游对输出 token 的硬下限：Cline 免费模型经 OpenRouter
// 转发时（如 meta/muse-spark），max_output_tokens < 16 会被上游直接 400。
const minUpstreamMaxTokens = 16

// defaultFirstContentTimeout 是单次上游尝试等待「首个内容事件」的上限。
// 超时即认定该模型此刻不可用：放弃本次尝试、冷却该模型、立刻换到链上下一个模型。
//
// 依据 proxy.log 2026/09/22 12:34~13:04 的实测：同一个 glm-5.3-flash 账号在 93 tools
// 的负载下，有时 1~3 秒就出内容，有时建流后静默 5~10 分钟（超过客户端耐心后
// 只能看到「Combobulating…」然后 timeout），而同一时刻链上的 deepseek-v4.1-flash
// 1.5 秒就返回。继续等待没有收益 —— 快速换模型才有。
//
// 20s 而不是 90s：90s 的实测后果是 13:31:40 那次请求在 GLM 上白等 90s、
// 换到 laguna 立刻 400、再换 solar-pro4 又静默，客户端只看到「Fermenting… (2m51s)」。
// 好几个候选都被跳过时，旧行为（客户端自己超时后重连）体验明显好于我们长时间静默等待，
// 所以这里取「一次不行就赶紧换」的短耐心。可用 CLINE2API_FIRST_CONTENT_TIMEOUT_MS 覆盖。
const defaultFirstContentTimeout = 20 * time.Second

// lastAttemptFirstContentTimeout 是最后一次尝试的等待上限，比普通尝试宽容：
// 候选模型都被跳过时，宁可多等一下也要给出答案，而不是把错误抛给客户端。
// 但仍然收敛在客户端忍耐范围内，不留几分钟的静默「假活着」。
// 可用 CLINE2API_LAST_ATTEMPT_TIMEOUT_MS 覆盖。
const lastAttemptFirstContentTimeout = 45 * time.Second

// maxOutputTokens 是客户端请求中 max_tokens 的上限。
// 客户端显式要更大的输出（例如 128k）也会被夹到这里：预留过大的输出空间会把输入+输出
// 顶出上游上下文窗口（实测 400），收益却接近没有。
func maxOutputTokens() int {
	return envInt("CLINE2API_MAX_TOKENS", defaultMaxTokens)
}

// clampMaxTokens 把请求的输出上限夹到 [1, maxOutputTokens()]。
func clampMaxTokens(n int) int {
	limit := maxOutputTokens()
	if n <= 0 || n > limit {
		return limit
	}
	return n
}

// reasoningEffort 返回下发给上游的推理强度。默认保持 "high"，
// 可用 CLINE2API_REASONING_EFFORT=low|medium|high|none 覆盖：推理模型在大上下文下
// 的思考阶段可能是长时间静默的来源之一，这是用户侧最简单的延迟开关。
func reasoningEffort() string {
	if v := strings.TrimSpace(os.Getenv("CLINE2API_REASONING_EFFORT")); v != "" {
		return v
	}
	return defaultReasoningEffort
}

// freeModelChain 是 model="free" 时的降级顺序。
// 顺序依据 Artificial Analysis Intelligence Index v4.1.1：
// glm-5.3-flash 57 > deepseek-v4-flash 0731 52 > longcat-2.0 34。
var freeModelChain = []string{freeModelPrimary, freeModelFallback, freeModelLastResort}

// builtinModels 是内置默认模型列表（不可删除），仅作为离线 / 未同步时的 fallback。
// 同步 Cline 官方推荐模型成功后，getAllModels 以远程模型为主。
var builtinModels = []Model{
	{ID: "z-ai/glm-5.3-flash", Provider: "z-ai", Cost: "free", Status: "active", Custom: false},
	{ID: "cline-free/longcat-2.0", Provider: "cline-free", Cost: "free", Status: "active", Custom: false},
	{ID: "cline-pass/glm-5.2", Provider: "zai", Cost: "pass", Status: "active", Custom: false},
	{ID: "cline-pass/deepseek-v4-flash", Provider: "deepseek", Cost: "pass", Status: "active", Custom: false},
	{ID: "cline-pass/qwen3.7-max", Provider: "qwen", Cost: "pass", Status: "active", Custom: false},
	{ID: "deepseek/deepseek-v4-flash", Provider: "deepseek", Cost: "free", Status: "active", Custom: false},
	{ID: "poolside/laguna-s-2.1:free", Provider: "poolside", Cost: "free", Status: "active", Custom: false},
}

// getAllModels 返回可用模型列表：
//   - 已同步远程模型：Cline 远程（Source=remote）+ opencode 同步（Source=zen）+ 用户自定义
//   - 未同步 / 离线：内置 fallback（Cline + zen 种子表）+ 用户自定义
func getAllModels() []Model {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	var custom []Model
	var remote []Model
	var zen []Model
	for _, m := range p.Models {
		switch m.Source {
		case "remote":
			remote = append(remote, m)
		case "zen":
			zen = append(zen, m)
		default:
			custom = append(custom, m)
		}
	}

	if len(remote) > 0 || len(zen) > 0 || remoteZenActive() {
		result := make([]Model, 0, len(remote)+len(zen)+len(custom))
		result = append(result, remote...)
		result = append(result, zen...)
		result = append(result, custom...)
		return result
	}

	builtin := make([]Model, 0, len(builtinModels)+len(zenSeedModels))
	builtin = append(builtin, builtinModels...)
	builtin = append(builtin, builtinZenModels()...)

	result := make([]Model, 0, len(builtin)+len(custom))
	result = append(result, builtin...)
	result = append(result, custom...)
	return result
}

// buildModelsList 组装客户端发现用的模型列表（/v1/models、/models）：
// 池内模型（已停用的排除，onlyFree 时仅免费）+ 启用中的具名模型组合。
//
// 组合不插入 pool.Models（避免污染上游归属 / 同步 / 删除 / Cline-Zen 分流判定），
// 只在此发现层追加，owned_by="combo" 便于客户端区分。组合可能跨免费/付费源，故
// 不受 onlyFree 过滤影响（组合是管理员显式选择的集合，不是单一计费模型）。
func buildModelsList(onlyFree bool) []map[string]any {
	all := getAllModels()
	list := make([]map[string]any, 0, len(all))
	for _, m := range all {
		// 管理员手动停用的模型不进客户端模型列表
		if m.Disabled {
			continue
		}
		// 通配符排除的模型不进列表（与请求入口同一策略）
		if excludedForListing(m.ID) {
			continue
		}
		if onlyFree && !isFreeModelEntry(m) {
			continue
		}
		ownedBy := "cline"
		if m.Source == "zen" || m.Provider == "opencode" {
			ownedBy = "opencode"
		}
		entry := map[string]any{
			"id":       m.ID,
			"object":   "model",
			"created":  time.Now().UnixMilli(),
			"owned_by": ownedBy,
		}
		if m.Context > 0 {
			entry["context_length"] = m.Context
		}
		if m.Output > 0 {
			entry["max_output_tokens"] = m.Output
		}
		if len(m.ThinkingLevels) > 0 {
			entry["thinking_levels"] = m.ThinkingLevels
		}
		if len(m.InputModalities) > 0 {
			entry["input_modalities"] = m.InputModalities
		}
		if len(m.OutputModalities) > 0 {
			entry["output_modalities"] = m.OutputModalities
		}
		list = append(list, entry)
	}
	for _, c := range comboEnabledList() {
		if excludedForListing(c.ID) {
			continue
		}
		list = append(list, map[string]any{
			"id":       c.ID,
			"object":   "model",
			"created":  time.Now().UnixMilli(),
			"owned_by": "combo",
		})
	}
	list = append(list, aliasListingEntries()...)
	return list
}

// getDefaultModel 返回用户设置的默认模型；未设置时优先回退到第一个远程 free 模型，
// 否则用内置 fallback。
func getDefaultModel() string {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	// 管理员设置的默认模型也会随上游下架而失效：远程同步已启用却查不到时
	// 视为已下架，落到下方「第一个远程免费模型」，避免每个无模型请求必败。
	// 被管理员手动停用的默认模型同样跳过（含离线模式）。
	if p.DefaultModel != "" {
		defaultDisabled := false
		for _, m := range p.Models {
			if m.ID == p.DefaultModel && m.Disabled {
				defaultDisabled = true
				break
			}
		}
		if !defaultDisabled && (!remoteModelsActive() || liveModelSet(p)[p.DefaultModel]) {
			return p.DefaultModel
		}
	}

	for _, m := range p.Models {
		if m.Source == "remote" && m.Cost == "free" && !m.Disabled {
			return m.ID
		}
	}

	// 远程同步已启用但上游没有免费模型（列表调整 / 接口变化）：从仍然在线的
	// 模型里挑一个，绝不在有实时数据时退回下面的硬编码内置表（写死的模型
	// 已下架时会让每个无模型请求必败）。
	if remoteModelsActive() {
		for _, m := range p.Models {
			if m.Status == "active" && !m.Disabled {
				return m.ID
			}
		}
	}

	// 仅离线 / 从未同步成功时才允许内置硬编码 fallback。
	for _, m := range builtinModels {
		if m.Cost == "free" {
			return m.ID
		}
	}
	return fallbackDefaultModel
}

// 当前监听地址（startProxy 启动时赋值，供管理后台展示）。
var (
	listenHost string
	listenPort int
)

// HTTP server 实例与路由表（restartListener 换地址重启时复用）。
var (
	serverMux     *http.ServeMux
	currentServer *http.Server
	serverMu      sync.Mutex
)

// restartListener 用新地址重启 HTTP 监听。
// 注意：必须在 goroutine 中调用——Shutdown 会等待当前 HTTP 请求完成，
// 若在 admin handler 内同步调用会死锁。
func restartListener(host string, port int) error {
	if host == "" {
		host = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	listenHost = host
	listenPort = port

	serverMu.Lock()
	old := currentServer
	server := &http.Server{Addr: addr, Handler: serverMux}
	currentServer = server
	serverMu.Unlock()

	if old != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = old.Shutdown(ctx)
		cancel()
	}

	fmt.Println("")
	fmt.Println(strings.Repeat("=", 58))
	fmt.Printf("  Listener restarted: %s\n", addr)
	if !isLoopbackHost(host) {
		for _, ip := range detectLocalIPs() {
			fmt.Printf("  http://%s:%d (LAN)\n", ip, port)
		}
		fmt.Println("  !!! 监听非本机地址，管理后台无鉴权，请确认网络环境安全")
	}
	fmt.Println(strings.Repeat("=", 58))
	return server.ListenAndServe()
}

// effectiveAdminHost 返回管理后台/浏览器实际可用的访问地址：
// host 为空或通配地址（0.0.0.0 / ::）时展示回环 127.0.0.1，否则返回 host 本身。
func effectiveAdminHost(host string) string {
	switch host {
	case "", "0.0.0.0", "::":
		return "127.0.0.1"
	}
	return host
}

// detectLocalIPs 检测本机所有可用 IPv4 地址（排除回环、链路本地和未启用的网卡）。
func detectLocalIPs() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	result := make([]string, 0, len(addrs))
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			result = append(result, v4.String())
		}
	}
	return result
}

// isLoopbackHost 判断监听地址是否为回环（127.x / localhost），用于安全提示。
func isLoopbackHost(host string) bool {
	switch host {
	case "", "localhost", "127.0.0.1":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

var passThroughKeys = []string{
	"tools", "tool_choice", "parallel_tool_calls", "functions", "function_call",
	"temperature", "top_p", "top_k", "stop", "presence_penalty", "frequency_penalty",
	"response_format", "user", "n", "logit_bias", "seed", "logprobs", "top_logprobs",
	"stream_options", "metadata",
}

type chatRequest struct {
	Model               string          `json:"model"`
	Messages            json.RawMessage `json:"messages"`
	Stream              bool            `json:"stream,omitempty"`
	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Tools               json.RawMessage `json:"tools,omitempty"`
	ToolChoice          json.RawMessage `json:"tool_choice,omitempty"`
	ReasoningEffort     string          `json:"reasoning_effort,omitempty"`
	ReasoningEffortAlt  string          `json:"reasoningEffort,omitempty"`
	Extra               map[string]any  `json:"-"`
}

// isFreeModelEntry 判断模型是否免费（/models 过滤用）：
// Cost 直接标记 free，或 zen 来源且命中免费判定（种子白名单兜底）。
func isFreeModelEntry(m Model) bool {
	if m.Cost == "free" {
		return true
	}
	return isZenSource(m) && isZenFreeModel(m)
}

func startProxy(host string, port int) error {
	log.Printf("Data directory: %s (set %s to override)", currentDataDir(), dataDirEnv)
	p := loadPool()
	loadRequestLogs()
	// 自定义 provider 声明的模型同步进池：模型管理页可见、可启停、参与自动回退
	syncProviderModels()
	activeCount := 0
	for _, a := range p.Accounts {
		if a.Status == "active" {
			// Try to pre-warm tokens
			if a.AccessToken == "" || time.Now().UnixMilli() >= a.ExpiresAt {
				if err := refreshAccountToken(a); err != nil {
					log.Printf("  Pre-warm failed for %s: %v", a.Email, err)
					continue
				}
			}
			activeCount++
		}
	}
	log.Printf("Loaded %d active accounts from pool", activeCount)

	// 启动时异步同步一次 Cline 官方推荐模型（不阻塞启动）
	startModelSync()

	// §W3-C：已启用 provider 的上游模型清单后台自动检测（下架标记 + 新模型入队待审）
	startProviderModelRecheck()

	// opencode zen：定时同步免费模型列表 + 压缩会话状态清理
	if getZenConfig().Enabled {
		startZenModelsRefresher()
	}
	startCompactCleanup()

	freePort(port)

	mux := http.NewServeMux()

	health := httpapi.Health{
		Version:        func() string { return appVersion },
		ActiveAccounts: func() int { return activeCount },
	}
	mux.HandleFunc("/v1/health", corsHandler(health.Handler))
	mux.HandleFunc("/health", corsHandler(health.Handler))

	// Admin API (frontend + REST)
	registerAdminRoutes(mux)

	apiKeyHandler := (&httpapi.APIKeyAuth{
		Keys: func() []string { return loadPool().Keys },
		Unauthorized: func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": map[string]string{
					"message": "invalid API key. Generate one at /admin/ or set x-api-key header",
					"type":    "auth_error",
				},
			})
		},
	}).Wrap

	modelsHandler := apiKeyHandler(func(w http.ResponseWriter, r *http.Request) {
		list := buildModelsList(getProxyConfig().OnlyFree)
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": list})
	})
	mux.HandleFunc("/v1/models", modelsHandler)
	mux.HandleFunc("/models", modelsHandler)

	chatHandler := apiKeyHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "parse_error"},
			})
			return
		}

		var params map[string]any
		if err := json.Unmarshal(body, &params); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "parse_error"},
			})
			return
		}

		isStream, _ := params["stream"].(bool)
		toolCount := 0
		if tools, ok := params["tools"]; ok {
			if t, ok := tools.([]any); ok {
				toolCount = len(t)
			}
		}
		model, _ := params["model"].(string)
		log.Printf("  client: stream=%v tools=%d model=%s", isStream, toolCount, model)

		// Administrator model policy: alias rewrite + wildcard exclusions,
		// enforced before routing and before the disabled check.
		if resolved, reject := resolveClientModel(model); reject != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": reject, "type": "invalid_request_error"},
			})
			return
		} else if resolved != model {
			model = resolved
			params["model"] = resolved
		}
		applyThinkingPolicy(params, model)

		// 管理员手动停用的模型在入口直接拒绝（不进列表，也不可点名调用）
		if model != "" && isModelDisabled(model) {
			msg := "model " + model + " is disabled"
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": msg, "type": "invalid_request_error"},
			})
			return
		}

		reqLog := RequestLog{StartedAt: time.Now(), Protocol: "openai", Model: model, Stream: isStream}

		// Free-only routing rejects models whose route access is not verified
		// free (or free_tier with an active allowance).
		if msg := freeOnlyRejection(model); msg != "" {
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": msg, "type": "invalid_request_error"},
			})
			return
		}

		// Opt-in nonstream keepalive (NonstreamKeepaliveSeconds): a single
		// space every interval while the upstream call blocks, to outlive
		// byte-level read timeouts in intermediaries. The wrapped writer
		// serializes those writes with the eventual body write and stops the
		// keepalive once real bytes are committed, so the two never race.
		if !isStream {
			if kw := newKeepaliveWriter(w); kw != nil {
				w = kw
				defer kw.stop()
			}
		}

		// Override system prompt from override.md for OpenAI format
		if override := loadOverrideContent(); override != "" {
			if msgs, ok := params["messages"].([]any); ok {
				found := false
				for _, m := range msgs {
					if mm, ok := m.(map[string]any); ok {
						if mm["role"] == "system" {
							mm["content"] = override
							found = true
							break
						}
					}
				}
				if !found {
					params["messages"] = append([]any{map[string]any{"role": "system", "content": override}}, msgs...)
				}
			}
		}

		// 模型组合：具名有序回退。命中组合时在普通路由之前分流 —— 组合 ID 不是
		// 真实上游模型，必须在此拦截，按管理员配置的顺序逐个尝试；不会走全局
		// ModelChain / free 链（除非组合显式开启 emergencyFallback）。
		if combo, ok := findCombo(model); ok {
			if !combo.Enabled {
				msg := fmt.Sprintf("model combo %q is disabled", model)
				finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error": map[string]string{"message": msg, "type": "invalid_request_error"},
				})
				return
			}
			reqLog.ToolNames = clampParamsToolNames(params)
			run := runCombo(params, combo, isStream, requestSessionID(params, r.Header))
			applyComboTrace(&reqLog, combo, run)
			stampUpstream(&reqLog, params) // 归因实际服务方（provider / opencode / cline）
			if run.Acc != nil {
				reqLog.AccountID = run.Acc.AccountID
				reqLog.AccountEmail = run.Acc.Email
			}
			if run.Err != nil {
				log.Printf("  combo %q error: %v", combo.ID, run.Err)
				msg := comboTraceSummary(combo, run)
				finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
				writeJSON(w, clineErrorHTTPStatus(run.Err), map[string]any{
					"error": map[string]string{"message": msg, "type": "api_error"},
				})
				return
			}
			defer run.Resp.Body.Close()
			// 非流式：完整按条目顺序回退。流式：仅在连接级回退（runCombo 在写任何
			// 字节给客户端前拿到 200 响应）；空流由统一契约补发 in-band 错误，不再
			// 伪装成正常结束（Anthropic 端点另有 streamAnthropicCombo 的预提交回退）。
			if isStream {
				serveChatStream(r.Context(), w, run.Resp, run.Acc, &reqLog, nil)
			} else {
				handleNonStreamResponse(w, run.Resp, run.Acc, &reqLog)
			}
			return
		}

		// 按 model 自动分流：zen 免费模型 / zen 付费拒绝 / 其余走 Cline 池
		switch routeModel(model) {
		case "reject":
			msg := fmt.Sprintf("model %q is a paid opencode model; only free models are proxied", model)
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": msg, "type": "invalid_request_error"},
			})
			return
		case "zen":
			reqLog.Upstream = upstreamOpenCode
			zm, _ := resolveZenInfo(model)
			out := maybeCompact(params, zm, requestSessionID(params, r.Header))
			if out.changed {
				log.Printf("  chat %s", out.note)
			}
			resp, err := callZenAPI(params, isStream)
			// 上游返回「上下文超限」400：压缩后重试一次，避免 Claude Code 大上下文直接失败
			if err != nil && isContextLengthError(err) {
				log.Printf("  chat: context length exceeded, compacting and retrying")
				out2 := fallbackTruncate(params, zm)
				if out2.changed {
					log.Printf("  chat %s", out2.note)
					resp, err = callZenAPI(params, isStream)
				}
			}
			if err != nil {
				// zen 故障转移到 Cline 池：先截短超长工具名（Meta 后端硬限 ≤64）
				reqLog.ToolNames = clampParamsToolNames(params)
				if fbResp, fbAcc, fbErr, attempted := zenFailoverToCline(params, isStream); attempted {
					if fbErr == nil {
						log.Printf("  chat failover: serving %q via cline pool", model)
						stampUpstream(&reqLog, params) // 归因实际服务方（provider / cline）
						if fbAcc != nil {
							reqLog.AccountID = fbAcc.AccountID
							reqLog.AccountEmail = fbAcc.Email
						}
						defer fbResp.Body.Close()
						if isStream {
							serveChatStream(r.Context(), w, fbResp, fbAcc, &reqLog, func() (*http.Response, *Account, error) {
								rr, ra, rerr := callClineAPI(params, true)
								if rerr == nil {
									stampUpstream(&reqLog, params)
								}
								if m, ok := params["model"].(string); ok && m != "" {
									reqLog.Model = m
								}
								return rr, ra, rerr
							})
						} else {
							handleNonStreamResponse(w, fbResp, fbAcc, &reqLog)
						}
						return
					}
					// 兜底也失败：zen 的真实失败原因不能被吞 —— 最终错误带上两段，
					// 日志/UI 能直接看到 zen 为什么挂、Cline 兜底为什么也没接住
					err = fmt.Errorf("zen: %v; cline free chain: %w", err, fbErr)
				}
				log.Printf("  api error: %v", err)
				finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, err.Error())
				writeJSON(w, http.StatusBadGateway, map[string]any{
					"error": map[string]string{"message": err.Error(), "type": "api_error"},
				})
				return
			}
			defer resp.Body.Close()
			if isStream {
				serveChatStream(r.Context(), w, resp, nil, &reqLog, func() (*http.Response, *Account, error) {
					fbResp, fbAcc, fbErr, attempted := zenFailoverToCline(params, true)
					if !attempted {
						return nil, nil, fmt.Errorf("no cline fallback available")
					}
					if fbErr == nil {
						stampUpstream(&reqLog, params)
						if m, ok := params["model"].(string); ok && m != "" {
							reqLog.Model = m
						}
					}
					return fbResp, fbAcc, fbErr
				})
			} else {
				handleNonStreamResponse(w, resp, nil, &reqLog)
			}
			return
		}

		// cline 池路径才要求账号；zen 免费模型不依赖本地账号池
		if activeCount == 0 && len(loadPool().Accounts) == 0 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": map[string]string{
					"message": "No accounts in pool. Run with --add-account or POST /admin/login to add accounts.",
					"type":    "auth_error",
				},
			})
			return
		}

		// Cline 通道：截短超长工具名并记录映射，回程由响应处理器还原（Meta 后端硬限 ≤64）
		// 主动压缩同样适用于 cline/provider 路由 —— 此前只有 zen 路径会压缩，
		// 大上下文请求会直接撞上游窗口失败。
		zm := modelForCompaction(model)
		compactForRequest(params, zm, requestSessionID(params, r.Header), "chat")
		reqLog.ToolNames = clampParamsToolNames(params)
		resp, acc, err := callClineAPI(params, isStream)
		if err != nil && isContextLengthError(err) {
			if out := fallbackTruncate(params, zm); out.changed {
				log.Printf("  chat %s", out.note)
				resp, acc, err = callClineAPI(params, isStream)
			}
		}
		if effectiveModel, ok := params["model"].(string); ok && effectiveModel != "" {
			reqLog.Model = effectiveModel // 含回退后的实际服务模型
		}
		stampUpstream(&reqLog, params) // 归因实际服务方（provider / opencode / cline）
		if acc != nil {
			reqLog.AccountID = acc.AccountID
			reqLog.AccountEmail = acc.Email // 失败也要记账号：否则日志显示不出实际被 402/429 的账号
		}
		if err != nil {
			log.Printf("  api error: %v", err)
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, err.Error())
			writeJSON(w, clineErrorHTTPStatus(err), map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "api_error"},
			})
			return
		}
		defer resp.Body.Close()
		if acc != nil {
			reqLog.AccountID = acc.AccountID
			reqLog.AccountEmail = acc.Email
		}

		if isStream {
			serveChatStream(r.Context(), w, resp, acc, &reqLog, func() (*http.Response, *Account, error) {
				rr, ra, rerr := callClineAPI(params, true)
				if rerr == nil {
					stampUpstream(&reqLog, params)
				}
				if m, ok := params["model"].(string); ok && m != "" {
					reqLog.Model = m
				}
				return rr, ra, rerr
			})
		} else {
			handleNonStreamResponse(w, resp, acc, &reqLog)
		}
	})
	mux.HandleFunc("/v1/chat/completions", withBodyLimit(chatHandler))
	mux.HandleFunc("/chat/completions", withBodyLimit(chatHandler))

	// Anthropic Messages API support
	anthropicHandler := apiKeyHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		handleAnthropicMessages(w, r)
	})
	mux.HandleFunc("/v1/messages", withBodyLimit(anthropicHandler))
	mux.HandleFunc("/messages", withBodyLimit(anthropicHandler))

	// Anthropic Messages API token counting (polyfilled)
	countTokensHandler := apiKeyHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		handleAnthropicCountTokens(w, r)
	})
	mux.HandleFunc("/v1/messages/count_tokens", withBodyLimit(countTokensHandler))
	mux.HandleFunc("/messages/count_tokens", withBodyLimit(countTokensHandler))

	// OpenAI Responses API support（所有上游：zen 免费模型 + Cline 账号池）
	responsesHandler := apiKeyHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		handleResponses(w, r)
	})
	mux.HandleFunc("/v1/responses", withBodyLimit(responsesHandler))
	mux.HandleFunc("/responses", withBodyLimit(responsesHandler))

	if host == "" {
		host = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	listenHost = host
	listenPort = port
	serverMux = mux
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	serverMu.Lock()
	currentServer = server
	serverMu.Unlock()

	// 启动后台冷却恢复巡检
	startCooldownRecovery()

	fmt.Println("")
	fmt.Println(strings.Repeat("=", 58))
	fmt.Printf("  Cline Go Proxy %s - No CLI Required\n", appVersion)
	fmt.Println(strings.Repeat("=", 58))
	fmt.Printf("  http://%s\n", addr)
	fmt.Printf("  http://%s/v1\n", addr)
	if !isLoopbackHost(host) {
		for _, ip := range detectLocalIPs() {
			fmt.Printf("  http://%s:%d (LAN)\n", ip, port)
		}
		fmt.Println("  !!! 监听非本机地址，管理后台无鉴权，请确认网络环境安全")
	}
	fmt.Println("  API Key: any value")
	fmt.Printf("  Model:   %s\n", getDefaultModel())
	fmt.Printf("  Accounts: %d total, %d active\n", len(loadPool().Accounts), activeCount)
	if zc := getZenConfig(); zc.Enabled {
		fmt.Printf("  OpenCode: enabled (%s free models)\n", strings.TrimRight(zc.BaseURL, "/"))
	} else {
		fmt.Println("  OpenCode: disabled")
	}
	fmt.Println(strings.Repeat("=", 58))

	return server.ListenAndServe()
}

func corsHandler(h http.HandlerFunc) http.HandlerFunc {
	return httpx.CORS(h)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	httpx.WriteJSON(w, status, data)
}

func cleanMessages(messages []any) []any {
	return openaichat.CleanMessages(messages)
}

// shortenToolName truncates names above the upstream limit deterministically.
func shortenToolName(orig string) string {
	return openaichat.ShortenToolName(orig)
}

// clampParamsToolNames shortens over-long tool names and returns the mapping
// used to restore them on the response path.
func clampParamsToolNames(params map[string]any) map[string]string {
	return openaichat.ClampToolNames(params)
}

// restoreToolCallsInResponse restores shortened tool names in stream and
// non-stream response shapes.
func restoreToolCallsInResponse(obj map[string]any, shortToOrig map[string]string) {
	openaichat.RestoreToolCalls(obj, shortToOrig)
}

// sanitizeMessages repairs malformed tool_calls in outbound history.
func sanitizeMessages(messages []any) []any {
	return openaichat.SanitizeMessages(messages)
}

// genToolUseID returns a fallback tool_use block id.
func genToolUseID() string {
	return openaichat.GenToolUseID()
}

// hasToolUseBlocks reports whether content contains an Anthropic tool_use block.
func hasToolUseBlocks(content any) bool {
	return openaichat.HasToolUseBlocks(content)
}

func numericInt(value any) (int, bool) {
	return openaichat.NumericInt(value)
}

// modelMaxOutputLimit 返回模型已知的最大输出 token 硬上限（0=未知，不封顶）。
// 已知硬限制表优先（gemini-3.8-flash 等）；其次取池中该模型的 Output 元数据
// （remote 同步 / 管理页设置 / 用户自定义模型）。zen 模型的 Output 是压缩预算
// 用的估值（未知模型默认 32768），作为硬上限会误伤长输出，跳过。
func modelMaxOutputLimit(model string) int {
	if meta, ok := lookupClineModelMeta(model); ok && meta.Output > 0 {
		return meta.Output
	}
	if strings.TrimSpace(model) == "" {
		return 0
	}
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	for _, m := range p.Models {
		if m.ID == model && !isZenSource(m) && m.Output > 0 {
			return m.Output
		}
	}
	return 0
}

func buildUpstreamBody(params map[string]any, stream bool) map[string]any {
	sessionID := fmt.Sprintf("sess_%d", time.Now().UnixMilli())

	maxTokens := maxOutputTokens()
	source := ""
	if mt, ok := numericInt(params["max_tokens"]); ok {
		maxTokens, source = clampMaxTokens(mt), "max_tokens"
	} else if mt, ok := numericInt(params["max_completion_tokens"]); ok {
		maxTokens, source = clampMaxTokens(mt), "max_completion_tokens"
	}
	// 客户端发的 0 视为未设置、1~15 低于上游硬下限：一律兜到默认值，
	// 否则 muse-spark 等模型直接 400 且错误会被回退链吞掉
	if maxTokens < minUpstreamMaxTokens {
		if source != "" {
			log.Printf("  clamp %s=%d -> %d (upstream requires >= %d)", source, maxTokens, maxOutputTokens(), minUpstreamMaxTokens)
		}
		maxTokens = maxOutputTokens()
	}

	model := getDefaultModel()
	if m, ok := params["model"].(string); ok && m != "" {
		model = m
	}

	// 模型已知硬上限封顶：gemini-3.8-flash 最大输出 65536，默认预算 128000 会被
	// 上游网关 400（maxOutputTokens out of range / Request contains an invalid argument），
	// 且 429 配额限流回退到 vertex/google 路由时必现。
	if limit := modelMaxOutputLimit(model); limit > 0 && maxTokens > limit {
		log.Printf("  clamp max_tokens=%d -> %d (model %q output limit)", maxTokens, limit, model)
		maxTokens = limit
	}

	body := map[string]any{
		"model":            model,
		"max_tokens":       maxTokens,
		"session_id":       sessionID,
		"reasoning_effort": reasoningEffort(),
	}

	if msgsRaw, ok := params["messages"]; ok {
		if msgsArr, ok := msgsRaw.([]any); ok {
			body["messages"] = sanitizeMessages(msgsArr)
		} else {
			body["messages"] = msgsRaw
		}
	}

	if stream {
		body["stream"] = true
	}

	if re, ok := params["reasoning_effort"].(string); ok && re != "" {
		body["reasoning_effort"] = re
	} else if re, ok := params["reasoningEffort"].(string); ok && re != "" {
		body["reasoning_effort"] = re
	}

	// Cline 上游不接受 "none" 枚举：客户端显式关闭推理时直接省略该字段；
	// 空字符串同样省略（避免把非法枚举下发到上游）。
	if re, _ := body["reasoning_effort"].(string); re == "none" || re == "" {
		delete(body, "reasoning_effort")
	}

	for _, key := range passThroughKeys {
		if val, ok := params[key]; ok {
			body[key] = val
		}
	}

	// 定稿前清洗 tools：null 可选字段会被部分上游网关硬拒 400（见函数注释）。
	sanitizeToolsForUpstream(body)

	return body
}

// sanitizeToolsForUpstream cleans optional tool fields before a request body is
// finalized for any gateway.
func sanitizeToolsForUpstream(body map[string]any) {
	openaichat.SanitizeTools(body)
}

func clineHeaders(token, sessionID string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	h.Set("Content-Type", "application/json")
	h.Set("X-Task-ID", sessionID)

	cfg := getProxyConfig()
	for k, v := range cfg.Headers {
		h.Set(k, v)
	}

	return h
}

type clineAPIError struct {
	statusCode int
	message    string
}

func (e *clineAPIError) Error() string {
	return fmt.Sprintf("API %d: %s", e.statusCode, e.message)
}

type clineAccountUnavailableError struct {
	err error
}

func (e *clineAccountUnavailableError) Error() string {
	return e.err.Error()
}

func (e *clineAccountUnavailableError) Unwrap() error {
	return e.err
}

type freeModelUnavailableError struct {
	message string
}

func (e *freeModelUnavailableError) Error() string {
	return e.message
}

func clineErrorHTTPStatus(err error) int {
	if _, ok := err.(*freeModelUnavailableError); ok {
		return http.StatusTooManyRequests
	}
	return http.StatusInternalServerError
}

// isDeterministicRequestError 判断 err 是否为「换账号/换模型重试也无法自愈」的
// 上游确定性 400：invalid-request 语义（invalid_request_error 类型、invalid_request
// 伪码、网关字段校验 "Invalid input"，如工具可选字段被序列化为 null）。
// 上下文超限 400 显式排除（isContextLengthError）：那类错误有压缩+重试的自愈路径，
// 必须保留现有的 truncate/retry 行为。限流 429/5xx 不在此列。
func isDeterministicRequestError(err error) bool {
	if err == nil || isContextLengthError(err) {
		return false
	}
	var apiErr *clineAPIError
	if !errors.As(err, &apiErr) || apiErr.statusCode != http.StatusBadRequest {
		return false
	}
	msg := strings.ToLower(apiErr.message)
	return strings.Contains(msg, "invalid_request_error") ||
		strings.Contains(msg, "invalid_request") ||
		strings.Contains(msg, "invalid input")
}

// servedByParam 是 callClineAPI 族回写在 params 上的「实际服务方」标记：
// 自定义 provider 成功服务时写入 upstreamProvider。仅用于请求日志归因——
// buildUpstreamBody / callProvider / callZenAPI 只拷贝白名单键，不会外发。
const servedByParam = "__served_by"

// providerNameParam 与 servedByParam 配套：自定义 provider 成功服务时回写实际
// 命中之家的显示名，供 stampUpstream 归因到请求日志的 providerName 字段。
const providerNameParam = "__served_by_name"

// stampUpstream 按实际服务结果写入请求日志的上游归因：
// provider 标记 > 生效模型是 zen 模型（反向故障转移）> Cline 账号池。
func stampUpstream(reqLog *RequestLog, params map[string]any) {
	if v, _ := params[servedByParam].(string); v == upstreamProvider {
		reqLog.Upstream = upstreamProvider
		reqLog.ProviderName, _ = params[providerNameParam].(string)
		return
	}
	if m, _ := params["model"].(string); m != "" {
		if _, isZen := resolveZenInfo(m); isZen {
			reqLog.Upstream = upstreamOpenCode
			reqLog.ProviderName = ""
			return
		}
	}
	reqLog.Upstream = upstreamCline
	reqLog.ProviderName = ""
}

func callClineAPI(params map[string]any, stream bool) (*http.Response, *Account, error) {
	model, _ := params["model"].(string)
	delete(params, servedByParam)     // 归因标记按次重算，避免流重试沿用上一次结果
	delete(params, providerNameParam) // 同步清掉上次归因的 provider 名
	// Request-level pin "model@ProviderName": only the named provider may
	// serve. An explicit pin must fail loudly rather than silently fall back
	// to a redundancy sibling, the Cline pool, or the fallback chain.
	if bare, pin := splitProviderPin(model); pin != "" {
		params["model"] = bare // upstream body and log attribution carry the bare id
		if getProxyConfig().OnlyFree && !modelFreeOnlyEligible(bare) {
			return nil, nil, fmt.Errorf("model %q is not eligible while onlyFree is enabled (access class: %s)", bare, modelRouteAccess(bare).Class)
		}
		resp, perr, attempted, provName := callCustomProviderAPINamed(params, stream, pin)
		switch {
		case attempted && perr == nil:
			params[servedByParam] = upstreamProvider
			params[providerNameParam] = provName
			return resp, nil, nil
		case !attempted:
			return nil, nil, fmt.Errorf("no enabled provider named %q serves model %q", pin, bare)
		default:
			return nil, nil, perr
		}
	}
	if model == "free" {
		resp, acc, err := callFreeClineAPI(params, stream)
		if err != nil {
			// Cline 残血池整条链耗尽（429 冷却/无账号）→ 落到 zen 免费模型。
			// 转移前记下链尾实际模型：没有任何 zen 模型服务成功时还原，
			// 否则请求日志会把失败请求记成最后一个 zen 尝试的模型
			// （链尾 cline 模型才是实际归宿）。
			lastChainModel, _ := params["model"].(string)
			fbResp, _ := clineFailoverToZen(params, stream)
			if fbResp != nil {
				return fbResp, nil, nil
			}
			if lastChainModel != "" {
				params["model"] = lastChainModel
			}
		}
		return resp, acc, err
	}
	// zen 免费模型进入 cline 池仅发生在 zen 故障转移期间：改写成 cline 侧可用的
	// free 模型链，否则 Cline 上游会报 "invalid model format. Expected format:
	// modelType/model"（zen 的裸模型 ID 不符合 Cline 的 provider/model 格式）。
	if zm, ok := resolveZenInfo(model); ok && isZenFreeModel(zm) {
		log.Printf("  zen failover: rewriting zen model %q to cline free chain", model)
		return callFreeClineAPI(params, stream)
	}

	// 显式模型请求降级序列：
	//  1. 自定义 provider（若该模型接入了 provider）
	//  2. 客户端点名模型走 Cline 池
	//  3. modelChain（管理员配置或内置 free 链）逐个降级
	//  4. 全部失败 → 明确报错
	// 可用性感知重排：点名模型保持首位，其余按「未冷却优先、用量少优先」
	// 重排，避免回退流量每次都集中砸在第一个可用模型上直到它也冷却。
	sorted := sortModelsByAvailability(modelFallbackChain(model))
	chain := make([]string, 0, len(sorted)+1)
	chain = append(chain, model)
	for _, m := range sorted {
		if m != model {
			chain = append(chain, m)
		}
	}
	if getProxyConfig().OnlyFree {
		eligible := make([]string, 0, len(chain))
		for _, m := range chain {
			if modelFreeOnlyEligible(m) {
				eligible = append(eligible, m)
			} else {
				log.Printf("  onlyFree: skipping model %q (access class %s)", m, modelRouteAccess(m).Class)
			}
		}
		chain = eligible
		if len(chain) == 0 {
			return nil, nil, fmt.Errorf("onlyFree is enabled and model %q has no verified free route", model)
		}
	}
	// 会话亲和（管理员开关，默认关闭）：同一会话在 TTL 内钉到同一 Cline 账号。
	var affinitySk string
	if getProxyConfig().SessionAffinity.Enabled {
		affinitySk = sessionKeyFromParams(params)
	}
	// 回退轮数/每轮凭证上限由管理员 RetryPolicy 决定；默认 rounds=1、
	// credCap=0（不限），与旧行为完全一致。rounds>1 时每轮之间等待一个有界
	// 窗口，给短冷却账号恢复的机会；确定性错误/stop 规则走 return 直接终止，
	// 不会被后续轮次空转重跑。
	rounds, credCap, roundInterval := retryRoundSettings()
	for round := 0; round < rounds; round++ {
		if round > 0 && roundInterval > 0 {
			log.Printf("  retry round %d/%d for %q after %s cooldown window", round+1, rounds, model, roundInterval)
			time.Sleep(roundInterval)
		}
		for _, m := range chain {
			// 先试自定义 provider
			if pResp, pErr, attempted, provName := callCustomProviderAPI(withModel(params, m), stream); attempted {
				if pErr == nil {
					if m != model {
						log.Printf("  model fallback: %q unavailable, serving via provider %q", model, m)
					}
					params["model"] = m // 回写实际服务模型，供请求日志归因
					params[servedByParam] = upstreamProvider
					params[providerNameParam] = provName
					return pResp, nil, nil
				}
				// callCustomProviderAPI 已逐个记录 provider 失败原因，链上不重复打日志
				var st *requestErrorStop
				if errors.As(pErr, &st) {
					return nil, nil, st.err // 错误规则声明终止：不再走账号池/链
				}
			}

			// 点名模型保持原有轮询语义；链上的降级模型改用「最久未用优先」，
			// 避免所有降级流量都压到第一个可用账号/模型的额度上。
			pickAcc := pickAccountForModelStrict
			if m != model {
				pickAcc = pickAccountForModelLeastUsed
			}
			attempts := 0
			for {
				if credCap > 0 && attempts >= credCap {
					break // 本轮该模型已试满 maxCredentialsPerRound 个账号
				}
				acc, pinned := pickAffinityAccount(affinitySk, m)
				if !pinned {
					acc = pickAcc(m)
				}
				if acc == nil {
					break // 该模型所有账号均冷却/不可用 → 尝试链上下一个模型
				}
				attempts++
				resp, usedAcc, err := callClineAPIWithAccountCtx(context.Background(), acc, withModel(params, m), stream)
				observeAffinityOutcome(affinitySk, usedAcc, err)
				if err == nil {
					if m != model {
						log.Printf("  model fallback: %q cooling on all accounts, serving via %q", model, m)
					}
					params["model"] = m // 回写实际服务模型，供请求日志归因
					return resp, usedAcc, nil
				}
				var accountErr *clineAccountUnavailableError
				if errors.As(err, &accountErr) {
					continue
				}
				// 管理员错误规则先于内置 402/429 分类：stop 立即中止整条链，
				// continue 落到下面照常轮转（仅可能附加冷却），二者都不改变成功路径。
				if stop, cooldown, matched := ruleDecision(err); matched {
					if cooldown && usedAcc != nil {
						setModelCooldown(usedAcc, m, ruleCooldownUntil(clineErrorStatus(err)))
					}
					if stop {
						logRuleHit("stop", clineErrorStatus(err), "pool model "+m)
						return nil, usedAcc, err
					}
					logRuleHit("continue", clineErrorStatus(err), "pool model "+m)
				}
				apiErr, ok := err.(*clineAPIError)
				if ok && apiErr.statusCode == http.StatusPaymentRequired {
					// 402 余额不足是账号级问题（该账号扣费额度耗尽），不是模型级：
					// 冷却该账号上的这个模型（30min），换下一个账号继续试当前模型。
					// 不换账号会导致轮询反复选中同一破产账号，每个请求先烧一次 402 往返；
					// 不能整账号冷却 —— 同账号上仍在免费列表的模型零成本可用。
					setModelCooldown(usedAcc, m, time.Now().Add(30*time.Minute))
					log.Printf("  model %q on %s: insufficient credits (402), cooling for 30m, trying next account", m, usedAcc.Email)
					continue
				}
				if !ok || apiErr.statusCode != http.StatusTooManyRequests {
					// 非 429/402 错误：若后面还有候选（provider / 链）则继续降级，否则透传
					if !hasAnyFallbackLeft(model, m) {
						return nil, usedAcc, err
					}
					break
				}
				// 429：模型冷却已记录，换下一个账号；全部冷却后降级到下一个模型
			}
		}
	}
	// 终极兜底：free 链（手动链全部失败时自动切换）
	if model != "free" && !isFreeAliasModel(model) {
		fp := withModel(params, "free")
		if resp, acc, err := callFreeClineAPI(fp, stream); err == nil {
			log.Printf("  auto fallback: all configured options failed for %q, served by free chain", model)
			if fm, ok := fp["model"].(string); ok && fm != "" {
				params["model"] = fm // callFreeClineAPI 就地改写 fp，取回实际服务模型
			}
			if v, ok := fp[servedByParam].(string); ok {
				params[servedByParam] = v // provider 经 free 链服务时同步归因标记
			} else {
				delete(params, servedByParam)
			}
			if v, ok := fp[providerNameParam].(string); ok {
				params[providerNameParam] = v // 同步实际服务的 provider 名
			} else {
				delete(params, providerNameParam)
			}
			return resp, acc, nil
		}
	}
	// Cline 侧（含 free 链）全部耗尽 → 反向故障转移到 zen 免费模型。
	// 没有任何 zen 模型服务成功时还原模型名：请求日志按 params 记实际服务
	// 模型，不能记成最后一次失败的 zen 尝试
	fbResp, _ := clineFailoverToZen(params, stream)
	if fbResp != nil {
		return fbResp, nil, nil
	}
	if model != "" {
		params["model"] = model
	}
	if hasActiveAccounts() {
		return nil, nil, &freeModelUnavailableError{message: fmt.Sprintf("model %q is cooling on all accounts and no fallback model is available", model)}
	}
	return nil, nil, fmt.Errorf("no active accounts available. Use --login or admin API to add accounts")
}

// withModel 返回带指定模型名的参数副本（避免污染调用方 map）。
func withModel(params map[string]any, model string) map[string]any {
	cp := make(map[string]any, len(params)+1)
	for k, v := range params {
		cp[k] = v
	}
	cp["model"] = model
	return cp
}

// isFreeAliasModel 判断是否为 "free" 别名或链内免费模型（避免重复兜底）。
func isFreeAliasModel(model string) bool {
	return dispatch.IsFreeAlias(model, defaultFreeChain())
}

// defaultFreeChain 动态派生默认回退链（管理员未配置 modelChain 时）：
// 优先取 Cline 远程同步的免费模型（有效、不下架），再补内置常量链。
// 已下架的内置模型（如 longcat-2.0，不在远程同步列表）自动排除，不再写死；
// 从未同步成功（离线）时回退内置常量链。
func defaultFreeChain() []string {
	remote := remoteModelsActive()
	p := loadPool()
	known := make(map[string]bool, len(p.Models))
	var remoteFree []string
	for _, m := range p.Models {
		known[m.ID] = true
		if remote && m.Source == "remote" && m.Cost == "free" && m.Status == "active" {
			remoteFree = append(remoteFree, m.ID)
		}
		// 启用中自定义 provider 的免费模型参与自动回退（provider 失败时也会被
		// callFreeClineAPI 内的 callCustomProviderAPI 尝试，其次才是账号池）
		if m.Source == "provider" && m.Cost == "free" && m.Status == "active" && !m.Disabled {
			remoteFree = append(remoteFree, m.ID)
		}
	}
	chain := make([]string, 0, len(remoteFree)+len(freeModelChain))
	seen := make(map[string]bool, len(remoteFree)+len(freeModelChain))
	for _, m := range remoteFree {
		if !seen[m] {
			seen[m] = true
			chain = append(chain, m)
		}
	}
	for _, m := range freeModelChain {
		if seen[m] || (remote && !known[m]) {
			continue
		}
		seen[m] = true
		chain = append(chain, m)
	}
	if len(chain) == 0 {
		return freeModelChain
	}
	return chain
}

// liveModelSet 返回当前仍然「在线」的模型 ID 集合：池内全部模型（远程同步
// remote / opencode zen 同步 / 管理员自定义，已手动停用的除外）+ 启用中自定义
// provider 声明的模型。
// 调用方须持有 poolMu；providers 走独立的 providersMu，仓库内不存在反向持锁路径。
func liveModelSet(p *AccountPool) map[string]bool {
	live := make(map[string]bool, len(p.Models))
	disabled := make(map[string]bool)
	for _, m := range p.Models {
		if m.Disabled {
			disabled[m.ID] = true
			continue
		}
		live[m.ID] = true
	}
	// provider 声明的模型（已同步进池的直接生效；池内被停用的一致排除）
	for _, sp := range listProviders() {
		if !sp.Enabled {
			continue
		}
		for _, id := range sp.ModelIDs {
			if disabled[id] {
				continue
			}
			live[id] = true
		}
	}
	return live
}

// isModelDisabled 判断模型是否被管理员在管理页手动停用（Disabled 标记）。
// 支持 "opencode/" 前缀别名（与 routeModel 的匹配方式一致）。
// 停用语义：不进客户端模型列表、直接请求在入口处拒绝、默认模型与回退链跳过。
func isModelDisabled(id string) bool {
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(id, "opencode/")
	if id == "" {
		return false
	}
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	for _, m := range p.Models {
		if m.ID == id && m.Disabled {
			return true
		}
	}
	return false
}

// filterStaleModels 从回退链里剔除已下架的模型。
//
// 上游免费/推荐模型每日调整，写死或旧配置里的模型会随下架而失效：继续尝试
// 只会白白多一次必败的上游往返（404/400 + 冷却），排在链头时每个请求都要先
// 撞一次失败。判定来源：远程同步成功的模型列表（含 zen/自定义）+ 启用中
// provider 的 modelIds。
//
// 远程同步从未成功（离线冷启动）时不判定、原样保留；剔空时也原样保留
// （宁可按原链试一次，也不要空链直接报错）。
func filterStaleModels(chain []string) []string {
	if len(chain) == 0 || !remoteModelsActive() {
		return chain
	}
	p := loadPool()
	poolMu.Lock()
	live := liveModelSet(p)
	poolMu.Unlock()
	out, dropped := dispatch.FilterLive(chain, true, live)
	if len(dropped) == 0 {
		return out
	}
	log.Printf("  model chain: dropped delisted model(s) %v -> %v", dropped, out)
	return out
}

// hasAnyFallbackLeft 判断点名模型之后是否还有候选（决定 500 是否透传）。
func hasAnyFallbackLeft(requested, current string) bool {
	return dispatch.HasFallbackAfter(modelFallbackChain(requested), current)
}

// modelFallbackChain 显式模型的降级序列：点名模型优先，其后是管理员配置的
// 回退链（modelChain）；未配置时动态派生默认链（排除已下架模型）。均去重。
// 用户显式配置的链**不做列表过滤**：官方列表之外常有实测仍可用的模型
// （列表只是参考不是白名单），失效条目由请求时的模型级冷却与故障转移兜底；
// 只有动态派生的免费链才按 liveModelSet 过滤下架条目。点名模型本身始终
// 保留 —— 它是客户端显式要求的（可能由 zen / provider 服务，不归本仓判定）。
func modelFallbackChain(requested string) []string {
	configured := getProxyConfig().ModelChain
	if len(configured) == 0 {
		configured = filterStaleModels(defaultFreeChain())
	}
	return dispatch.BuildChain(requested, configured)
}

// hasActiveAccounts 池中是否存在 active 状态的账号。
func hasActiveAccounts() bool {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	for _, a := range p.Accounts {
		if a.Status == "active" {
			return true
		}
	}
	return false
}

func callFreeClineAPI(params map[string]any, stream bool) (*http.Response, *Account, error) {
	// "free" 别名的实际顺序：管理员配置的回退链优先，否则动态派生默认链。
	configured := getProxyConfig().ModelChain
	chain := configured
	if len(configured) == 0 {
		chain = defaultFreeChain()
	}
	// 先剔除已下架模型，再按可用性重排：把流量摊到用量最少的可用模型上，
	// 而不是顺序打满第一个，也不在必败的死模型上浪费一次上游往返。
	chain = filterStaleModels(chain)
	chain = sortModelsByAvailability(chain)
	if getProxyConfig().OnlyFree {
		eligible := make([]string, 0, len(chain))
		for _, m := range chain {
			if modelFreeOnlyEligible(m) {
				eligible = append(eligible, m)
			} else {
				log.Printf("  onlyFree: skipping free-chain model %q (access class %s)", m, modelRouteAccess(m).Class)
			}
		}
		chain = eligible
	}
	delete(params, servedByParam)     // 本次调用重新归因
	delete(params, providerNameParam) // 同步清掉上次归因的 provider 名
	var affinitySk string
	if getProxyConfig().SessionAffinity.Enabled {
		affinitySk = sessionKeyFromParams(params)
	}
	var lastErr error
	lastWas429 := false
	for _, model := range chain {
		params["model"] = model
		// 与显式模型链一致：链内模型命中自定义 provider 时先走 provider，
		// 失败（记冷却）后继续用账号池兜底——free 别名不再绕过 provider。
		if pResp, pErr, attempted, provName := callCustomProviderAPI(params, stream); attempted {
			if pErr == nil {
				params[servedByParam] = upstreamProvider
				params[providerNameParam] = provName
				return pResp, nil, nil
			}
			// callCustomProviderAPI 已逐个记录 provider 失败原因，链上不重复打日志
			var st *requestErrorStop
			if errors.As(pErr, &st) {
				return nil, nil, st.err // 错误规则声明终止 free 链
			}
		}
		// "free" 链是纯降级路径：全部用「最久未用优先」挑账号，摊平用量。
		// 会话亲和开启时优先复用绑定的账号（free 别名请求正是上游 session
		// 连续性最有价值的路径）。
		var freeAffinity *Account
		if affinitySk != "" {
			freeAffinity = accountAffinity.get(affinitySk, model, time.Now())
		}
		for {
			acc := freeAffinity
			freeAffinity = nil // 绑定账号只试一次，失败后融入常规轮转
			if acc == nil {
				acc = pickAccountForModelLeastUsed(model)
			}
			if acc == nil {
				break
			}

			resp, usedAcc, err := callClineAPIWithAccountCtx(context.Background(), acc, params, stream)
			observeAffinityOutcome(affinitySk, usedAcc, err)
			if err == nil {
				return resp, usedAcc, nil
			}
			var accountErr *clineAccountUnavailableError
			if errors.As(err, &accountErr) {
				continue
			}
			// 管理员错误规则先于内置分类：stop 中止整条 free 链。
			if stop, cooldown, matched := ruleDecision(err); matched {
				if cooldown && usedAcc != nil {
					setModelCooldown(usedAcc, model, ruleCooldownUntil(clineErrorStatus(err)))
				}
				if stop {
					logRuleHit("stop", clineErrorStatus(err), "free-chain model "+model)
					return nil, usedAcc, err
				}
				logRuleHit("continue", clineErrorStatus(err), "free-chain model "+model)
			}
			lastErr = err
			lastWas429 = false
			if apiErr, ok := err.(*clineAPIError); ok && (apiErr.statusCode == http.StatusTooManyRequests || apiErr.statusCode == http.StatusPaymentRequired) {
				// 429 限流 / 402 余额不足：都是账号级问题 —— 402 冷却该账号上的
				// 这个模型后换下一个账号继续试，不能换模型（其他账号可能还有额度）
				if apiErr.statusCode == http.StatusPaymentRequired {
					setModelCooldown(acc, model, time.Now().Add(30*time.Minute))
					log.Printf("  model %q on %s: insufficient credits (402), cooling for 30m, trying next account", model, acc.Email)
				}
				lastWas429 = true
				continue
			}
			// 确定性 payload 400：链上其余模型会因同一非法请求体收到同一 400，
			// 整条链直接放弃 —— 不再逐账号/逐模型空转注定失败的上游往返。
			if isDeterministicRequestError(err) {
				return nil, usedAcc, err
			}
			// 其他 API 错误（如上游 500）：换链内下一个模型降级
			break
		}
	}
	if lastErr != nil {
		if lastWas429 {
			// 整条链被限流耗尽：按 free 池不可用处理（429）
			return nil, nil, &freeModelUnavailableError{message: "no eligible accounts available for free models"}
		}
		return nil, nil, lastErr
	}
	return nil, nil, &freeModelUnavailableError{message: "no eligible accounts available for free models"}
}

func callClineAPIWithAccount(acc *Account, params map[string]any, stream bool) (*http.Response, *Account, error) {
	return callClineAPIWithAccountCtx(context.Background(), acc, params, stream)
}

func callClineAPIWithAccountCtx(ctx context.Context, acc *Account, params map[string]any, stream bool) (*http.Response, *Account, error) {
	token, err := ensureAccountToken(acc)
	if err != nil {
		// Try other accounts
		return nil, acc, &clineAccountUnavailableError{err: fmt.Errorf("account %s token failed: %w", acc.Email, err)}
	}

	body := buildUpstreamBody(params, stream)
	sessionID, _ := body["session_id"].(string)

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, acc, fmt.Errorf("marshal body: %w", err)
	}

	client := &clineupstream.Client{
		HTTP:         &http.Client{Transport: httpClient.Transport},
		BaseURL:      clineAPIBase,
		ExtraHeaders: getProxyConfig().Headers,
	}

	toolCount := 0
	if tools, ok := params["tools"]; ok {
		if t, ok := tools.([]any); ok {
			toolCount = len(t)
		}
	}
	log.Printf("  upstream: account=%s stream=%v tools=%d msgs=%d max_tokens=%v effort=%v",
		truncateEmail(acc.Email), stream, toolCount, getMsgCount(params), body["max_tokens"], body["reasoning_effort"])

	resp, err := client.Chat(ctx, token, sessionID, bodyJSON)
	if err != nil {
		acc.Status = "cooldown"
		acc.CooldownUntil = time.Now().Add(5 * time.Minute)
		savePool()
		return nil, acc, &clineAccountUnavailableError{err: fmt.Errorf("upstream request: %w", err)}
	}

	if resp.StatusCode == 401 {
		resp.Body.Close()
		// Refresh token and retry
		if err := refreshAccountToken(acc); err == nil {
			token = acc.AccessToken
			resp, err = client.Chat(ctx, token, sessionID, bodyJSON)
			if err != nil {
				acc.Status = "cooldown"
				acc.CooldownUntil = time.Now().Add(5 * time.Minute)
				savePool()
				return nil, acc, &clineAccountUnavailableError{err: fmt.Errorf("upstream retry: %w", err)}
			}
			if resp.StatusCode == 401 {
				resp.Body.Close()
				acc.Status = "expired"
				savePool()
				return nil, acc, &clineAccountUnavailableError{err: fmt.Errorf("account %s token expired permanently", acc.Email)}
			}
		} else {
			return nil, acc, &clineAccountUnavailableError{err: fmt.Errorf("account %s refresh failed: %w", acc.Email, err)}
		}
	}

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		bodyStr := string(bodyBytes)
		// 429：模型级冷却 —— 只暂停该模型，账号保持可用，其他模型继续转发
		if resp.StatusCode == 429 {
			model, _ := body["model"].(string)
			until := parseCooldownUntil(bodyStr)
			if model != "" {
				setModelCooldown(acc, model, until)
			} else {
				acc.Status = "cooldown"
				acc.CooldownUntil = until
				savePool()
			}
		}
		// 上游明确报「模型不存在」时清理下架残留（同步标记 Delisted 保留的模型）
		if model, _ := body["model"].(string); model != "" && isModelGoneError(resp.StatusCode, bodyStr) {
			markModelGone(model)
		}
		return nil, acc, &clineAPIError{statusCode: resp.StatusCode, message: truncate(bodyStr, 500)}
	}

	acc.LastUsed = time.Now()
	acc.UsageCount++
	savePool()
	return resp, acc, nil
}

type accountTestResult struct {
	AccountID    string `json:"accountId"`
	Email        string `json:"email"`
	OK           bool   `json:"ok"`
	DurationMs   int64  `json:"durationMs"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	Error        string `json:"error,omitempty"`
}

// parseCooldownUntil 从 429 响应体中解析 "Try again in 1h 1m" 格式的等待时长，
// 返回预计恢复时间；解析失败则回退到 1 小时后。
var cooldownRe = regexp.MustCompile(`(?i)try\s+again\s+in\s+(\d+)\s*h?(?:\s*(\d+))?\s*m?`)

func parseCooldownUntil(body string) time.Time {
	matches := cooldownRe.FindStringSubmatch(body)
	if len(matches) >= 2 {
		hours, _ := strconv.Atoi(matches[1])
		minutes := 0
		if len(matches) >= 3 && matches[2] != "" {
			minutes, _ = strconv.Atoi(matches[2])
		}
		if hours > 0 || minutes > 0 {
			return time.Now().Add(time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute)
		}
	}
	// 解析失败，回退 1 小时
	return time.Now().Add(1 * time.Hour)
}

// startCooldownRecovery 启动后台 goroutine，每 30 秒检查一次 cooldown 账号，
// 对 CooldownUntil 已过期的账号执行探活，成功则自动激活。
func startCooldownRecovery() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			p := loadPool()
			poolMu.Lock()
			var toRecover []*Account
			for _, acc := range p.Accounts {
				if acc.Status != "cooldown" {
					continue
				}
				// 有恢复时间且已过期 → 探活
				// 无恢复时间（旧数据）→ 也尝试探活
				if acc.CooldownUntil.IsZero() || time.Now().After(acc.CooldownUntil) {
					toRecover = append(toRecover, acc)
				}
			}
			poolMu.Unlock()

			for _, acc := range toRecover {
				log.Printf("cooldown recovery: testing %s", acc.Email)
				result := testAccount(acc)
				if result.OK {
					log.Printf("cooldown recovery: %s reactivated", acc.Email)
				} else {
					log.Printf("cooldown recovery: %s still unavailable: %s", acc.Email, result.Error)
				}
			}
		}
	}()
}

// testAccount sends a minimal "hi" request through a specific account to verify
// it can complete an upstream call. It does not update aggregate token counters
// or request logs; it is a diagnostic-only probe.
func testAccount(acc *Account) accountTestResult {
	result := accountTestResult{AccountID: acc.AccountID, Email: acc.Email}
	started := time.Now()

	// max_tokens 不能太小：推理模型（effort=high）会把预算全耗在 reasoning 上，
	// 16 token 时上游必回「empty response content」500，账号测试与冷却恢复
	// 频繁误报（issue #32）。1024 对连通性探针足够且成本可忽略。
	params := map[string]any{
		"model":      getDefaultModel(),
		"max_tokens": 1024,
		"stream":     false,
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}

	resp, _, err := callClineAPIWithAccount(acc, params, false)
	if err != nil {
		result.DurationMs = time.Since(started).Milliseconds()
		result.Error = truncate(err.Error(), 200)
		return result
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		result.DurationMs = time.Since(started).Milliseconds()
		result.Error = "read response: " + truncate(err.Error(), 200)
		return result
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		result.DurationMs = time.Since(started).Milliseconds()
		result.Error = "decode response: " + truncate(err.Error(), 200)
		return result
	}
	if data, ok := obj["data"]; ok {
		if d, ok := data.(map[string]any); ok {
			obj = d
		}
	}
	obj = normalizeOpenAIResponse(obj)
	usage := parseTokenUsage(obj["usage"])

	result.OK = true
	result.DurationMs = time.Since(started).Milliseconds()
	if usage.Valid {
		result.InputTokens = usage.Prompt
		result.OutputTokens = usage.Completion
	}
	// If the account was in cooldown/expired but the test succeeded, restore it.
	if acc.Status != "active" {
		poolMu.Lock()
		acc.Status = "active"
		poolMu.Unlock()
		savePool()
	}
	return result
}

type tokenUsage = openaichat.Usage

func parseTokenUsage(value any) tokenUsage {
	return openaichat.ParseUsage(value)
}

func mergeTokenUsage(current, next tokenUsage) tokenUsage {
	return openaichat.MergeUsage(current, next)
}

func recordTokenUsage(acc *Account, model string, usage tokenUsage) {
	if acc == nil || !usage.Valid {
		return
	}
	// 先判断是否免费模型（getAllModels 会拿 poolMu，必须在持有锁之前计算）
	isFree := model != "" && isFreeModelID(model)
	poolMu.Lock()
	acc.PromptTokens += usage.Prompt
	acc.CompletionTokens += usage.Completion
	acc.TotalTokens += usage.Total
	acc.CachedTokens += usage.Cached
	// 按模型细分统计（仅记录 free 模型）
	if isFree {
		if acc.ModelStats == nil {
			acc.ModelStats = make(map[string]*ModelStat)
		}
		st := acc.ModelStats[model]
		if st == nil {
			st = &ModelStat{ModelID: model, Cost: "free"}
			acc.ModelStats[model] = st
		}
		st.UsageCount++
		st.PromptTokens += usage.Prompt
		st.CompletionTokens += usage.Completion
		st.TotalTokens += usage.Total
		st.CachedTokens += usage.Cached
	}
	poolMu.Unlock()
	savePool()
}

// isFreeModelID 判断模型是否为 free 计费（用于按模型统计和模型级冷却）。
func isFreeModelID(model string) bool {
	for _, m := range getAllModels() {
		if m.ID == model {
			return m.Cost == "free"
		}
	}
	// 未知模型：按 ID 后缀/前缀启发式判断
	return strings.HasSuffix(model, ":free") || strings.Contains(model, "/free/")
}

// modelCooldownActive 判断某账号下该模型是否处于模型级冷却中。
func modelCooldownActive(acc *Account, model string) bool {
	if acc == nil || model == "" {
		return false
	}
	poolMu.Lock()
	defer poolMu.Unlock()
	until, ok := acc.ModelCooldowns[model]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(acc.ModelCooldowns, model)
		savePool()
		return false
	}
	return true
}

// hasAlternativeModel 判断降级链上是否还有未冷却的候选模型。
// 用于避免「所有模型都被冷却」导致整条链不可用。
func hasAlternativeModel(acc *Account, current string) bool {
	for _, m := range modelFallbackChain(current) {
		if m == "" || m == current {
			continue
		}
		if !modelCooldownActive(acc, m) {
			return true
		}
	}
	return false
}

// setModelCooldown 记录模型级冷却（429 或首 token 前失败时调用）：只暂停该模型，账号保持可用。
// fallback 为解析失败时的恢复时长（默认 1 小时）。
func setModelCooldown(acc *Account, model string, until time.Time) {
	if acc == nil || model == "" {
		return
	}
	poolMu.Lock()
	if acc.ModelCooldowns == nil {
		acc.ModelCooldowns = make(map[string]time.Time)
	}
	acc.ModelCooldowns[model] = until
	poolMu.Unlock()
	savePool()
	log.Printf("model cooldown: account=%s model=%s until=%s", truncateEmail(acc.Email), model, until.Format("15:04:05"))
}

func truncateEmail(email string) string {
	if len(email) <= 12 {
		return email
	}
	parts := splitEmail(email)
	if len(parts) == 2 && len(parts[0]) > 3 {
		return parts[0][:3] + "***@" + parts[1]
	}
	if len(email) > 12 {
		return email[:8] + "..."
	}
	return email
}

func splitEmail(email string) []string {
	for i := 0; i < len(email); i++ {
		if email[i] == '@' {
			return []string{email[:i], email[i+1:]}
		}
	}
	return []string{email}
}

func getMsgCount(params map[string]any) int {
	if msgs, ok := params["messages"].([]any); ok {
		return len(msgs)
	}
	return 0
}

func hasFirstOutput(obj map[string]any) bool {
	return openaichat.HasFirstOutput(obj)
}

func handleNonStreamResponse(w http.ResponseWriter, upstream *http.Response, acc *Account, reqLog *RequestLog) {
	copyPassthroughHeaders(w, upstream.Header)
	var raw map[string]any
	if err := json.NewDecoder(upstream.Body).Decode(&raw); err != nil {
		finalizeRequestLog(reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "parse_error"},
		})
		return
	}

	// Some Cline responses wrap in {data: {...}}
	out := raw
	if data, ok := raw["data"]; ok {
		if d, ok := data.(map[string]any); ok {
			out = d
		}
	}

	out = normalizeOpenAIResponse(out)
	restoreToolCallsInResponse(out, reqLog.ToolNames)
	usage := parseTokenUsage(out["usage"])
	recordUsage(acc, upstream, reqLog, reqLog.Model, usage)
	finalizeRequestLog(reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")

	if msg, ok := getNested(out, "choices", 0, "message").(map[string]any); ok {
		tc, _ := msg["tool_calls"].([]any)
		content, _ := msg["content"].(string)
		log.Printf("  nonstream finish=%v tool_calls=%d content_len=%d",
			getNested(out, "choices", 0, "finish_reason"),
			len(tc), len(content))
	}

	writeJSON(w, http.StatusOK, out)
}

// Anthropic Messages API support
type anthropicMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicReq struct {
	Model             string          `json:"model"`
	MaxTokens         int             `json:"max_tokens"`
	Messages          []anthropicMsg  `json:"messages"`
	System            json.RawMessage `json:"system,omitempty"`
	Stream            bool            `json:"stream,omitempty"`
	Temperature       float64         `json:"temperature,omitempty"`
	TopP              float64         `json:"top_p,omitempty"`
	TopK              int             `json:"top_k,omitempty"`
	Stop              json.RawMessage `json:"stop_sequences,omitempty"`
	Thinking          json.RawMessage `json:"thinking,omitempty"`
	Tools             json.RawMessage `json:"tools,omitempty"`
	ToolChoice        json.RawMessage `json:"tool_choice,omitempty"`
	Metadata          json.RawMessage `json:"metadata,omitempty"`
	ContextManagement json.RawMessage `json:"context_management,omitempty"`
	Extra             map[string]any  `json:"-"`
}

func loadOverrideContent() string {
	data, err := os.ReadFile("override.md")
	if err != nil {
		// 每个请求都会调用本函数；“文件不存在”只在首次打印一次，避免刷屏
		if overrideMissLogged.CompareAndSwap(false, true) {
			log.Printf("  override.md not found: %v", err)
		}
		return ""
	}
	overrideMissLogged.Store(false) // 文件出现了，重新允许日后删除时再提示一次
	content := strings.TrimSpace(string(data))
	if content != "" {
		log.Printf("  using override.md as system prompt (%d bytes)", len(content))
	} else {
		log.Printf("  override.md is empty")
	}
	return content
}

// overrideMissLogged override.md 缺失日志的去重标记（详见 loadOverrideContent）。
var overrideMissLogged atomic.Bool

func extractStringContent(raw json.RawMessage) string {
	return anthropicconv.ExtractStringContent(raw)
}

func anthropicToolsToOpenAI(tools []any) []any {
	return anthropicconv.ToolsToOpenAI(tools)
}

func anthropicToOpenAI(req anthropicReq) map[string]any {
	msgs := make([]anthropicconv.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, anthropicconv.Message{Role: m.Role, Content: m.Content})
	}
	return anthropicconv.ToOpenAI(anthropicconv.Request{
		Model: req.Model, MaxTokens: req.MaxTokens, Stream: req.Stream,
		Temperature: req.Temperature, TopP: req.TopP, System: req.System,
		Messages: msgs, Tools: req.Tools, ToolChoice: req.ToolChoice, Thinking: req.Thinking,
	}, anthropicconv.Options{
		SystemOverride:  loadOverrideContent(),
		ReasoningEffort: reasoningEffort(),
		NewToolUseID:    genToolUseID,
		Logf:            log.Printf,
	})
}

func openAIToAnthropic(openAI map[string]any) map[string]any {
	return anthropicconv.FromOpenAI(openAI, anthropicconv.Options{NewToolUseID: genToolUseID})
}

// zenFailoverToCline zen 调用失败后的透明降级：改走 cline 账号池 free 模型链。
// attempted=false 表示未启用故障转移，调用方维持原错误路径。
// 注意：失败计数由 callZenAPI 内部标记（每请求恰好一次），此处不再重复计数，
// 否则限流/服务错误路径 + 此处各计一次，故障转移会被过早触发。
func zenFailoverToCline(params map[string]any, stream bool) (*http.Response, *Account, error, bool) {
	cfg := getZenConfig()
	if !cfg.Failover {
		return nil, nil, nil, false
	}
	orig, _ := params["model"].(string)
	log.Printf("  zen failover: %q unavailable upstream, falling back to cline free pool", orig)
	params["model"] = "free"
	resp, acc, err := callFreeClineAPI(params, stream)
	return resp, acc, err, true
}

// clineFailoverToZen Cline 侧（含 free 池链）全部耗尽后的反向故障转移：
// 落到 opencode zen 免费模型。与 zenFailoverToCline 方向相反，形成双向闭环——
// 任一免费上游挂掉，流量自动落到另一条。zen 未启用或处于故障转移窗口
// （连续失败被判定不可达）时不尝试；最多试 3 个免费模型，避免在坏模型上反复烧时间。
func clineFailoverToZen(params map[string]any, stream bool) (*http.Response, bool) {
	cfg := getZenConfig()
	if !cfg.Enabled || zenFailedNow() {
		return nil, false
	}
	orig, _ := params["model"].(string)
	attempts := 0
	for _, m := range currentZenModels() {
		if !isZenFreeModel(m) || m.ID == orig {
			continue
		}
		attempts++
		if attempts > failoverModelCap() {
			break
		}
		log.Printf("  cline failover: %q exhausted, trying zen free model %q", orig, m.ID)
		params["model"] = m.ID
		resp, err := callZenAPI(params, stream)
		if err == nil {
			return resp, true
		}
		log.Printf("  cline failover: zen model %q failed: %v", m.ID, err)
	}
	return nil, false
}

func handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "parse_error"},
		})
		return
	}

	var req anthropicReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "parse_error"},
		})
		return
	}

	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": "messages is required", "type": "parse_error"},
		})
		return
	}

	req.MaxTokens = clampMaxTokens(req.MaxTokens)

	openAIReq := anthropicToOpenAI(req)

	log.Printf("  anthropic: model=%s stream=%v msgs=%d", req.Model, req.Stream, len(req.Messages))

	reqLog := RequestLog{StartedAt: time.Now(), Protocol: "anthropic", Model: req.Model, Stream: req.Stream}

	// Administrator model policy: alias rewrite + wildcard exclusions.
	if resolved, reject := resolveClientModel(req.Model); reject != "" {
		finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, reject)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": reject, "type": "invalid_request_error"},
		})
		return
	} else if resolved != req.Model {
		req.Model = resolved
		reqLog.Model = resolved
		openAIReq["model"] = resolved
	}

	if msg := freeOnlyRejection(req.Model); msg != "" {
		finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": msg, "type": "invalid_request_error"},
		})
		return
	}

	// Context management (Anthropic context_management edits) — polyfill.
	// Inspected before routing so compaction can rewrite openAIReq["messages"].
	var cmResult *CMResult
	cmPaused := false
	var cmCompactionSummary string
	var cmApplied []CMAppliedEdit
	if len(req.ContextManagement) > 0 {
		res, err := applyContextManagement(r.Context(), req, openAIReq)
		if err != nil {
			msg := err.Error()
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": msg, "type": "invalid_request_error"}})
			return
		}
		cmResult = res
		if res != nil {
			if res.AppliedEdits != nil {
				cmApplied = res.AppliedEdits
			} else {
				cmApplied = []CMAppliedEdit{}
			}
			if res.Compacted {
				cmCompactionSummary = res.Summary
				cmPaused = res.Paused
			}
		}
	}

	// Paused compaction: return compaction-only response, no upstream call.
	if cmPaused {
		if req.Stream {
			sw := newSSEWriter(w)
			sw.Start()
			msgID := "msg_" + fmt.Sprintf("%x", reqLog.StartedAt.UnixMilli())
			emit := func(event string, data any) {
				d, _ := json.Marshal(data)
				sw.Write([]byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(d))))
			}
			if !sw.Envelope {
				sw.Envelope = true
				emit("message_start", map[string]any{
					"type": "message_start",
					"message": map[string]any{
						"id": msgID, "type": "message", "role": "assistant",
						"content": []any{}, "model": req.Model, "stop_reason": nil,
					},
				})
			}
			sw.Write([]byte(": ping\n\n"))
			if !sw.CompactionEmitted {
				sw.CompactionEmitted = true
				emitCompactionBlock(sw, cmCompactionSummary)
			}
			emit("message_delta", map[string]any{
				"type":               "message_delta",
				"delta":              map[string]any{"stop_reason": "compaction", "stop_sequence": nil},
				"usage":              map[string]any{"output_tokens": 0},
				"context_management": cmContextManagementResponse(cmApplied),
			})
			emit("message_stop", map[string]any{"type": "message_stop"})
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, true, "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":                 "msg_" + fmt.Sprintf("%x", reqLog.StartedAt.UnixMilli()),
			"type":               "message",
			"role":               "assistant",
			"content":            []any{map[string]any{"type": "compaction", "content": cmCompactionSummary}},
			"model":              req.Model,
			"stop_reason":        "compaction",
			"usage":              map[string]any{"input_tokens": 0, "output_tokens": 0},
			"context_management": cmContextManagementResponse(cmApplied),
		})
		finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, true, "")
		return
	}

	// 管理员手动停用的模型在入口直接拒绝（不进列表，也不可点名调用）
	if req.Model != "" && isModelDisabled(req.Model) {
		msg := fmt.Sprintf("model %q is disabled", req.Model)
		finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": msg, "type": "invalid_request_error"},
		})
		return
	}

	// 模型组合：在普通路由之前分流（组合 ID 不是真实上游模型）。按配置顺序回退，
	// 默认不触发全局 ModelChain / free 链。
	if combo, ok := findCombo(req.Model); ok {
		if !combo.Enabled {
			msg := fmt.Sprintf("model combo %q is disabled", req.Model)
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": msg, "type": "invalid_request_error"},
			})
			return
		}
		sessionID := requestSessionID(map[string]any{"session_id": r.Header.Get("x-opencode-session")}, nil)
		reqLog.ToolNames = clampParamsToolNames(openAIReq)

		if req.Stream {
			streamAnthropicCombo(r.Context(), w, openAIReq, combo, &reqLog, sessionID, cmCompactionSummary, cmApplied)
			return
		}

		run := runCombo(openAIReq, combo, false, sessionID)
		applyComboTrace(&reqLog, combo, run)
		stampUpstream(&reqLog, openAIReq)
		if run.Acc != nil {
			reqLog.AccountID = run.Acc.AccountID
			reqLog.AccountEmail = run.Acc.Email
		}
		if run.Err != nil {
			log.Printf("  anthropic combo %q error: %v", combo.ID, run.Err)
			msg := comboTraceSummary(combo, run)
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
			writeJSON(w, clineErrorHTTPStatus(run.Err), map[string]any{
				"error": map[string]string{"message": msg, "type": "api_error"},
			})
			return
		}
		defer run.Resp.Body.Close()
		copyPassthroughHeaders(w, run.Resp.Header)
		var raw map[string]any
		if err := json.NewDecoder(run.Resp.Body).Decode(&raw); err != nil {
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "parse_error"},
			})
			return
		}
		out := normalizeOpenAIResponse(unwrapDataEnvelope(raw))
		usage := parseTokenUsage(out["usage"])
		recordUsage(run.Acc, run.Resp, &reqLog, reqLog.Model, usage)
		finalizeRequestLog(&reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")
		restoreToolCallsInResponse(out, reqLog.ToolNames)
		anthropicResp := openAIToAnthropic(out)
		applyCMToResponse(anthropicResp, cmResult)
		if hasToolUseBlocks(anthropicResp["content"]) {
			anthropicResp["stop_reason"] = "tool_use"
		}
		writeJSON(w, http.StatusOK, anthropicResp)
		return
	}

	// 按 model 自动分流（与 chat 端点一致）：zen 免费/付费拒绝/Cline 池
	switch routeModel(req.Model) {
	case "reject":
		msg := fmt.Sprintf("model %q is a paid opencode model; only free models are proxied", req.Model)
		finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": msg, "type": "invalid_request_error"},
		})
		return
	case "zen":
		reqLog.Upstream = upstreamOpenCode
		zm, _ := resolveZenInfo(req.Model)
		out := maybeCompact(openAIReq, zm, requestSessionID(map[string]any{"session_id": r.Header.Get("x-opencode-session")}, nil))
		if out.changed {
			log.Printf("  anthropic %s", out.note)
		}
		resp, err := callZenAPI(openAIReq, req.Stream)
		// 上游返回「上下文超限」400：压缩后重试一次
		if err != nil && isContextLengthError(err) {
			log.Printf("  anthropic: context length exceeded, compacting and retrying")
			out2 := fallbackTruncate(openAIReq, zm)
			if out2.changed {
				log.Printf("  anthropic %s", out2.note)
				resp, err = callZenAPI(openAIReq, req.Stream)
			}
		}
		if err != nil {
			// zen 故障转移到 Cline 池：先截短超长工具名（Meta 后端硬限 ≤64）
			reqLog.ToolNames = clampParamsToolNames(openAIReq)
			if fbResp, fbAcc, fbErr, attempted := zenFailoverToCline(openAIReq, req.Stream); attempted {
				if fbErr == nil {
					log.Printf("  anthropic failover: serving %q via cline pool", req.Model)
					stampUpstream(&reqLog, openAIReq) // 归因实际服务方（provider / cline）
					if fm, ok := openAIReq["model"].(string); ok && fm != "" {
						reqLog.Model = fm // zen 故障转移后记录实际服务模型
					}
					if fbAcc != nil {
						reqLog.AccountID = fbAcc.AccountID
						reqLog.AccountEmail = fbAcc.Email
					}
					defer fbResp.Body.Close()
					copyPassthroughHeaders(w, fbResp.Header)
					if req.Stream {
						sw := newSSEWriter(w)
						// 这条降级路径没有重试，所以用宽容的上限，尽量把慢但能出的流交给客户端。
						if !handleAnthropicStream(r.Context(), sw, fbResp, fbAcc, &reqLog, lastAttemptFirstContentTimeout, cmCompactionSummary, cmApplied) {
							log.Printf("  anthropic stream: failover upstream failed before any content")
							finishAnthropicStreamFailure(sw, w, &reqLog, http.StatusBadGateway,
								"stream failed before first token", "upstream stream failed before first token")
						}
					} else {
						var raw map[string]any
						if err := json.NewDecoder(fbResp.Body).Decode(&raw); err != nil {
							finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
							writeJSON(w, http.StatusInternalServerError, map[string]any{
								"error": map[string]string{"message": err.Error(), "type": "parse_error"},
							})
							return
						}
						out2 := normalizeOpenAIResponse(unwrapDataEnvelope(raw))
						usage := parseTokenUsage(out2["usage"])
						recordUsage(fbAcc, fbResp, &reqLog, reqLog.Model, usage)
						finalizeRequestLog(&reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")
						restoreToolCallsInResponse(out2, reqLog.ToolNames)
						anthropicResp := openAIToAnthropic(out2)
						applyCMToResponse(anthropicResp, cmResult)
						if hasToolUseBlocks(anthropicResp["content"]) {
							anthropicResp["stop_reason"] = "tool_use"
						}
						writeJSON(w, http.StatusOK, anthropicResp)
					}
					return
				}
				// 兜底也失败：zen 的真实失败原因不能被吞 —— 最终错误带上两段，
				// 日志/UI 能直接看到 zen 为什么挂、Cline 兜底为什么也没接住
				err = fmt.Errorf("zen: %v; cline free chain: %w", err, fbErr)
			}
			log.Printf("  anthropic api error: %v", err)
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, err.Error())
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "api_error"},
			})
			return
		}
		defer resp.Body.Close()
		copyPassthroughHeaders(w, resp.Header)
		if req.Stream {
			sw := newSSEWriter(w)
			// 同样是单次尝试（无重试），用宽容的上限。
			if !handleAnthropicStream(r.Context(), sw, resp, nil, &reqLog, lastAttemptFirstContentTimeout, cmCompactionSummary, cmApplied) {
				log.Printf("  anthropic stream: zen upstream failed before any content")
				finishAnthropicStreamFailure(sw, w, &reqLog, http.StatusBadGateway,
					"stream failed before first token", "zen stream failed before first token")
			}
		} else {
			var raw map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
				finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error": map[string]string{"message": err.Error(), "type": "parse_error"},
				})
				return
			}
			out2 := normalizeOpenAIResponse(unwrapDataEnvelope(raw))
			usage := parseTokenUsage(out2["usage"])
			finalizeRequestLog(&reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")
			anthropicResp := openAIToAnthropic(out2)
			applyCMToResponse(anthropicResp, cmResult)
			if hasToolUseBlocks(anthropicResp["content"]) {
				anthropicResp["stop_reason"] = "tool_use"
			}
			writeJSON(w, http.StatusOK, anthropicResp)
		}
		return
	}

	activeCount := 0
	p := loadPool()
	for _, a := range p.Accounts {
		if a.Status == "active" {
			activeCount++
		}
	}

	if activeCount == 0 && len(p.Accounts) == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{
				"message": "No accounts in pool",
				"type":    "auth_error",
			},
		})
		return
	}

	// Cline 池 / provider 路由也要主动压缩：此前仅 zen 路径会压，大上下文
	// 请求会直接撞上游窗口失败。
	zm := modelForCompaction(req.Model)
	compactForRequest(openAIReq, zm, requestSessionID(map[string]any{"session_id": r.Header.Get("x-opencode-session")}, nil), "anthropic")

	if req.Stream {
		contextRetried := false
		streamAnthropicWithRetry(r.Context(), w, func() (*http.Response, *Account, error) {
			resp, acc, err := callClineAPI(openAIReq, req.Stream)
			if err != nil && !contextRetried && isContextLengthError(err) {
				contextRetried = true
				if out := fallbackTruncate(openAIReq, zm); out.changed {
					log.Printf("  anthropic %s", out.note)
					resp, acc, err = callClineAPI(openAIReq, req.Stream)
				}
			}
			if err == nil {
				if effectiveModel, ok := openAIReq["model"].(string); ok && effectiveModel != "" {
					reqLog.Model = effectiveModel // 含回退后的实际服务模型
				}
				stampUpstream(&reqLog, openAIReq) // 归因实际服务方（provider / opencode / cline）
			}
			return resp, acc, err
		}, &reqLog, cmCompactionSummary, cmApplied)
		return
	}

	// Cline 通道：截短超长工具名并记录映射，回程由响应处理器还原（Meta 后端硬限 ≤64）
	reqLog.ToolNames = clampParamsToolNames(openAIReq)
	resp, acc, err := callClineAPI(openAIReq, req.Stream)
	if err != nil && isContextLengthError(err) {
		if out := fallbackTruncate(openAIReq, zm); out.changed {
			log.Printf("  anthropic %s", out.note)
			resp, acc, err = callClineAPI(openAIReq, req.Stream)
		}
	}
	if effectiveModel, ok := openAIReq["model"].(string); ok && effectiveModel != "" {
		reqLog.Model = effectiveModel // 含回退后的实际服务模型
	}
	stampUpstream(&reqLog, openAIReq) // 归因实际服务方（provider / opencode / cline）
	if acc != nil {
		reqLog.AccountID = acc.AccountID
		reqLog.AccountEmail = acc.Email // 失败也要记账号
	}
	if err != nil {
		log.Printf("  anthropic api error: %v", err)
		finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, err.Error())
		writeJSON(w, clineErrorHTTPStatus(err), map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "api_error"},
		})
		return
	}
	defer resp.Body.Close()
	copyPassthroughHeaders(w, resp.Header)
	if acc != nil {
		reqLog.AccountID = acc.AccountID
		reqLog.AccountEmail = acc.Email
	}

	{
		var raw map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "parse_error"},
			})
			return
		}
		out := raw
		if data, ok := raw["data"]; ok {
			if d, ok := data.(map[string]any); ok {
				out = d
			}
		}
		out = normalizeOpenAIResponse(out)
		usage := parseTokenUsage(out["usage"])
		recordUsage(acc, resp, &reqLog, reqLog.Model, usage)
		finalizeRequestLog(&reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")
		restoreToolCallsInResponse(out, reqLog.ToolNames)
		anthropicResp := openAIToAnthropic(out)

		applyCMToResponse(anthropicResp, cmResult)
		if hasToolUseBlocks(anthropicResp["content"]) {
			anthropicResp["stop_reason"] = "tool_use"
		}

		writeJSON(w, http.StatusOK, anthropicResp)
	}
}

// maxStreamAttempts 预提交失败的最大尝试次数（含首次）。实际次数还会被降级链长度限制：
// 3 次 × 短耐心，总等待被压在客户端可接受范围内，而不是在整条链上把时间耗光。
// 链上有几个模型就最多试几个，避免在同一个模型上反复空等。
const maxStreamAttempts = 3

// firstContentTimeoutForAttempt 返回本次尝试等待「首个内容事件」的上限：
// 普通尝试用较短的上限快速换模型；最后一次尝试放宽，宁可慢也尽量把答案交出去。
func firstContentTimeoutForAttempt(attempt, maxAttempts int) time.Duration {
	if attempt >= maxAttempts {
		return envDurationFromMs("CLINE2API_LAST_ATTEMPT_TIMEOUT_MS", lastAttemptFirstContentTimeout)
	}
	return envDurationFromMs("CLINE2API_FIRST_CONTENT_TIMEOUT_MS", defaultFirstContentTimeout)
}

// streamFailoverCooldown 是「上游在产出任何内容前失败」时对该模型施加的冷却时长。
// 目的：点名模型（如 glm-5.3-flash）排队超时 / 空闲 504 / 早断流时，同一次请求内
// 立即换到配置链上的下一个模型，而不是把错误抛给客户端；同时短期内不再把新请求压到它上面。
const streamFailoverCooldown = 2 * time.Minute

// streamAnthropicWithRetry 获取上游响应并转发 Anthropic 流。
//
// 首 token 之前失败（预提交）可以透明重试：客户端只看到已发出的信封与 ping，
// 看不到重试痕迹。每次失败还会把该模型短时冷却，使后续尝试自动切换到配置链上的
// 下一个模型 —— 这是「点名模型老是超时」的缓解：坏模型自动让位给备用模型。
// 预提交失败（上游在任何数据产出前断开/报错/静默超时）时客户端尚未收到任何字节，
// 自动重新获取并重试，对客户端完全透明 —— 这是修复「流式经常断」的核心：
// 排队/思考阶段的 504 idle timeout 以前会直接杀死流，现在变成一次不可见的重试。
// sseWriter 包装 http.ResponseWriter，让预提交重试共享同一个响应：
//   - start() 幂等，重试时不会重复 WriteHeader（Go 会报 superfluous 警告）；
//   - write() 每次写完立即 Flush，保证中间层（Cloudflare 隧道等）及时看到字节。
//
// 同时用 started 记录「是否已有字节发出」，供调用方决定重试用 JSON 还是带内 error。
type sseWriter = dispatch.SSEWriter

func newSSEWriter(w http.ResponseWriter) *sseWriter {
	return dispatch.NewSSEWriter(w)
}

// emitAnthropicErrorEvent 在响应头已发出（SSE 模式）后向客户端报告错误：
// 此时无法再返回 JSON 状态码，改用 Anthropic 协议的 error 事件。
func emitAnthropicErrorEvent(sw *sseWriter, msg string) {
	sw.EmitError(msg)
}

// finishAnthropicStreamFailure 统一处理流式尝试失败后的报错：
// 响应头尚未发送 → JSON 状态码；已经发出 → 带内 error 事件（避免重复 WriteHeader）。
func finishAnthropicStreamFailure(sw *sseWriter, w http.ResponseWriter, reqLog *RequestLog, status int, note, msg string) {
	finalizeRequestLog(reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, note)
	if sw.Started {
		log.Printf("  anthropic stream: %s (reported as in-band error event)", note)
		emitAnthropicErrorEvent(sw, msg)
		return
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"message": msg, "type": "api_error"},
	})
}

// streamAnthropicCombo 组合的 Anthropic 流式路径：按配置顺序逐个条目尝试，仅在
// **尚未向客户端产出任何内容**（文本 / thinking / 具名工具调用）时才切换到下一个
// 条目 —— 复用 handleAnthropicStream 的预提交可重试语义与共享 sseWriter（信封只发
// 一次）。一旦已经交付有意义的内容，该次尝试即视为完成，绝不把答案重开到另一个模型。
// 尝试次数被组合的可用条目数上界约束，避免无限回退。
// compactionSummary / compactionApplied 与单模型流路径（handleAnthropicStream）一致：
// CM 已在路由前应用并改写了 params["messages"]；组合流同样要在信封后发出压缩 advisory
// 块与 message_delta 里的 context_management。compaction 块由 sseWriter.compactionEmitted
// 幂等保护，预提交回退时不会重复发送。
func streamAnthropicCombo(ctx context.Context, w http.ResponseWriter, params map[string]any, combo ModelCombo, reqLog *RequestLog, sessionID string, compactionSummary string, compactionApplied []CMAppliedEdit) {
	sw := newSSEWriter(w)

	type cand struct {
		model string
		entry ComboEntry
		order int
	}
	var cands []cand
	seen := map[string]bool{}
	trace := make([]AttemptLog, 0, len(combo.Entries))
	for i, e := range combo.Entries {
		m := strings.TrimSpace(e.Model)
		if m == "" {
			continue
		}
		if e.Disabled {
			trace = append(trace, AttemptLog{Model: m, Order: i + 1, Outcome: "skipped", Reason: "entry disabled by admin",
				Source: strings.TrimSpace(e.Source), Provider: strings.TrimSpace(e.Provider)})
			continue
		}
		key := comboEntryKey(e)
		if seen[key] || isModelDisabled(m) {
			reason := "duplicate entry"
			if isModelDisabled(m) {
				reason = "model disabled by admin"
			}
			trace = append(trace, AttemptLog{Model: m, Order: i + 1, Outcome: "skipped", Reason: reason,
				Source: strings.TrimSpace(e.Source), Provider: strings.TrimSpace(e.Provider)})
			continue
		}
		seen[key] = true
		cands = append(cands, cand{model: m, entry: e, order: i + 1})
	}
	// 预置组合字段：handleAnthropicStream 成功时会直接 finalize 请求日志。
	applyComboLog(reqLog, combo, trace, "")
	stampUpstream(reqLog, params)

	if len(cands) == 0 {
		finishAnthropicStreamFailure(sw, w, reqLog, http.StatusBadGateway,
			"combo has no eligible entries", "combo has no eligible entries")
		return
	}

	// 组合级主动压缩：以候选模型中最小的有效上下文为预算，一次压缩写回共享
	// params（callSingleModel 压缩的是 map 副本，条目之间不会传递）。
	if out := maybeCompact(params, comboCompactionModel(combo), sessionID); out.changed {
		log.Printf("  combo %q %s", combo.ID, out.note)
	}

	// 上下文超限的「截断重试」整条组合只做一次：截断后的 params 在后续条目间共享。
	// 与 runCombo 的非流式路径保持一致 —— 流式组合此前缺少这一步，导致某个条目
	// 因上下文超限返回 400 时整条组合直接中止（单模型路径会先压缩/截断再回退）。
	truncatedForRun := false

	for idx, c := range cands {
		if ctx.Err() != nil {
			return
		}
		start := time.Now()
		resp, acc, upstream, provName, err := callSingleModel(params, c.entry, true, sessionID)
		if err != nil && isContextLengthError(err) && !truncatedForRun {
			truncatedForRun = true
			log.Printf("  combo %q: entry %d %q context length exceeded, truncating and retrying", combo.ID, c.order, c.model)
			if out := fallbackTruncate(params, modelForCompaction(c.model)); out.changed {
				log.Printf("  combo %s", out.note)
				start = time.Now()
				resp, acc, upstream, provName, err = callSingleModel(params, c.entry, true, sessionID)
			}
		}
		if err != nil {
			trace = append(trace, AttemptLog{Model: c.model, Order: c.order, Upstream: upstream, Status: attemptStatus(err),
				Outcome: "failed", Reason: truncate(err.Error(), 200), DurationMs: time.Since(start).Milliseconds(),
				Source: strings.TrimSpace(c.entry.Source), Provider: strings.TrimSpace(c.entry.Provider),
				ProviderName: provName})
			applyComboLog(reqLog, combo, trace, "")
			if isContextLengthError(err) {
				// 上下文上限是模型/上游各自的属性：当前条目装不下，换下一个条目仍可能成功。
				continue
			}
			if !comboRetryable(err) {
				break
			}
			continue
		}
		params["model"] = c.model
		if upstream == upstreamProvider {
			params[servedByParam] = upstreamProvider
			params[providerNameParam] = provName
		} else {
			delete(params, servedByParam)
			delete(params, providerNameParam)
		}
		// 每次成功建流后按实际服务方重算归因（流式组合此前只在循环外 stamp 过一次，
		// 会把 provider/zen 条目错记为 cline）。
		stampUpstream(reqLog, params)
		if acc != nil {
			reqLog.AccountID = acc.AccountID
			reqLog.AccountEmail = acc.Email
		}
		// 乐观写入成功条目：handleAnthropicStream 成功后会直接 finalize 落盘。
		trace = append(trace, AttemptLog{Model: c.model, Order: c.order, Upstream: upstream,
			Outcome: "success", DurationMs: time.Since(start).Milliseconds(),
			Source: strings.TrimSpace(c.entry.Source), Provider: strings.TrimSpace(c.entry.Provider),
			ProviderName: provName})
		applyComboLog(reqLog, combo, trace, c.model)

		if handleAnthropicStream(ctx, sw, resp, acc, reqLog, firstContentTimeoutForAttempt(idx+1, len(cands)), compactionSummary, compactionApplied) {
			return // 已交付内容 / 已完整收尾（含带内 error）
		}
		resp.Body.Close()
		if ctx.Err() != nil {
			return // 客户端断开：不是模型失败
		}
		// 预提交失败：把乐观成功条目改写为失败，冷却该条目模型，换下一个条目。
		trace[len(trace)-1] = AttemptLog{Model: c.model, Order: c.order, Upstream: upstream,
			Outcome: "failed", Reason: "stream produced no content before first token",
			DurationMs: time.Since(start).Milliseconds(),
			Source:     strings.TrimSpace(c.entry.Source), Provider: strings.TrimSpace(c.entry.Provider),
			ProviderName: provName}
		applyComboLog(reqLog, combo, trace, "")
		// 仅当组合里还有下一个条目时才冷却该模型（避免单条目组合冷却掉唯一模型，
		// 让下一次请求直接失败）；不再用全局 modelFallbackChain 作为判定依据。
		if acc != nil && c.model != "" && idx+1 < len(cands) {
			setModelCooldown(acc, c.model, time.Now().Add(streamFailoverCooldown))
			log.Printf("  combo %q: model %q produced no content, cooling %s and failing over", combo.ID, c.model, streamFailoverCooldown)
		}
	}
	if ctx.Err() != nil {
		return
	}
	log.Printf("  combo %q: giving up after %d attempt(s) with no content", combo.ID, len(cands))
	finishAnthropicStreamFailure(sw, w, reqLog, http.StatusBadGateway,
		"combo stream failed before first token", "upstream stream failed before first token")
}

func streamAnthropicWithRetry(ctx context.Context, w http.ResponseWriter, fetch func() (*http.Response, *Account, error), reqLog *RequestLog, compactionSummary string, compactionApplied []CMAppliedEdit) {
	sw := newSSEWriter(w)
	// 尝试次数不超过降级链上的模型数：每个模型只给一次机会。
	maxAttempts := maxStreamAttempts
	if n := len(modelFallbackChain(reqLog.Model)); n > 0 && n < maxAttempts {
		maxAttempts = n
	}
	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil {
			return // 客户端已断开，不再发起上游请求
		}
		resp, acc, err := fetch()
		if err != nil {
			log.Printf("  anthropic api error: %v", err)
			finishAnthropicStreamFailure(sw, w, reqLog, clineErrorHTTPStatus(err), err.Error(), err.Error())
			return
		}
		if acc != nil {
			reqLog.AccountID = acc.AccountID
			reqLog.AccountEmail = acc.Email
		}
		if handleAnthropicStream(ctx, sw, resp, acc, reqLog, firstContentTimeoutForAttempt(attempt, maxAttempts), compactionSummary, compactionApplied) {
			return
		}
		resp.Body.Close()
		// 客户端主动断开（Esc 中断 / Claude Code 空闲看门狗重连）不是模型的失败：
		// 此时冷却会把用户点名/配置的模型拉黑 2 分钟，后续请求被静默赶到回退链上，
		// 表现为「配置了模型却总走 fallback」。实测见 proxy.log 2026/09/26 09:52:52 /
		// 09:59:00：「client disconnected before first token」紧跟 pixel-canary 被冷却。
		// ctx 由 r.Context() 派生，在 handler 内只有客户端断开才会取消。
		if ctx.Err() != nil {
			return
		}
		// 上游在产出任何内容前失败（排队超时 / 空闲 504 / 早断流）：把这次实际使用的模型
		// 标记为短时冷却，下一次尝试就会自动落到链上的下一个模型（pool 的选择器会跳过
		// 冷却中的模型）。有备用模型时才冷却，避免把整条链一起冷掉。
		if acc != nil && reqLog.Model != "" && hasAlternativeModel(acc, reqLog.Model) {
			setModelCooldown(acc, reqLog.Model, time.Now().Add(streamFailoverCooldown))
			log.Printf("  anthropic stream: model %q produced no content, cooling it for %s and failing over",
				reqLog.Model, streamFailoverCooldown)
		}
		if attempt >= maxAttempts {
			log.Printf("  anthropic stream: giving up after %d attempts with no content", attempt)
			finishAnthropicStreamFailure(sw, w, reqLog, http.StatusBadGateway,
				"stream failed before first token", "upstream stream failed before first token")
			return
		}
		log.Printf("  anthropic stream: upstream produced no content, retrying (%d/%d)", attempt+1, maxAttempts)
		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// handleAnthropicStream 把上游 OpenAI SSE 流转换为 Anthropic 事件流转发给客户端。
//
// 关键设计：
//  1. 响应头 + message_start 在上游接通后立即发出。保活心跳分两段：
//     尚未出内容时只发 SSE 注释行 `: ping` —— 足以喂饱中间层（Cloudflare 隧道 120s
//     Proxy Read Timeout）的字节级读超时、避免 524，但按规范它不是事件，因此**不会**
//     喂客户端的事件级看门狗（CLAUDE_STREAM_IDLE_TIMEOUT_MS）。这是有意为之：注定失败的
//     尝试宁可让客户端自己超时后重连（用户实测「断开重连」好于长时间静默），也不要
//     一直哄着它假活着；一旦出过内容就改发真实 ping 事件，因为那时保住这一回合的产出
//     比让客户端重跑更划算。
//  2. 信封提前发送但内容不提前：只要还没向客户端发出任何内容事件（文本 / thinking /
//     具名工具调用），本次尝试的失败（排队超时、空闲 504、上游静默、早断流）都可以透明
//     重试；重试沿用同一 message_start（msgID 由请求开始时间推导），客户端只看到一条
//     连续消息。判定依据是「有无内容」而不是「有无首行」—— 上游常常先送一条空 delta
//     （仅 role）就静默数分钟，那种情况必须能换模型，否则用户只能干等到超时。
//  3. firstContentTimeout 是本次尝试等待首个内容事件的上限，超时同样是可重试的失败。
//
// 返回 true 表示客户端响应已完整处理（正常收尾或已发出 error 事件）；
// 返回 false 表示上游尚未产出任何内容 —— 调用方重试，或用 sw.Started 判断后发带内 error。
func handleAnthropicStream(ctx context.Context, sw *sseWriter, upstream *http.Response, acc *Account, reqLog *RequestLog, firstContentTimeout time.Duration, compactionSummary string, compactionApplied []CMAppliedEdit) bool {
	if upstream.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(upstream.Body, 2048))
		upstream.Body.Close()
		log.Printf("  anthropic stream: upstream status %d before stream: %s", upstream.StatusCode, truncate(string(b), 200))
		return false
	}
	if sw.Flusher == nil {
		log.Printf("  streaming not supported for client")
		io.Copy(io.Discard, upstream.Body)
		upstream.Body.Close()
		return false
	}
	if !sw.Started {
		copyPassthroughHeaders(sw.W, upstream.Header)
	}
	log.Printf("  anthropic stream: waiting for first token")

	// 上游 SSE 逐行泵：独立 goroutine 阻塞读取，主循环通过 select 同时处理
	// 心跳、客户端断开（ctx）与上游数据；放弃时关闭 body + pumpStop 确保不泄漏。
	type sseLine struct {
		line string
		err  error
	}
	lines := make(chan sseLine)
	pumpStop := make(chan struct{})
	go func() {
		reader := bufio.NewReader(upstream.Body)
		for {
			l, err := reader.ReadString('\n')
			select {
			case lines <- sseLine{l, err}:
			case <-pumpStop:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	defer close(pumpStop)

	emit := func(event string, data any) {
		d, _ := json.Marshal(data)
		sw.Write([]byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(d))))
	}

	// msgID 跨预提交重试保持稳定：整个请求只发一个信封，重试沿用同一条消息。
	msgID := "msg_" + fmt.Sprintf("%x", reqLog.StartedAt.UnixMilli())
	// 块索引/思考-文本生命周期/工具累计是纯协议逻辑，由 anthropic 适配器负责；
	// 这里只做上游泵、心跳、重试与记账。compaction advisory 占用 index 0。
	startIndex := -1
	if compactionSummary != "" {
		startIndex = 0
	}
	st := anthropicconv.NewStreamState(startIndex)
	st.ToolID = genToolUseID
	committed := false

	// 信封提前发送：响应头 + message_start + 首个 ping 事件。
	// 必须从第 0 秒起就有事件流动 —— 大上下文下上游排队/预填充实测 TTFT 可达 3~4 分钟，
	// 期间只有注释行（不是事件）时，Claude Code 的流空闲看门狗会把连接当成卡死。
	// 重试沿用同一信封（sw.Envelope 幂等），客户端看不到任何重试痕迹。
	// model 取 reqLog.Model（调用方已在 fetch 时更新为含回退的实际服务模型），
	// 不能发空串 —— 客户端会丢失流式消息的模型身份。
	sw.Start()
	if !sw.Envelope {
		sw.Envelope = true
		emit("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":          msgID,
				"type":        "message",
				"role":        "assistant",
				"content":     []any{},
				"model":       reqLog.Model,
				"stop_reason": nil,
			},
		})
	}
	// 接通即写一个注释行：从第 0 秒起就有字节流出（Cloudflare 不会 524），
	// 但客户端的事件级看门狗不受影响 —— 这段等待若是白等，客户端会按自己的节奏超时重连。
	sw.Write([]byte(": ping\n\n"))

	if compactionSummary != "" && !sw.CompactionEmitted {
		sw.CompactionEmitted = true
		emitCompactionBlock(sw, compactionSummary)
	}

	// commit：上游首条有效数据行到达，开始实时转发（信封已提前发送）。
	// committed=true 后本次尝试不再重试，只能实时转发或发带内 error。
	commit := func() {
		committed = true
		log.Printf("  anthropic stream: starting real-time forward")
	}

	var latestUsage tokenUsage
	var firstOutputAt time.Time

	// 心跳：SSE 注释行，所有 SSE 客户端按规范忽略，零开销。
	// 提交前后都要发：提交前是防止中间层（Cloudflare 120s 空闲读超时）掐断连接的关键，
	// 提交后用于长时间思考/工具生成阶段保活。
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	// 首个内容事件的上限：上游连一行 data 都没有、或只发空 delta 就长期静默时，
	// 最多等 firstContentTimeout 就放弃本次尝试并让调用方换链上下一个模型。
	firstContentTimer := time.NewTimer(firstContentTimeout)
	defer firstContentTimer.Stop()

L:
	for {
		select {
		case <-ctx.Done():
			// 客户端断开：停止转发并释放上游连接，不再为已放弃的请求继续耗账号流量
			upstream.Body.Close()
			if committed {
				log.Printf("  anthropic stream aborted: client disconnected")
				finalizeRequestLog(reqLog, latestUsage, firstOutputAt, reqLog.StartedAt, false, "client disconnected")
			} else {
				log.Printf("  anthropic stream: client disconnected before first token")
			}
			return committed
		case <-firstContentTimer.C:
			if !st.HasContent() {
				log.Printf("  anthropic stream: no content from upstream within %s, aborting attempt", firstContentTimeout)
				upstream.Body.Close()
				return false
			}
		case <-heartbeat.C:
			// 已出过内容 → 发真实事件：这一回合已经有产出，宁可保活到结束也不要让客户端
			// 中途放弃后整轮重跑（长回合重跑的代价远大于多等一会儿）。
			// 还没出内容 → 只发注释行：只喂字节级看门狗（Cloudflare），注定失败的尝试
			// 让客户端按自己的节奏超时并重连（用户偏好断开重连而非长时间静默假活着）。
			if st.HasContent() {
				emit("ping", map[string]any{"type": "ping"})
			} else {
				sw.Write([]byte(": ping\n\n"))
			}
		case sl, ok := <-lines:
			if !ok {
				// 读取泵退出但未送达错误（防御性处理）
				if !st.HasContent() {
					log.Printf("  anthropic stream: upstream closed before any content, failing over")
					return false
				}
				log.Printf("  anthropic stream broken: upstream closed unexpectedly")
				emit("error", map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": "upstream connection closed unexpectedly"}})
				finalizeRequestLog(reqLog, latestUsage, firstOutputAt, reqLog.StartedAt, false, "upstream closed unexpectedly")
				return true
			}
			if sl.err != nil {
				if !st.HasContent() {
					// 建流后没出任何内容就断了（实测 "unexpected EOF" 会静默 10 分钟才发生）：
					// 不要把它当成「已提交所以只能报错」，换模型重试对客户端完全无感。
					log.Printf("  anthropic stream: upstream ended before any content: %v", sl.err)
					return false
				}
				if errors.Is(sl.err, io.EOF) {
					break L // 正常结束 → 走收尾路径（ReadString 带回的残包与旧实现一致忽略）
				}
				// 真实的读取错误（超时/重置）：记录原因、通知客户端，而不是无声截断
				log.Printf("  anthropic stream broken: %v", sl.err)
				emit("error", map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": sl.err.Error()}})
				finalizeRequestLog(reqLog, latestUsage, firstOutputAt, reqLog.StartedAt, false, "stream read error: "+sl.err.Error())
				return true
			}
			line := strings.TrimRight(sl.line, "\r\n")
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(line[5:])
			if payload == "" {
				continue
			}
			if payload == "[DONE]" {
				if !st.HasContent() {
					// 上游一个内容块都没给就关闭 —— 视为失败，调用方可换模型重试
					log.Printf("  anthropic stream: upstream sent empty stream before any content")
					return false
				}
				continue
			}

			var obj map[string]any
			if err := json.Unmarshal([]byte(payload), &obj); err != nil {
				continue
			}
			if data, ok := obj["data"]; ok {
				if d, ok := data.(map[string]any); ok {
					obj = d
				}
			}
			// 回程还原出站截短的工具名（Meta 后端硬限 ≤64 的映射）
			restoreToolCallsInResponse(obj, reqLog.ToolNames)
			if usage := parseTokenUsage(obj["usage"]); usage.Valid {
				latestUsage = mergeTokenUsage(latestUsage, usage)
			}
			if firstOutputAt.IsZero() && hasFirstOutput(obj) {
				firstOutputAt = time.Now()
			}

			// 上游带内错误（如 {"code":504,"message":"Upstream idle timeout exceeded"}）。
			// 只要还没发出内容事件（StreamState.HasContent 为 false）就仍是可透明重试的失败；
			// 已经发出内容后则转发 error 事件并终止 —— 绝不伪造 end_turn 收尾（旧行为会让
			// 客户端收到 error 后又收到一个「成功的空消息」，表现为静默断流）。
			if errPayload, ok := obj["error"]; ok {
				errBody, _ := json.Marshal(errPayload)
				if !st.HasContent() {
					log.Printf("  anthropic stream: upstream error before any content: %s", string(errBody))
					return false
				}
				log.Printf("  anthropic stream broken by upstream error: %s", string(errBody))
				emit("error", map[string]any{"type": "error", "error": errPayload})
				finalizeRequestLog(reqLog, latestUsage, firstOutputAt, reqLog.StartedAt, false, "upstream error: "+string(errBody))
				return true
			}

			// 上游被截短的工具名还原为客户端原始名
			restoreToolCallsInResponse(obj, reqLog.ToolNames)

			if !committed {
				commit() // 首条有效数据行到达，提交响应头 + message_start
			}

			choices, _ := getNested(obj, "choices").([]any)
			if len(choices) == 0 {
				continue
			}
			choice, _ := choices[0].(map[string]any)
			if choice == nil {
				continue
			}

			for _, ev := range st.PushDelta(choice) {
				emit(ev.Name, ev.Data)
			}
		}
	}

	// 上游正常收尾但一个内容事件都没发出（无文本 / 无思考 / 无具名工具调用）：
	// 这不是可用的回答 —— 实测 glm-5.3-flash 会以 4~5 分钟的空 end_turn 结束。
	// 视为本模型失败：返回 false 让调用方冷却该模型并换链上下一个模型重试。
	// 此时客户端只收到信封与 ping，没有任何内容事件，因此重试完全无感
	// （否则会重复 content_block index，客户端会错乱）。
	if !st.HasContent() {
		log.Printf("  anthropic stream: model %q finished with no content, failing over", reqLog.Model)
		return false
	}

	for _, ev := range st.Finalize() {
		emit(ev.Name, ev.Data)
	}

	msgDelta := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   st.StopReason(),
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": latestUsage.Completion,
		},
	}
	if compactionApplied != nil {
		msgDelta["context_management"] = cmContextManagementResponse(compactionApplied)
	}
	emit("message_delta", msgDelta)
	recordUsage(acc, upstream, reqLog, reqLog.Model, latestUsage)
	finalizeRequestLog(reqLog, latestUsage, firstOutputAt, reqLog.StartedAt, true, "")

	emit("message_stop", map[string]any{"type": "message_stop"})
	log.Printf("  anthropic stream done: hasText=%v tools=%d reason=%s", st.TextStarted(), st.ToolCount(), st.StopReason())
	return true
}

func normalizeOpenAIResponse(obj map[string]any) map[string]any {
	return openaichat.NormalizeResponse(obj)
}

func sanitizeContent(s string) string { return protocol.SanitizeContent(s) }

func normalizeMessage(msg map[string]any) map[string]any {
	return openaichat.NormalizeMessage(msg)
}

func getNested(obj map[string]any, keys ...any) any {
	return protocol.GetNested(obj, keys...)
}

func freePort(port int) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return // port is free
	}
	conn.Close()

	// Try to kill the process using the port
	cmd := execCommand("powershell", "-Command",
		fmt.Sprintf(`$p=Get-NetTCPConnection -LocalPort %d -ErrorAction SilentlyContinue; if($p){Stop-Process -Id $p.OwningProcess -Force}`, port))
	_ = cmd.Run()
	time.Sleep(500 * time.Millisecond)
}
