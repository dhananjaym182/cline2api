package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"cline-go-proxy/internal/config"
	"cline-go-proxy/internal/dispatch"
	"cline-go-proxy/internal/transport"
	zenupstream "cline-go-proxy/internal/upstream/zen"
)

// ============================================================================
// opencode Zen 免费模型支持
// 按请求中的 model 自动分流：zen 免费模型 → https://opencode.ai/zen/v1；
// zen 付费模型 → 400 拒绝；其余 → Cline 账号池。
// zen 模型与 Cline 远程模型同存于 pool.Models（Source="zen"），
// 同步采用全量替换：官方下架的模型自动移除，不会残留僵尸条目。
// ============================================================================

// zenAPIBase 自 2026-10 中旬起免费模型只在 inference API 应答：
// /zen/v1 上的同款请求直接 403 FreeTierError（magpie zen-free 插件 0.1.6 实测）。
const zenAPIBase = "https://opencode.ai/inference/v1"

// 请求日志中的上游标记
const (
	upstreamCline    = "cline"
	upstreamOpenCode = "opencode"
	upstreamProvider = "provider"
)

const zenModelSyncInterval = 10 * time.Minute

// zenHeaderWatchdogTimeout 限制 zen 上游"发出请求 → 收到响应头"的最长等待。
// 看门狗 30s：黑洞连接（抖动代理节点）最多挂 30s 即掐，重试/故障转移更快启动。
// 更长的真实 TTFT（免费层排队 4~11s）不受影响 —— 看门狗只管响应头之前。
const zenHeaderWatchdogTimeout = 30 * time.Second

// zenMaxRetryWait 单次重试的最大等待（上游 Retry-After 可达 13h，绝不能在请求里睡数小时）。
const zenMaxRetryWait = 60 * time.Second

// zenMaxProxyCooldown 出口代理冷却的上限（本地代理端口背后可切换节点，长冷却有害）。
const zenMaxProxyCooldown = 30 * time.Minute

// withCancelOnClose 包装响应 body：调用方关闭 body 时同步释放请求 ctx，
// 避免长生命周期流式响应泄漏 cancel 函数。
func withCancelOnClose(resp *http.Response, cancel context.CancelFunc) *http.Response {
	resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
	once   sync.Once
}

func (b *cancelOnCloseBody) Close() error {
	b.once.Do(b.cancel)
	return b.ReadCloser.Close()
}

// zenSeedModels 内置 zen 免费模型种子表（含别名），仅作为从未同步成功时的离线 fallback。
// 与 builtinModels（Cline 侧）同一模式：同步成功后以远程列表为准。
// 列表与 2026-09-22 线上 /models 实测的免费模型保持一致。
type zenSeedModel struct {
	ID      string
	Aliases []string
	Context int
	Output  int
}

// zenSeedModels 种子表三用途：离线 fallback、免费判定白名单、别名解析。
// Context 默认 1M（2026-09 主流模型普遍 1M 上下文；压缩阈值按此计算，
// 偏小会让压缩过早触发——曾导致 200K 默认值把 1M 模型压到几万 token）。
var zenSeedModels = []zenSeedModel{
	{ID: "deepseek-v4-flash-free", Aliases: []string{"deepseek-v4-flash", "deepseek-v4"}, Context: 200000, Output: 128000},
	{ID: "mimo-v2.6-flash-free", Aliases: []string{"mimo-v2.6-flash", "mimo-v2.6", "mimo"}, Context: 200000, Output: 32000},
	{ID: "mimo-v2.5-free", Aliases: []string{"mimo-v2.5"}, Context: 200000, Output: 32000},
	{ID: "ling-3.0-flash-fin-free", Aliases: []string{"ling-3.0-flash", "ling"}, Context: 200000, Output: 32768},
	{ID: "nemotron-3-ultra-free", Aliases: []string{"nemotron-3-ultra", "nemotron"}, Context: 200000, Output: 128000},
	{ID: "nemotron-3.5-lightning-free", Aliases: []string{"nemotron-3.5-lightning"}, Context: 200000, Output: 32768},
	{ID: "jev-1.13-free", Context: 200000, Output: 32768},
	{ID: "muse-spark-1.3-contributor-free", Context: 200000, Output: 32768},
	{ID: "muse-spark-1.2-contributor-free", Context: 200000, Output: 32768},
	{ID: "big-pickle", Context: 200000, Output: 32000},
}

// builtinZenModels 把种子表转成 Model 条目（离线 fallback 用，Source="seed"）。
func builtinZenModels() []Model {
	out := make([]Model, 0, len(zenSeedModels))
	for _, m := range zenSeedModels {
		out = append(out, Model{
			ID:       m.ID,
			Provider: "opencode",
			Cost:     "free",
			Status:   "active",
			Custom:   false,
			Source:   "seed",
			Context:  m.Context,
			Output:   m.Output,
		})
	}
	return out
}

// remoteZenEnabled：zen 官方 /models 同步成功过 → 以远程列表为准，
// 种子表中已下架的模型自动休眠（不再出现在列表和路由里）。与 remoteModelsEnabled 同一模式。
var (
	remoteZenEnabled   bool
	remoteZenEnabledMu sync.Mutex
)

func remoteZenActive() bool {
	remoteZenEnabledMu.Lock()
	defer remoteZenEnabledMu.Unlock()
	return remoteZenEnabled
}

// isZenSource 判断模型来源是否属于 opencode 体系（同步条目 "zen" / 内置种子 "seed"）。
func isZenSource(m Model) bool {
	return m.Source == "zen" || m.Source == "seed"
}

// currentZenModels 返回当前生效的 zen 模型（pool 中 zen 来源条目；
// 从未同步成功时回退到种子表）。
func currentZenModels() []Model {
	p := loadPool()
	poolMu.Lock()
	var zen []Model
	for _, m := range p.Models {
		if isZenSource(m) {
			zen = append(zen, m)
		}
	}
	poolMu.Unlock()
	if len(zen) > 0 || remoteZenActive() {
		return zen
	}
	return builtinZenModels()
}

// resolveZenInfo 解析模型名到当前生效的 zen 模型。支持别名与 "opencode/" 前缀。
// 别名优先于精确 ID 匹配之后、但优先级高于付费同名 ID（种子的 free 别名不会被覆盖）。
func resolveZenInfo(id string) (Model, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Model{}, false
	}
	models := currentZenModels()

	// 别名表：seed 模型的别名 → 正式 ID（用种子表数据补全上下文）
	contextOf := func(m Model) Model { return m }
	for _, sm := range zenSeedModels {
		for _, a := range sm.Aliases {
			if a == id {
				// 种子模型必须在当前生效列表里才算数（否则就是被下架了）
				for _, m := range models {
					if m.ID == sm.ID && m.Cost == "free" {
						return contextOf(m), true
					}
				}
				break
			}
		}
	}

	tryOne := func(name string) (Model, bool) {
		for _, m := range models {
			if m.ID == name {
				return m, true
			}
		}
		return Model{}, false
	}
	if m, ok := tryOne(id); ok {
		return m, true
	}
	if strings.HasPrefix(id, "opencode/") {
		if m, ok := tryOne(strings.TrimPrefix(id, "opencode/")); ok {
			return m, true
		}
	}
	return Model{}, false
}

// isZenFreeModel 判定 zen 模型是否免费（用于路由：非免费的 zen 模型直接拒绝）。
// 种子白名单兜底：官方免费模型即使同步条目漏标 -free 后缀也不会被误拒。
func isZenFreeModel(m Model) bool {
	if m.Cost == "free" || strings.HasSuffix(m.ID, "-free") {
		return true
	}
	for _, sm := range zenSeedModels {
		if sm.ID == m.ID {
			return true
		}
	}
	return false
}

// routeModel 三态路由："zen" / "reject" / "cline"。
// 故障转移开启且 zen 连续失败期间，免费模型请求临时改走 cline 账号池。
func routeModel(id string) string {
	m, ok := resolveZenInfo(id)
	if !ok {
		return "cline"
	}
	if !isZenFreeModel(m) {
		return "reject"
	}
	// 极端情况：同名 ID 同时是 cline 模型（自定义冲突）→ 让给 cline
	p := loadPool()
	poolMu.Lock()
	for _, pm := range p.Models {
		if !isZenSource(pm) && pm.ID == strings.TrimPrefix(strings.TrimSpace(id), "opencode/") {
			poolMu.Unlock()
			return "cline"
		}
	}
	poolMu.Unlock()

	cfg := getZenConfig()
	if cfg.Failover && zenFailedNow() {
		log.Printf("  failover: zen degraded, %q routed to cline pool", id)
		return "cline"
	}
	return "zen"
}

// ============ zen 配置 ============

type zenCompactConfig = config.ZenCompact

type zenConfigData = config.Zen

func defaultZenConfig() *zenConfigData {
	return config.DefaultZen(zenAPIBase)
}

var (
	zenConfig   *zenConfigData
	zenConfigMu sync.Mutex
)

// getZenConfig 惰性加载配置（避免依赖包初始化顺序）。
func getZenConfig() *zenConfigData {
	zenConfigMu.Lock()
	defer zenConfigMu.Unlock()
	if zenConfig == nil {
		path := resolveDataPath(config.ZenPath)
		cfg, err := config.LoadZen(path, zenAPIBase)
		if err != nil {
			backupCorruptFile(path, err)
		}
		zenConfig = cfg
	}
	return zenConfig
}

// setZenConfig 原子替换配置并持久化，重建信号量与 HTTP 传输层。
func setZenConfig(c *zenConfigData) {
	c.NormalizeForSave(zenAPIBase)
	zenConfigMu.Lock()
	zenConfig = c
	zenConfigMu.Unlock()

	if err := config.SaveZen(resolveDataPath(config.ZenPath), c); err != nil {
		log.Printf("zen config save failed: %v", err)
	}
	// 关闭故障转移时立即解除已武装的窗口：否则 UI 仍显示“故障转移中”，
	// 反向降级 clineFailoverToZen 也会被旧窗口挡住直到超时。
	if !c.Failover {
		zenStateMu.Lock()
		zenFailCount = 0
		zenFailUntil = time.Time{}
		zenStateMu.Unlock()
	}
	rebuildZenTransport()
	rebuildZenSem()
}

// ============ 限流防御状态机 ============

var (
	zenSem       chan struct{} // 并发信号量（防上游瞬时超限）
	zenFailCount int           // 连续失败计数
	zenFailUntil time.Time     // 故障转移截止时间
	zenStateMu   sync.Mutex
)

func rebuildZenSem() {
	n := getZenConfig().MaxConcurrency
	if n <= 0 {
		n = 8
	}
	zenStateMu.Lock()
	zenSem = make(chan struct{}, n)
	zenStateMu.Unlock()
}

func markZenSuccess() {
	zenStateMu.Lock()
	zenFailCount = 0
	zenFailUntil = time.Time{}
	zenStateMu.Unlock()
}

func markZenFail() {
	cfg := getZenConfig()
	// 故障转移关闭时不计数也不报警：否则会打出 “routing to cline pool” 的
	// 误导性日志，而实际路由（routeModel）与降级（zenFailoverToCline）都会跳过。
	// 同时清掉已武装的窗口，确保 UI 的 “故障转移中” 状态立即消失。
	if !cfg.Failover {
		zenStateMu.Lock()
		zenFailCount = 0
		zenFailUntil = time.Time{}
		zenStateMu.Unlock()
		return
	}
	thr := cfg.FailoverCount
	if thr <= 0 {
		thr = 3
	}
	window := cfg.FailoverMinutes
	if window <= 0 {
		window = 5
	}
	zenStateMu.Lock()
	zenFailCount++
	if zenFailCount >= thr {
		zenFailUntil = time.Now().Add(time.Duration(window) * time.Minute)
		log.Printf("  zen failover armed: %d consecutive failures, routing to cline pool for %dm", zenFailCount, window)
	}
	zenStateMu.Unlock()
}

// zenFailedNow 当前是否处于故障转移窗口内。
func zenFailedNow() bool {
	zenStateMu.Lock()
	defer zenStateMu.Unlock()
	if zenFailUntil.IsZero() {
		return false
	}
	if time.Now().After(zenFailUntil) {
		zenFailCount = 0
		zenFailUntil = time.Time{}
		return false
	}
	return true
}

// isRateLimited 限流信号识别：429/503 直接命中；502/403 按错误体关键词。
func isRateLimited(status int, body string) bool {
	return dispatch.IsRateLimited(status, body)
}

// parseRetryAfter parses Retry-After (seconds or HTTP date); invalid input is 0.
func parseRetryAfter(h string) time.Duration {
	return dispatch.ParseRetryAfter(h)
}

// validateProxyList 校验代理列表格式：支持 http/https/socks5/socks5h，必须含 host:port。
func validateProxyList(proxies []string) error {
	return transport.ValidateProxyList(proxies)
}

// ============ 客户端身份 ============
// 规范会话格式（ses_ + 12 位 hex 时间戳 + 14 位 base62）自 2026-09-16 起
// 被免费层强校验，其他形态一律 403 FreeTierError（参考 opencode2api 的实测结论）。
// 会话按对话内容稳定派生：同一会话复用同一 upstream session，保留 prompt-cache 亲和；
// request-id 每次随机，规避请求维度的限流记账。
// 格式对齐官方客户端（sst/opencode v1.18.x）request.ts：
//   User-Agent:          opencode/<version>
//   x-opencode-project:  "global"（官方无仓库场景的静态 project id）
//   x-opencode-session:  "ses_" + 26 位标识（6 位时间 hex + 14 位 base62）
//   x-opencode-request:  "msg" + 26 位标识（消息 id）
//   x-opencode-client:   "cli"

// zenClientVersion tracks the opencode release used by the client identity.
const zenClientVersion = zenupstream.ClientVersion

// zenProviderUtilsVersions maps protocols to ai-sdk provider-utils versions.
var zenProviderUtilsVersions = zenupstream.ProviderUtilsVersions

var zenIDs = zenupstream.NewIdentifier()

// opencodeIdentifier generates an opencode-shaped identifier.
func opencodeIdentifier(prefix string, descending bool) string {
	return zenIDs.New(prefix, descending)
}

func randIntn(n int) int {
	return zenupstream.RandIntn(n)
}

// withRetryJitter adds 0-25% jitter to a backoff delay.
func withRetryJitter(delay time.Duration) time.Duration {
	return zenupstream.WithRetryJitter(delay)
}

// zenConversationSeed extracts a stable conversation seed.
func zenConversationSeed(params map[string]any) string {
	return zenupstream.ConversationSeed(params)
}

var zenSessions = zenupstream.NewSessionCache()

// zenSessionID returns a conversation-stable opencode-shaped session ID.
func zenSessionID(params map[string]any) string {
	return zenSessions.SessionID(params)
}

// zenSessionLookup maps a seed to a fixed opencode-shaped session.
func zenSessionLookup(seed []byte) string {
	return zenSessions.Lookup(seed)
}

// zenUserAgent returns the opencode User-Agent for a protocol.
func zenUserAgent(proto string) string {
	return zenupstream.UserAgent(proto)
}

// ============ zen 上游调用 ============

// anonymousCoreTools are the tool names expected by the anonymous free tier.
var anonymousCoreTools = zenupstream.AnonymousCoreTools

// ensureAnonymousTools tops up missing core tool definitions.
func ensureAnonymousTools(body map[string]any) {
	zenupstream.EnsureAnonymousTools(body)
}

func anonymousToolset(names []string) []any {
	return zenupstream.AnonymousToolset(names)
}

// buildZenBody constructs a zen chat request body.
func buildZenBody(params map[string]any, stream bool, anonymous bool) map[string]any {
	return zenupstream.BuildChatBody(params, stream, anonymous, zenupstream.BodyOptions{
		PassThroughKeys: passThroughKeys,
		ResolveModel: func(model string) (string, bool) {
			if m, ok := resolveZenInfo(model); ok {
				return m.ID, true
			}
			return "", false
		},
	})
}

// callZenAPI 调用 zen 上游：并发信号量 + 指数退避重试 + 代理冷却 + 故障转移计数。
// 匿名（public key）免费层自 2026-09 起只接受 agent 形态的流式请求：
// 上游强制 stream:true，下游要 JSON 时把 SSE 折叠回单个 chat.completion 响应。
// 返回的响应由调用方关闭。
func callZenAPI(params map[string]any, stream bool) (*http.Response, error) {
	cfg := getZenConfig()
	anonymous := cfg.Key == "public"
	// muse-spark 系走 OpenAI Responses 协议（/responses），其余走 chat/completions；
	// 两条路径统一在返回前转回 chat 协议，调用方无感
	useResponses := zenUpstreamNeedsResponses(params)
	protoName := "chat"
	var bodyJSON []byte
	var err error
	if useResponses {
		protoName = "responses"
		bodyJSON, err = json.Marshal(buildZenResponsesBody(params))
	} else {
		bodyJSON, err = json.Marshal(buildZenBody(params, stream, anonymous))
	}
	if err != nil {
		return nil, fmt.Errorf("marshal zen body: %w", err)
	}
	zc := zenupstream.Client{
		HTTP:     getZenHTTPClient(),
		BaseURL:  cfg.BaseURL,
		APIKey:   cfg.Key,
		Headers:  cfg.ZenHeaders,
		IDs:      zenIDs,
		Sessions: zenSessions,
	}

	zenStateMu.Lock()
	sem := zenSem
	if sem == nil {
		rebuildZenSemLocked()
		sem = zenSem
	}
	zenStateMu.Unlock()
	sem <- struct{}{}
	defer func() { <-sem }()

	retries := cfg.Retries
	if retries <= 0 {
		retries = 3
	}
	delay := time.Second

	for attempt := 0; ; attempt++ {
		// 响应头看门狗：黑洞场景（TCP 通、握手/响应头静默丢弃）请求会永久挂起，
		// 且 callZenAPI 不返回则 markZenFail 不触发、故障转移永远无法激活。
		// 60s 内未收到响应头则取消本次尝试（计为网络错误、走重试/故障转移）；
		// 响应头到达后立即停掉看门狗，流式传输时长不受限制。
		watchCtx, watchCancel := context.WithCancel(context.Background())
		watchdog := time.AfterFunc(zenHeaderWatchdogTimeout, watchCancel)

		modelID := ""
		if model, _ := params["model"].(string); model != "" {
			if m, ok := resolveZenInfo(model); ok {
				modelID = m.ID
			}
		}
		req, err := zc.BuildRequest(watchCtx, protoName, bodyJSON, params, modelID)
		if err != nil {
			watchdog.Stop()
			watchCancel()
			return nil, fmt.Errorf("create zen request: %w", err)
		}
		sess := req.Header.Get("x-opencode-session")
		log.Printf("  zen upstream: model=%v proto=%s stream=%v(下游=%v) msgs=%d via=%s attempt=%d session=%s",
			bodyParamsModel(params), protoName, anonymous, stream, getMsgCount(params), describeZenProxy(), attempt+1, truncate(sess, 30))

		resp, err := zc.HTTP.Do(req)
		if err != nil {
			watchdog.Stop()
			watchCancel()
			// 网络错误：退避重试；重试耗尽计一次失败（网络类故障也参与故障转移，
			// 持续网络不可达时才会切 Cline 池）
			if attempt < retries {
				log.Printf("  zen network error (%v), retry %d/%d after %v", err, attempt+1, retries, delay)
				time.Sleep(withRetryJitter(delay))
				delay *= 2
				continue
			}
			markZenFail()
			msg := fmt.Errorf("zen request: %w", err)
			if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "handshake") ||
				strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "unreachable") ||
				strings.Contains(err.Error(), "context canceled") {
				return nil, fmt.Errorf("%w（opencode.ai 网络不可达，可在管理页「上游服务 → opencode 出口代理」配置代理）", msg)
			}
			return nil, msg
		}
		watchdog.Stop()
		if resp.StatusCode == http.StatusOK {
			markZenSuccess()
			if useResponses {
				// Responses 上游统一流式：下游要 SSE 时转写成 chat chunk，要 JSON 时折叠
				if stream {
					return streamZenResponsesAsChat(resp, zenUpstreamModelID(params), watchCancel), nil
				}
				return collapseZenResponsesStream(resp, zenUpstreamModelID(params), watchCancel)
			}
			if anonymous && !stream {
				// 折叠路径内部会关闭 body（连带释放 watchCtx）
				return collapseZenStreamResponse(resp, bodyParamsModel(params))
			}
			return withCancelOnClose(resp, watchCancel), nil
		}

		watchCancel()
		bodyBytes := readAllLimited(resp.Body, 64<<10)
		resp.Body.Close()
		reason := fmt.Sprintf("zen API %d: %s", resp.StatusCode, truncate(string(bodyBytes), 500))

		// 上游明确报「模型不存在」时清理下架残留（同步标记 Delisted 保留的模型）
		if model := bodyParamsModel(params); model != "" && isModelGoneError(resp.StatusCode, string(bodyBytes)) {
			markModelGone(model)
		}

		// 上游 500/502/504 多为瞬时故障，退避重试（503 走限流分支）
		if resp.StatusCode == 500 || resp.StatusCode == 502 || resp.StatusCode == 504 {
			if attempt < retries {
				log.Printf("  zen server error (%d), retry %d/%d after %v", resp.StatusCode, attempt+1, retries, delay)
				time.Sleep(withRetryJitter(delay))
				delay *= 2
				continue
			}
			markZenFail()
			return nil, fmt.Errorf("%s", reason)
		}

		if isRateLimited(resp.StatusCode, string(bodyBytes)) {
			ra := parseRetryAfter(resp.Header.Get("Retry-After"))
			// 冷却当前出口代理（Retry-After 优先，默认 10 分钟，封顶 30 分钟）：
			// 单个本地代理端口背后可切换节点（出口 IP 变化），长冷却有害无益
			if idx := lastZenProxyIdx(); idx >= 0 {
				d := ra
				if d <= 0 {
					d = 10 * time.Minute
				}
				if d > zenMaxProxyCooldown {
					d = zenMaxProxyCooldown
				}
				cooldownZenProxy(idx, d)
			}
			// 短限流（≤60s）值得按 Retry-After 等待重试；
			// 长限流（实测可达 13h）重试无意义，立即失败触发故障转移
			if attempt < retries && ra > 0 && ra <= zenMaxRetryWait {
				log.Printf("  zen rate limited (%d), retry %d/%d after %v", resp.StatusCode, attempt+1, retries, ra)
				time.Sleep(withRetryJitter(ra))
				delay *= 2
				continue
			}
			if attempt < retries && ra <= 0 {
				log.Printf("  zen rate limited (%d), retry %d/%d after %v", resp.StatusCode, attempt+1, retries, delay)
				time.Sleep(withRetryJitter(delay))
				delay *= 2
				continue
			}
			markZenFail()
			if ra > 10*time.Minute {
				return nil, fmt.Errorf("%s（opencode 免费层出口 IP 限流 ~%s，切换代理节点后可立即重试）", reason, ra.Truncate(time.Minute))
			}
			return nil, fmt.Errorf("%s", reason)
		}

		markZenFail()
		return nil, fmt.Errorf("%s", reason)
	}
}

func bodyParamsModel(params map[string]any) string {
	m, _ := params["model"].(string)
	return m
}

// collapseZenStreamResponse 消费匿名免费层强制返回的 SSE 流，
// 折叠成等价的单个 chat.completion JSON 响应（下游要 JSON 时保持透明）。
func collapseZenStreamResponse(resp *http.Response, model string) (*http.Response, error) {
	defer resp.Body.Close()
	acc := &zenCollapseAcc{Model: model, Created: time.Now().Unix()}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[5:])
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Created int64  `json:"created"`
			Choices []struct {
				FinishReason any `json:"finish_reason"`
				Delta        struct {
					Content          string `json:"content"`
					ReasoningContent any    `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Name     string `json:"name"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage map[string]any `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.ID != "" {
			acc.ID = chunk.ID
		}
		if chunk.Model != "" {
			acc.Model = chunk.Model
		}
		if chunk.Created != 0 {
			acc.Created = chunk.Created
		}
		if len(chunk.Choices) > 0 {
			choice := chunk.Choices[0]
			acc.Content += choice.Delta.Content
			if rc, ok := choice.Delta.ReasoningContent.(string); ok {
				acc.Reasoning += rc
			}
			if choice.FinishReason != nil {
				switch v := choice.FinishReason.(type) {
				case string:
					acc.FinishReason = v
				}
			}
			for _, tc := range choice.Delta.ToolCalls {
				for len(acc.ToolCalls) <= tc.Index {
					acc.ToolCalls = append(acc.ToolCalls, zenCollapseTool{})
				}
				t := &acc.ToolCalls[tc.Index]
				if tc.ID != "" {
					t.ID = tc.ID
				}
				if tc.Function.Name != "" {
					t.Name += tc.Function.Name
				}
				t.Arguments += tc.Function.Arguments
			}
		}
		if chunk.Usage != nil {
			acc.Usage = mergeTokenUsage(acc.Usage, parseTokenUsage(chunk.Usage))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("zen stream read: %w", err)
	}
	log.Printf("  zen collapse: stream -> json (content_len=%d tool_calls=%d)", len(acc.Content), len(acc.ToolCalls))
	return zenCollapseToChatResponse(acc, resp.Request)
}

// readAllLimited 读取响应体，最多 limit 字节（防御异常大的错误页）。
func readAllLimited(r io.Reader, limit int64) []byte {
	data, _ := io.ReadAll(io.LimitReader(r, limit))
	return data
}

func rebuildZenSemLocked() {
	n := zenConfig.MaxConcurrency
	if n <= 0 {
		n = 8
	}
	zenSem = make(chan struct{}, n)
}

func describeZenProxy() string {
	cfg := getZenConfig()
	if len(cfg.Proxies) == 0 {
		return "direct"
	}
	idx := lastZenProxyIdx()
	if idx < 0 {
		idx = 0
	}
	idx %= len(cfg.Proxies)
	return fmt.Sprintf("proxy[%d]=%s", idx+1, truncate(maskProxyURL(cfg.Proxies[idx]), 60))
}

// ============ 模型同步 ============

// syncZenModels 拉取 zen 官方 /models 并全量替换 pool 中 Source=="zen" 条目：
// 计算新增/移除清单 —— 官方下架的模型自动从列表消失，不留僵尸条目。
// 自定义模型（Custom=true 或其他 Source）不受影响。
func syncZenModels() modelSyncResult {
	res := modelSyncResult{SyncedAt: time.Now().Format(time.RFC3339)}
	fail := func(err error) modelSyncResult {
		msg := err.Error()
		// 网络类错误给出代理配置提示（opencode.ai 被网络封锁时直连必然失败）
		if strings.Contains(msg, "timeout") || strings.Contains(msg, "handshake") ||
			strings.Contains(msg, "connection refused") || strings.Contains(msg, "unreachable") ||
			strings.Contains(msg, "connectex") {
			msg += "（opencode.ai 当前网络不可达，可在管理页「上游服务 → opencode 出口代理」配置代理后重试）"
		}
		log.Printf("zen models sync failed: %v", msg)
		res.Error = msg
		return res
	}

	cfg := getZenConfig()
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/models"
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return fail(err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Key)
	req.Header.Set("User-Agent", "opencode/"+zenClientVersion)
	req.Header.Set("x-opencode-project", "global")
	req.Header.Set("x-opencode-session", opencodeIdentifier("ses", true))
	req.Header.Set("x-opencode-client", "cli")
	// 与 chat 同路：zen 代理池 + uTLS 指纹传输层，25s 超时
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := getZenHTTPClient().Do(req)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("models API returned status %d", resp.StatusCode))
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fail(err)
	}

	// 组装远程 zen 模型（去重；计费按 -free 后缀或种子白名单判定）
	seedFree := make(map[string]bool, len(zenSeedModels))
	for _, sm := range zenSeedModels {
		seedFree[sm.ID] = true
	}
	seen := make(map[string]bool)
	var remote []Model
	for _, item := range payload.Data {
		id := item.ID
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		cost := "pass"
		if strings.HasSuffix(id, "-free") || seedFree[id] {
			cost = "free"
		}
		remote = append(remote, Model{
			ID:       id,
			Provider: "opencode",
			Cost:     cost,
			Status:   "active",
			Custom:   false,
			Source:   "zen",
		})
	}
	if len(remote) == 0 {
		return fail(fmt.Errorf("models API returned empty list"))
	}

	// 补全上下文信息：远程接口不带 context/output。
	// 用户在管理页锁定过的条目（MetaLocked）保留原值；
	// 其余按种子表刷新（种子值更新时旧条目自动跟进），新模型回退默认。
	p := loadPool()
	oldZen := make(map[string]Model, len(p.Models))
	for _, m := range p.Models {
		if m.Source == "zen" {
			oldZen[m.ID] = m
		}
	}
	fillMeta := func(m Model) Model {
		if om, ok := oldZen[m.ID]; ok {
			// 用户手动停用的模型在重建条目时保留标记，避免同步冲掉停用状态。
			// 能力元数据（thinking/modalities）上游不提供，同样保留旧值。
			m.Disabled = om.Disabled
			m.ThinkingLevels = om.ThinkingLevels
			m.InputModalities = om.InputModalities
			m.OutputModalities = om.OutputModalities
			if om.MetaLocked && om.Context > 0 {
				m.Context, m.Output = om.Context, om.Output
				m.MetaLocked = true
				return m
			}
		}
		for _, sm := range zenSeedModels {
			if sm.ID == m.ID {
				m.Context, m.Output = sm.Context, sm.Output
				return m
			}
		}
		if m.Context == 0 {
			m.Context = 200000
		}
		if m.Output == 0 {
			m.Output = 32768
		}
		return m
	}
	for i := range remote {
		remote[i] = fillMeta(remote[i])
	}

	poolMu.Lock()
	oldIDs := make(map[string]bool)
	var kept []Model
	for _, m := range p.Models {
		if m.Source == "zen" {
			oldIDs[m.ID] = true
			continue
		}
		kept = append(kept, m)
	}
	for _, m := range remote {
		if !oldIDs[m.ID] {
			res.Added = append(res.Added, m.ID)
		}
	}
	// 官方列表里消失的旧模型不删除：实测列表移除后模型往往仍可继续用
	// （如 z-ai/glm-5.3-flash），只打上 Delisted 标记（管理页显示「已下架」，
	// 支持手动移除）；重新出现时新条目天然无标记，标记自动清除。
	// res.Removed 只记录新下架的。
	for _, m := range p.Models {
		if m.Source == "zen" && !seen[m.ID] {
			if !m.Delisted {
				res.Removed = append(res.Removed, m.ID)
			}
			m.Delisted = true
			kept = append(kept, m)
		}
	}
	kept = append(kept, remote...)
	p.Models = kept
	res.Total = len(remote)
	res.Changed = len(res.Added) > 0 || len(res.Removed) > 0
	poolMu.Unlock()
	savePool()

	remoteZenEnabledMu.Lock()
	remoteZenEnabled = true
	remoteZenEnabledMu.Unlock()

	log.Printf("zen models sync: %d models, +%d added, -%d removed", res.Total, len(res.Added), len(res.Removed))
	return res
}

// startZenModelsRefresher 启动定时同步（10 分钟一次，不阻塞启动）。
func startZenModelsRefresher() {
	go func() {
		time.Sleep(2 * time.Second) // 错开启动高峰
		if cfg := getZenConfig(); cfg.Enabled {
			setLastZenModelSync(syncZenModels())
		}
		ticker := time.NewTicker(zenModelSyncInterval)
		defer ticker.Stop()
		for range ticker.C {
			if cfg := getZenConfig(); cfg.Enabled {
				setLastZenModelSync(syncZenModels())
			}
		}
	}()
}

// ============ 最近一次同步结果（管理后台展示） ============

var (
	lastZenSync    modelSyncResult
	lastZenSyncRan bool
	lastZenSyncMu  sync.Mutex
)

func setLastZenModelSync(res modelSyncResult) {
	lastZenSyncMu.Lock()
	lastZenSync = res
	lastZenSyncRan = true
	lastZenSyncMu.Unlock()
}

func lastZenModelSync() modelSyncResult {
	lastZenSyncMu.Lock()
	defer lastZenSyncMu.Unlock()
	if !lastZenSyncRan {
		return modelSyncResult{SyncedAt: ""}
	}
	return lastZenSync
}

// opencodeUsageToday 从请求日志聚合今日 opencode 上游用量（后台仪表盘卡片用）。
func opencodeUsageToday() map[string]any {
	requestLogsMu.Lock()
	defer requestLogsMu.Unlock()

	var requests int64
	var input, output, total int64
	today := time.Now().Format("2006-01-02")
	for _, e := range requestLogs {
		if e.Upstream != upstreamOpenCode || e.StartedAt.Format("2006-01-02") != today {
			continue
		}
		requests++
		input += e.InputTokens
		output += e.OutputTokens
		total += e.TotalTokens
	}
	return map[string]any{
		"requests":     requests,
		"inputTokens":  input,
		"outputTokens": output,
		"totalTokens":  total,
	}
}

// ============ Responses 协议上游（muse-spark 系） ============
// 对齐 opencode 官方端点表（opencode.ai/docs/zen）：GPT/Grok/Muse Spark 走
// OpenAI Responses 协议（POST /responses），Claude/Qwen → Anthropic，其余 →
// chat/completions。当前免费层只有 muse-spark 系命中 Responses；
// 参考实现：sub2api DefaultOpenCodeZenProtocolRules。
// 上游统一流式（Responses SSE），下游要 chat SSE 时逐事件转写，要 JSON 时折叠。

// zenResponsesModelPrefixes 需要 Responses 协议的 zen 模型前缀（gpt-* / grok-* 预留）。
var zenResponsesModelPrefixes = []string{"muse-spark-"}

func zenModelUsesResponses(modelID string) bool {
	for _, p := range zenResponsesModelPrefixes {
		if strings.HasPrefix(modelID, p) {
			return true
		}
	}
	return false
}

// zenUpstreamModelID 返回请求模型对应的 zen 正式 ID（未解析到时去掉 opencode/ 前缀）。
func zenUpstreamModelID(params map[string]any) string {
	m, _ := params["model"].(string)
	if m == "" {
		return m
	}
	if zm, ok := resolveZenInfo(m); ok {
		return zm.ID
	}
	return strings.TrimPrefix(m, "opencode/")
}

func zenUpstreamNeedsResponses(params map[string]any) bool {
	return zenModelUsesResponses(zenUpstreamModelID(params))
}

// buildZenResponsesBody 把下游 chat 参数构造成 Responses 请求体。
// 匿名免费层的 agent 形态要求（强制流式 + 核心工具）同样适用于该协议。
func buildZenResponsesBody(params map[string]any) map[string]any {
	body := map[string]any{"stream": true}
	body["model"] = zenUpstreamModelID(params)
	// max_tokens → max_output_tokens；Meta 系上游要求 >= 16，推理模型预算太小
	// 会全耗在 reasoning 上产出空内容，低于下限兜到默认值（同 buildUpstreamBody 约定）。
	// 客户端未指定时不带该字段，交由上游取模型默认。
	// chat 请求经 json.Unmarshal 是 float64，responses/anthropic 转换路径是 int，宽松读取。
	for _, k := range []string{"max_tokens", "max_completion_tokens"} {
		n := zenInt64(params[k])
		if n <= 0 {
			continue
		}
		if n < minUpstreamMaxTokens {
			log.Printf("  zen clamp max_output_tokens=%d -> %d (upstream requires >= %d)", n, defaultMaxTokens, minUpstreamMaxTokens)
			n = defaultMaxTokens
		}
		body["max_output_tokens"] = n
		break
	}
	if v, ok := params["temperature"]; ok {
		body["temperature"] = v
	}
	if v, ok := params["top_p"]; ok {
		body["top_p"] = v
	}

	// tools：先在 chat 嵌套形态上补齐核心工具（免费层 agent 形态校验对
	// 带 key 的请求同样生效），再转 Responses 扁平格式
	pseudo := map[string]any{}
	if raw, ok := params["tools"]; ok {
		pseudo["tools"] = raw
	}
	ensureAnonymousTools(pseudo)
	if rawTools, ok := pseudo["tools"].([]any); ok && len(rawTools) > 0 {
		flat := make([]any, 0, len(rawTools))
		for _, t := range rawTools {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			fn, _ := tm["function"].(map[string]any)
			if fn == nil {
				continue
			}
			// description 仅在非空字符串时下发、parameters 仅在非 null 时下发：
			// 上游网关对 "description": null / "parameters": null 硬拒 400
			entry := map[string]any{"type": "function", "name": fn["name"]}
			if d, _ := fn["description"].(string); d != "" {
				entry["description"] = d
			}
			if fn["parameters"] != nil {
				entry["parameters"] = fn["parameters"]
			}
			flat = append(flat, entry)
		}
		if len(flat) > 0 {
			body["tools"] = flat
		}
	}
	switch tc := params["tool_choice"].(type) {
	case string:
		body["tool_choice"] = tc
	case map[string]any:
		fn, _ := tc["function"].(map[string]any)
		if name, _ := fn["name"].(string); name != "" {
			body["tool_choice"] = map[string]any{"type": "function", "name": name}
		}
	}

	// messages → instructions（system）+ input（对话与工具往返）
	var instr []string
	input := make([]any, 0, 16)
	if msgs, ok := params["messages"].([]any); ok {
		for _, m := range sanitizeMessages(msgs) {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			switch role, _ := mm["role"].(string); role {
			case "system", "developer":
				if s := stringifyResponsesContent(mm["content"]); s != "" {
					instr = append(instr, s)
				}
			case "tool":
				input = append(input, map[string]any{
					"type":    "function_call_output",
					"call_id": mm["tool_call_id"],
					"output":  stringifyResponsesContent(mm["content"]),
				})
			case "assistant":
				if tcs, ok := mm["tool_calls"].([]any); ok {
					for _, tc := range tcs {
						cm, ok := tc.(map[string]any)
						if !ok {
							continue
						}
						fn, _ := cm["function"].(map[string]any)
						if fn == nil {
							continue
						}
						args, _ := fn["arguments"].(string)
						if args == "" {
							if am, ok := fn["arguments"].(map[string]any); ok {
								if b, err := json.Marshal(am); err == nil {
									args = string(b)
								}
							}
						}
						input = append(input, map[string]any{
							"type":      "function_call",
							"call_id":   cm["id"],
							"name":      fn["name"],
							"arguments": args,
						})
					}
				}
				if s := stringifyResponsesContent(mm["content"]); s != "" {
					input = append(input, map[string]any{"type": "message", "role": "assistant", "content": s})
				}
			default: // user
				input = append(input, map[string]any{
					"type":    "message",
					"role":    "user",
					"content": zenChatContentToResponsesInput(mm["content"]),
				})
			}
		}
	}
	if len(instr) > 0 {
		body["instructions"] = strings.Join(instr, "\n\n")
	}
	body["input"] = input
	// 定稿前清洗 tools（扁平形态）：null 可选字段会被上游网关硬拒 400
	sanitizeToolsForUpstream(body)
	return body
}

// zenChatContentToResponsesInput 把 chat content 转成 Responses 输入内容：
// 字符串原样；部件数组映射 text → input_text、image_url → input_image。
func zenChatContentToResponsesInput(content any) any {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := make([]any, 0, len(v))
		for _, p := range v {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			switch pm["type"] {
			case "text":
				if t, _ := pm["text"].(string); t != "" {
					parts = append(parts, map[string]any{"type": "input_text", "text": t})
				}
			case "image_url":
				iu, _ := pm["image_url"].(map[string]any)
				if u, _ := iu["url"].(string); u != "" {
					parts = append(parts, map[string]any{"type": "input_image", "image_url": u})
				}
			}
		}
		if len(parts) > 0 {
			return parts
		}
	}
	return ""
}

// zenInt64 coerces a JSON-decoded numeric value to int64.
func zenInt64(v any) int64 { return zenupstream.Int64(v) }

// zenCollapseAcc / zenCollapseTool are the shared collapsed-response shapes.
type zenCollapseAcc = zenupstream.CollapseAcc
type zenCollapseTool = zenupstream.CollapseTool

// zenCollapseToChatResponse renders a collapsed accumulator as one chat.completion.
func zenCollapseToChatResponse(acc *zenCollapseAcc, req *http.Request) (*http.Response, error) {
	return zenupstream.CollapseToChatResponse(acc, req)
}

// streamZenResponsesAsChat rewrites upstream Responses SSE into chat chunk SSE.
func streamZenResponsesAsChat(resp *http.Response, model string, cancel context.CancelFunc) *http.Response {
	return zenupstream.StreamResponsesAsChat(resp, model, cancel)
}

// collapseZenResponsesStream folds an upstream Responses SSE into one JSON response.
func collapseZenResponsesStream(resp *http.Response, model string, cancel context.CancelFunc) (*http.Response, error) {
	return zenupstream.CollapseResponsesStream(resp, model, cancel)
}
