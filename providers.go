package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"cline-go-proxy/internal/dispatch"
	"cline-go-proxy/internal/providerreg"
	"cline-go-proxy/internal/store/providers"
	"cline-go-proxy/internal/transport"
	"cline-go-proxy/internal/upstream/openaicompat"
)

// ============================================================================
// 自定义 OpenAI 兼容 Provider
// 用户可接入任意 OpenAI 兼容上游（OpenRouter / Groq / Cerebras / Gemini 兼容层 /
// Mistral / 自建 vLLM 等）。每个 provider 声明 baseURL、apiKey、自定义请求头、
// 模型列表；请求按模型归属路由，失败后按 回退链 → 自动兜底 降级。
// providerRegistry 内存态 + 落盘（.cline-providers.json）。
var (
	providersMu            sync.Mutex
	customProviders        []*CustomProvider
	providersLoaded        bool
	providersFilePath      string
	providersCooldownsPath string
	// modelCooldownsPerProvider providerID:model → 冷却截止
	providerCooldowns = map[string]time.Time{}
)

func init() {
	providersFilePath = resolveDataPath(".cline-providers.json")
	providersCooldownsPath = resolveDataPath(".cline-providers-cooldowns.json")
}

func loadProviders() {
	providersMu.Lock()
	defer providersMu.Unlock()
	if providersLoaded {
		return
	}
	providersLoaded = true
	// 冷却表在 registry 前加载：无论 provider 文件是否存在都要重启冷却记忆
	loadProviderCooldownsLocked()
	list, err := providers.Load(providersFilePath)
	if err != nil {
		backupCorruptFile(providersFilePath, err)
		return
	}
	customProviders = list
}

func saveProvidersLocked() {
	if err := providers.Save(providersFilePath, customProviders); err != nil {
		log.Printf("providers save failed: %v", err)
	}
}

// listProviders 返回副本列表（按 Priority, CreatedAt 排序）。
func listProviders() []CustomProvider {
	loadProviders()
	providersMu.Lock()
	defer providersMu.Unlock()
	out := make([]CustomProvider, 0, len(customProviders))
	for _, p := range customProviders {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func genProviderID() string {
	return "prov_" + fmt.Sprintf("%x", time.Now().UnixNano())
}

// upsertProvider 新增或更新（按 ID）。返回最终 provider。
func upsertProvider(p *CustomProvider) CustomProvider {
	loadProviders()
	providersMu.Lock()
	defer providersMu.Unlock()
	if p.ID == "" {
		p.ID = genProviderID()
		p.CreatedAt = time.Now()
		found := false
		for _, e := range customProviders {
			if e.ID == p.ID {
				*e = *p
				found = true
				break
			}
		}
		if !found {
			customProviders = append(customProviders, p)
		}
	} else {
		found := false
		for _, e := range customProviders {
			if e.ID == p.ID {
				*e = *p
				found = true
				break
			}
		}
		if !found {
			p.CreatedAt = time.Now()
			customProviders = append(customProviders, p)
		}
	}
	saveProvidersLocked()
	return *p
}

func deleteProvider(id string) bool {
	loadProviders()
	providersMu.Lock()
	defer providersMu.Unlock()
	for i, p := range customProviders {
		if p.ID == id {
			customProviders = append(customProviders[:i], customProviders[i+1:]...)
			saveProvidersLocked()
			// 冷却键是 "<providerID>:<model>"，删除 provider 时要连同全部
			// 模型冷却一起清掉；同时保留裸 id 删除以兼容历史残留键。
			delete(providerCooldowns, id)
			for k := range providerCooldowns {
				if strings.HasPrefix(k, id+":") {
					delete(providerCooldowns, k)
				}
			}
			// 领到的粘贴会话不要再粘到已删除的 provider 上
			stickyClearForProvider(id)
			saveProviderCooldownsLocked()
			return true
		}
	}
	return false
}

// resolveProvidersForModel 找到该模型所有可用 provider（启用中、未冷却），按
// Priority 升序（quality 轮询策略下改为实测成绩优先）返回**指针副本**，调用
// 方在锁外使用。同一个模型配置了多个 provider 时形成冗余组：首选失败可立即
// 换组内下一家重试，全部失败才降级到回退链上的其他模型。冷却中与预算超限的
// provider 不入候选，路由在派发前就避开配额墙。
func resolveProvidersForModel(model string) []*CustomProvider {
	loadProviders()
	providersMu.Lock()
	defer providersMu.Unlock()
	var cs []providerCand
	now := time.Now()
	for _, p := range customProviders {
		if !p.Enabled {
			continue
		}
		for _, m := range p.ModelIDs {
			if m != model {
				continue
			}
			if until, cool := providerCooldowns[p.ID+":"+model]; cool && now.Before(until) {
				break // 该 provider 的该模型冷却中
			}
			// 预算上限：超限的 provider 不入候选，路由在派发前就避开配额墙
			if providerBudgetExceeded(p.ID, model, p.RpmLimit, p.DailyLimit) {
				break
			}
			// Bound connections: provider stays out of candidates only when EVERY
			// pooled connection is missing/expired/pending reauth (fallback chain takes over).
			if len(providerConnIDs(p)) > 0 && !hasUsableConnection(p) {
				break
			}
			cs = append(cs, providerCand{p: *p})
			break
		}
	}
	cs = orderProviderCands(cs, model)
	out := make([]*CustomProvider, 0, len(cs))
	for i := range cs {
		out = append(out, &cs[i].p)
	}
	return out
}

// resolveProviderForModel 找到该模型的首选 provider（启用中、未冷却、优先级最小）。
func resolveProviderForModel(model string) *CustomProvider {
	cs := resolveProvidersForModel(model)
	if len(cs) == 0 {
		return nil
	}
	return cs[0]
}

// providerFallbackModels 该模型的所有 provider 候选按优先级排序的模型链：
// 原模型优先，然后是 modelChain（配置链）中启用了 provider 的模型。
func customProviderServes(model string) bool {
	return resolveProviderForModel(model) != nil
}

// setProviderCooldown 记录 provider 模型冷却（限流/5xx/配置错误时调用）。
// 同步落盘：重启后不再把刚冷却的 provider 当健康源重新探一遍 —— 对 429 这类
// 长冷却（幂重置 EP 违 30m 封顶）尤其必要。
func setProviderCooldown(providerID, model string, until time.Time) {
	providersMu.Lock()
	providerCooldowns[providerID+":"+model] = until
	saveProviderCooldownsLocked()
	providersMu.Unlock()
	stickyClearForProvider(providerID) // 粘贴会话不要粘在冷却中的 provider 上
}

// callProvider 调用自定义 provider 的 /chat/completions。
func callProvider(p *CustomProvider, params map[string]any, stream bool) (*http.Response, error) {
	bodyJSON, err := openaicompat.PrepareBody(params, stream, openaicompat.BodyOptions{
		PassThroughKeys:     passThroughKeys,
		MinMaxTokens:        minUpstreamMaxTokens,
		DefaultMaxTokens:    defaultMaxTokens,
		ModelMaxOutputLimit: modelMaxOutputLimit,
		Logf:                log.Printf,
	})
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(p.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	// Provider overrides get their own cached transport; an empty override
	// keeps reusing the global transport so existing behavior is unchanged.
	tr, err := providerTransportFor(p.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("provider %s: %w", p.Name, err)
	}
	httpCli := &http.Client{Transport: tr, Timeout: timeout}
	pc := &openaicompat.Client{
		HTTP:    httpCli,
		BaseURL: p.BaseURL,
		APIKey:  p.APIKey,
		Headers: p.Headers,
	}

	modelKey := fmt.Sprintf("%v", params["model"])
	log.Printf("  provider upstream: name=%s model=%v stream=%v msgs=%d", p.Name, modelKey, stream, getMsgCount(params))

	// Wave-4 connection pool: walk the usable connections in rotation order.
	// A connection-authenticated 401/403/429 parks that connection and retries
	// in place with the next one BEFORE the outer next-provider loop ever sees
	// a failure. API-key-only providers make exactly one attempt, as before.
	connIDs := providerConnIDs(p)
	conns := usableConnectionsForProvider(p)
	if len(connIDs) > 0 && len(conns) == 0 {
		return nil, fmt.Errorf("provider %s: connection unavailable (expired or reauth required)", p.Name)
	}
	if len(conns) == 0 {
		conns = []*Connection{nil}
	}
	for i, conn := range conns {
		token := ""
		extra := map[string]string{}
		// Bound connection: authenticate with the connection token and inject
		// connection-level dynamic headers (e.g. the Kilo org header).
		if conn != nil {
			spec, hasSpec := providerDeviceFlows[conn.Provider]
			if hasSpec {
				token = connectionBearer(conn, spec)
			} else {
				token = conn.AccessToken
			}
			if hasSpec && spec.OrgHeader != "" {
				if oid := conn.ProviderSpecificData["orgId"]; oid != "" {
					extra[spec.OrgHeader] = oid
				}
			}
		}
		req, err := pc.BuildRequest(bodyJSON, openaicompat.RequestOptions{Token: token, Headers: extra})
		if err != nil {
			return nil, fmt.Errorf("create provider request: %w", err)
		}
		started := time.Now()
		// Count at dispatch: RPM/daily budget measures requests actually sent upstream.
		recordProviderDispatch(p.ID, modelKey)
		resp, err := httpCli.Do(req)
		latency := time.Since(started).Milliseconds()
		if err != nil {
			recordProviderOutcome(p.ID, modelKey, false, latency)
			return nil, fmt.Errorf("provider request: %w", err)
		}
		if resp.StatusCode == http.StatusOK {
			recordProviderOutcome(p.ID, modelKey, true, latency)
			if conn != nil {
				// Consumed a real dispatch: move rotation past this connection.
				advanceConnCursor(p, conn.ID)
			}
			tagResponseConn(resp, conn)
			return resp, nil
		}
		recordProviderOutcome(p.ID, modelKey, false, latency)
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		resp.Body.Close()
		bodyStr := string(bodyBytes)
		// Connection-specific failure with siblings left in the pool:
		// park this connection (401/403 -> reauth, 429 -> timed cooldown)
		// and retry with the next connection now instead of failing the provider.
		connRetryable := conn != nil && (resp.StatusCode == http.StatusUnauthorized ||
			resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests)
		if connRetryable && i < len(conns)-1 {
			if resp.StatusCode == http.StatusTooManyRequests {
				setConnectionCooldown(conn.ID, time.Now().Add(connectionRateLimitCooldown))
			} else {
				markConnectionReauth(conn.ID)
			}
			log.Printf("  provider conn failover: name=%s conn=%s status=%d -> next connection", p.Name, conn.ID, resp.StatusCode)
			continue
		}
		// Final failure: original marking semantics.
		if conn != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			markConnectionReauth(conn.ID)
		}
		if until := providerCooldownUntil(resp.StatusCode, resp.Header.Get("Retry-After"), bodyStr); !until.IsZero() {
			setProviderCooldown(p.ID, modelKey, until)
		}
		return nil, &clineAPIError{statusCode: resp.StatusCode, message: truncate(bodyStr, 500)}
	}
	return nil, fmt.Errorf("provider %s: no connection attempts made", p.Name)
}

// providerRateLimitedKeys reports explicit quota/rate-limit keywords.
func providerRateLimitedKeys(body string) bool {
	return dispatch.RateLimitedByBody(body)
}

// providerCooldownUntil classifies an upstream failure into a cooldown window.
// Returning the zero time means no cooldown. Transient failures (408/5xx)
// honor Retry.TransientCooldownSeconds: >0 overrides the built-in window,
// -1 disables the cooldown, 0 keeps dispatch's default classification.
func providerCooldownUntil(status int, retryAfterHeader, body string) time.Time {
	until := dispatch.ProviderCooldownUntil(status, retryAfterHeader, body)
	t := getProxyConfig().Retry.TransientCooldownSeconds
	if t == 0 || !isTransientUpstreamStatus(status) {
		return until
	}
	if t < 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(t) * time.Second)
}

func isTransientUpstreamStatus(status int) bool {
	return status == http.StatusRequestTimeout || (status >= 500 && status < 600)
}

// ============================================================================
// Provider 路由加固：冷却落盘 / 粘贴会话 / 实测成绩排序 / 预算上限
// ============================================================================

// ---- 冷却落盘：provider:model → 恢复时间（unixMicro）--------------------

// loadProviderCooldownsLocked 读取冷却文件（与 provider registry 同锁调用），
// 过期条目直接丢弃。文件缺失/损坏只记日志，不阻塞 provider 初始化。
// 调方必须已持有 providersMu。
func loadProviderCooldownsLocked() {
	loaded, err := providers.LoadCooldowns(providersCooldownsPath)
	if err != nil {
		backupCorruptFile(providersCooldownsPath, err)
		return
	}
	for k, until := range loaded {
		providerCooldowns[k] = until
	}
}

// saveProviderCooldownsLocked 把未过期的冷却条目原子写盘。调方必须已持有
// providersMu。写失败只记日志：冷却丢失的最坏后果是重启后多探测一次。
func saveProviderCooldownsLocked() {
	if err := providers.SaveCooldowns(providersCooldownsPath, providerCooldowns); err != nil {
		log.Printf("provider cooldowns save failed: %v", err)
	}
}

// ---- 粘贴会话：同一会话在 TTL 内钉在同一家 provider --------------------

// providerStickyTTL 粘贴会话窗口。会话中途换 provider 是客户端最直接能感知
// 的质量抖动，30 分钟窗口足以覆盖一次连续的编码会话。
const providerStickyTTL = 30 * time.Minute

type stickyPin struct {
	providerID string
	model      string
	until      time.Time // 到期时间，stickyGet 过期即视为未钉
}

var (
	stickyMu       sync.Mutex
	stickySessions = map[string]stickyPin{} // 会话键 → 钉住的 provider:model
)

// sessionKeyFromParams 从请求消息里派生会话键：取第一条 user 消息内容的
// 稳定哈希（sha256 前 16 hex）。agent 会话中首条 user 消息在整个对话过程中
// 不变，可稳定标识一次会话；无 user 消息或取不到内容时返回空串（不钉）。
func sessionKeyFromParams(params map[string]any) string {
	msgs, ok := params["messages"].([]any)
	if !ok {
		return ""
	}
	for _, msg := range msgs {
		m, ok := msg.(map[string]any)
		if !ok || m["role"] != "user" {
			continue
		}
		bb, err := json.Marshal(m["content"])
		if err != nil || len(bb) == 0 {
			return ""
		}
		sum := sha256.Sum256(bb)
		return hex.EncodeToString(sum[:])[:16]
	}
	return ""
}

func stickyGet(sk string) *stickyPin {
	stickyMu.Lock()
	pin, ok := stickySessions[sk]
	if ok && stickyExpired(pin) {
		delete(stickySessions, sk)
		ok = false
	}
	stickyMu.Unlock()
	if !ok {
		return nil
	}
	return &pin
}

// stickyExpired 到期判断（调方须已持锁）。
func stickyExpired(pin stickyPin) bool { return time.Now().After(pin.until) }

// stickySet 钉住/续期：TTL 内每次成功都被刷新，一次编码会话中途不换源。
// 顺带清扫过期条目，map 不随时间无界增长。
func stickySet(sk, providerID, model string) {
	stickyMu.Lock()
	stickySessions[sk] = stickyPin{providerID: providerID, model: model, until: time.Now().Add(providerStickyTTL)}
	for k, pin := range stickySessions {
		if stickyExpired(pin) {
			delete(stickySessions, k)
		}
	}
	stickyMu.Unlock()
}

func stickyClear(sk string) {
	stickyMu.Lock()
	delete(stickySessions, sk)
	stickyMu.Unlock()
}

// stickyClearForProvider 某家 provider 被冷却/删除后，把钉在它身上的会话解放，
// 后续请求立刻融入组内故障转移。
func stickyClearForProvider(providerID string) {
	stickyMu.Lock()
	for k, pin := range stickySessions {
		if pin.providerID == providerID {
			delete(stickySessions, k)
		}
	}
	stickyMu.Unlock()
}

// pinProviderPresent 粘贴会话命中后校验钉住的 provider 仍然可用：启用、
// 服务该模型、未冷却、未超预算。任何一条不满足都返回 nil（随后解钉转入
// 组内故障转移）。
func pinProviderPresent(providerID, model string) *CustomProvider {
	loadProviders()
	providersMu.Lock()
	defer providersMu.Unlock()
	for _, p := range customProviders {
		if p.ID != providerID {
			continue
		}
		if !p.Enabled {
			return nil
		}
		serves := false
		for _, m := range p.ModelIDs {
			if m == model {
				serves = true
				break
			}
		}
		if !serves {
			return nil
		}
		if until, cool := providerCooldowns[providerID+":"+model]; cool && time.Now().Before(until) {
			return nil
		}
		if providerBudgetExceeded(providerID, model, p.RpmLimit, p.DailyLimit) {
			return nil
		}
		cp := *p
		return &cp
	}
	return nil
}

// ---- 实测成绩（quality 策略的排序依据）----------------------------------

const (
	providerScoreWindow    = time.Hour // 成绩窗口：只看最近 1 小时的表现
	providerScoreRingSize  = 24
	providerNeutralSuccess = 0.5 // 无样本时的中性成功率：不打乱配置优先级
)

type providerOutcome struct {
	at    time.Time
	ok    bool
	latMS int64
}

type providerScore struct {
	ring  [providerScoreRingSize]providerOutcome
	idx   int
	count int
}

var (
	scoreMu        sync.Mutex
	providerScores = map[string]*providerScore{} // providerID:model
)

// recordProviderOutcome 记录一次上游调用的成败与时延（成功/失败都要记，
// 成功率才有意义）。数据仅内存持有，重启后从零学起。
func recordProviderOutcome(providerID, model string, ok bool, latMS int64) {
	if providerID == "" {
		return
	}
	scoreMu.Lock()
	s := providerScores[providerID+":"+model]
	if s == nil {
		s = &providerScore{}
		providerScores[providerID+":"+model] = s
	}
	s.ring[s.idx] = providerOutcome{at: time.Now(), ok: ok, latMS: latMS}
	s.idx = (s.idx + 1) % providerScoreRingSize
	if s.count < providerScoreRingSize {
		s.count++
	}
	scoreMu.Unlock()
}

// providerScoreStats 返回窗口内 (成功率, 成功样本平均时延 ms)；无样本返回
// 中性值 (0.5, 0)。只用窗口内样本：早就废弃的历史不拖累当下的判断。
func providerScoreStats(providerID, model string) (float64, float64) {
	scoreMu.Lock()
	defer scoreMu.Unlock()
	s := providerScores[providerID+":"+model]
	if s == nil || s.count == 0 {
		return providerNeutralSuccess, 0
	}
	cut := time.Now().Add(-providerScoreWindow)
	var ok, total int
	var latSum, latN int64
	for _, o := range s.ring {
		if o.at.IsZero() || o.at.Before(cut) {
			continue
		}
		total++
		if o.ok {
			ok++
			latSum += o.latMS
			latN++
		}
	}
	if total == 0 {
		return providerNeutralSuccess, 0
	}
	ratio := float64(ok) / float64(total)
	var latAvg float64
	if latN > 0 {
		latAvg = float64(latSum) / float64(latN)
	}
	return ratio, latAvg
}

// ---- 预算上限：派发前就避开配额墙，别再拿 429 当发现手段 ----------------

var (
	budgetMu      sync.Mutex
	providerRPM   = map[string][]time.Time{}      // providerID:model → 分钟窗口内派发时间
	providerDaily = map[string]map[string]int64{} // providerID → UTC 日 → 计数
)

const providerRPMWindow = time.Minute

// providerBudgetExceeded 判断该 provider:model 是否已超配（超一票都算超）。
// rpmLimit/dailyLimit 任一超限即返回 true；不超过顺带清理过期窗口数据。
func providerBudgetExceeded(providerID, model string, rpmLimit, dailyLimit int) bool {
	if rpmLimit <= 0 && dailyLimit <= 0 {
		return false
	}
	budgetMu.Lock()
	defer budgetMu.Unlock()
	if rpmLimit > 0 {
		key := providerID + ":" + model
		cut := time.Now().Add(-providerRPMWindow)
		kept := providerRPM[key][:0]
		for _, t := range providerRPM[key] {
			if t.After(cut) {
				kept = append(kept, t)
			}
		}
		providerRPM[key] = kept
		if len(kept) >= rpmLimit {
			return true
		}
	}
	if dailyLimit > 0 {
		day := time.Now().UTC().Format("2006-01-02")
		if providerDaily[providerID][day] >= int64(dailyLimit) {
			return true
		}
	}
	return false
}

// recordProviderDispatch 派发即计数（成败都计入：已打出的上游请求都占配额）。
func recordProviderDispatch(providerID, model string) {
	if providerID == "" {
		return
	}
	budgetMu.Lock()
	defer budgetMu.Unlock()
	providerRPM[providerID+":"+model] = append(providerRPM[providerID+":"+model], time.Now())
	day := time.Now().UTC().Format("2006-01-02")
	if providerDaily[providerID] == nil {
		providerDaily[providerID] = map[string]int64{}
	}
	providerDaily[providerID][day]++
}

// ---- 候选排序 --------------------------------------------------------

// providerCand 候选 provider（包级定义，排序函数复用）。
type providerCand struct {
	p CustomProvider
}

// sortProviderCands 候选排序：默认/未知策略按 Priority 升序（并列按创建时间）；
// quality 策略按实测成绩优先 —— 窗口内成功率高者在前，等同程按平均时延升序，
// 仍平按配置优先级。无样本的候选拿中性值，不因无数据而插队压过实测优秀者。
func sortProviderCands(cs []providerCand, model string) {
	if getProxyConfig().Strategy == "quality" {
		sort.SliceStable(cs, func(i, j int) bool {
			ri, li := providerScoreStats(cs[i].p.ID, model)
			rj, lj := providerScoreStats(cs[j].p.ID, model)
			if ri != rj {
				return ri > rj
			}
			if li > 0 && lj > 0 && li != lj {
				return li < lj
			}
			if cs[i].p.Priority != cs[j].p.Priority {
				return cs[i].p.Priority < cs[j].p.Priority
			}
			return cs[i].p.CreatedAt.Before(cs[j].p.CreatedAt)
		})
		return
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].p.Priority != cs[j].p.Priority {
			return cs[i].p.Priority < cs[j].p.Priority
		}
		return cs[i].p.CreatedAt.Before(cs[j].p.CreatedAt)
	})
}

// ---- 加权轮询（smooth weighted round-robin）--------------------------------
//
// weighted_round_robin 策略下，候选按整数权重比例分布，而不是按优先级轮流。
// 权重为 0 视为 1（未设置），负值把候选从该策略排除。每个模型维护一份
// current-weight 状态，请求顺序在长期收敛到权重比例，且同轮内稳定可测。
var (
	providerWRRMu    sync.Mutex
	providerWRRState = map[string]float64{}
)

func effectiveProviderWeight(p CustomProvider) int {
	if p.Weight == 0 {
		return 1
	}
	return p.Weight
}

// orderProviderCandsWeighted 执行一次 smooth weighted round-robin 单选：
// 每个候选 current += weight，选 current 最大者放首位（并列取基线序前者），
// 其 current -= total，其余候选保持基线（优先级）顺序用于故障转移。
// 负权候选被剔除；全部非正时返回 nil（调用方落入回退链）。
func orderProviderCandsWeighted(cs []providerCand, model string) []providerCand {
	remaining := make([]providerCand, 0, len(cs))
	total := 0
	for _, c := range cs {
		w := effectiveProviderWeight(c.p)
		if w <= 0 {
			continue
		}
		total += w
		remaining = append(remaining, c)
	}
	if total == 0 {
		return nil
	}
	sort.SliceStable(remaining, func(i, j int) bool {
		if remaining[i].p.Priority != remaining[j].p.Priority {
			return remaining[i].p.Priority < remaining[j].p.Priority
		}
		return remaining[i].p.CreatedAt.Before(remaining[j].p.CreatedAt)
	})

	providerWRRMu.Lock()
	defer providerWRRMu.Unlock()
	if len(providerWRRState) > 10000 {
		providerWRRState = map[string]float64{}
	}
	best := 0
	var bestVal float64
	for i := range remaining {
		key := remaining[i].p.ID + ":" + model
		providerWRRState[key] += float64(effectiveProviderWeight(remaining[i].p))
		if i == 0 || providerWRRState[key] > bestVal {
			best, bestVal = i, providerWRRState[key]
		}
	}
	providerWRRState[remaining[best].p.ID+":"+model] -= float64(total)

	out := make([]providerCand, 0, len(remaining))
	out = append(out, remaining[best])
	for i := range remaining {
		if i != best {
			out = append(out, remaining[i])
		}
	}
	return out
}

// orderProviderCands 按当前路由策略产出候选顺序；weighted_round_robin 会重排
// 并可能剔除负权候选，其余策略沿用 sortProviderCands 的排序语义。
func orderProviderCands(cs []providerCand, model string) []providerCand {
	if getProxyConfig().Strategy == "weighted_round_robin" {
		return orderProviderCandsWeighted(cs, model)
	}
	sortProviderCands(cs, model)
	return cs
}

// ============================================================================
// Provider 预设（市面常见免费/低成本 OpenAI 兼容上游）
// ============================================================================

type providerPreset = providerreg.Preset

// modelsFetcher 描述 preset 对应上游的模型目录端点（目前仅 openai 兼容格式）。
type modelsFetcher = providerreg.ModelsFetcher

// mergePresets 把 src 合入 dst，冲突时保留手写的运行时 preset。
func mergePresets(dst, src map[string]providerPreset) map[string]providerPreset {
	out := make(map[string]providerPreset, len(dst)+len(src))
	for k, v := range src {
		out[k] = v
	}
	for k, v := range dst {
		out[k] = v
	}
	return out
}

// providerPresets 常见 OpenAI 兼容上游预设。Key 为前端展示名。
var providerPresets = map[string]providerPreset{
	"openrouter": {
		Name:    "OpenRouter",
		BaseURL: "https://openrouter.ai/api/v1",
		Headers: map[string]string{
			"HTTP-Referer": "https://cline.bot",
			"X-Title":      "Cline Proxy",
		},
		Notes:    "300+ models; many :free variants. Key: openrouter.ai/keys",
		FreeTier: true,
	},
	"groq": {
		Name:     "Groq",
		BaseURL:  "https://api.groq.com/openai/v1",
		Notes:    "Fast free tier (Llama/Qwen). Key: console.groq.com/keys",
		FreeTier: true,
	},
	"cerebras": {
		Name:     "Cerebras",
		BaseURL:  "https://api.cerebras.ai/v1",
		Notes:    "Free tier, very high throughput. Key: cloud.cerebras.ai",
		FreeTier: true,
	},
	"gemini": {
		Name:     "Google AI Studio",
		BaseURL:  "https://generativelanguage.googleapis.com/v1beta/openai",
		Notes:    "Gemini free tier via OpenAI-compat layer. Key: aistudio.google.com/apikey",
		FreeTier: true,
	},
	"mistral": {
		Name:     "Mistral (La Plateforme)",
		BaseURL:  "https://api.mistral.ai/v1",
		Notes:    "Free experiment tier. Key: console.mistral.ai",
		FreeTier: true,
	},
	"together": {
		Name:     "Together AI",
		BaseURL:  "https://api.together.xyz/v1",
		Notes:    "Some free models (e.g. Llama Vision free). Key: api.together.ai",
		FreeTier: true,
	},
	"deepseek": {
		Name:    "DeepSeek",
		BaseURL: "https://api.deepseek.com/v1",
		Notes:   "Paid, very cheap. Key: platform.deepseek.com",
	},
	"openai": {
		Name:    "OpenAI",
		BaseURL: "https://api.openai.com/v1",
		Notes:   "Paid. Key: platform.openai.com",
	},
	"vllm-local": {
		Name:     "Local vLLM / Ollama",
		BaseURL:  "http://127.0.0.1:8000/v1",
		Notes:    "Self-hosted OpenAI-compatible server (no key needed usually)",
		FreeTier: true,
	},
	"ollama-local": {
		Name:     "Local Ollama",
		BaseURL:  "http://127.0.0.1:11434/v1",
		Kind:     "local",
		Notes:    "Ollama 自带 OpenAI 兼容端点（/api 同服务），无需密钥，模型列表来自本机 /v1/models",
		FreeTier: true,
	},
	// §8.3 登录类免费源：无 API Key，用「登录连接（OAuth）」设备码授权后在编辑器绑定连接
	"kilocode": {
		Name:      "Kilo Code (Free)",
		BaseURL:   "https://api.kilo.ai/api/openrouter",
		Notes:     "设备码登录（无需 API Key）；登录后在编辑器「账号连接」选连接，组织头自动注入",
		FreeTier:  true,
		LoginFlow: true,
	},
	"github-copilot": {
		Name:          "GitHub Copilot (Free plan)",
		BaseURL:       "https://api.githubcopilot.com",
		Notes:         "设备码登录 GitHub → 自动兑换 Copilot 作业票（免 Key）；Copilot Free 计划含少量高级请求配额",
		FreeTier:      true,
		LoginFlow:     true,
		ModelsFetcher: &modelsFetcher{URL: "https://api.githubcopilot.com/models", Type: "openai"},
	},
	"qwen-chat": {
		Name:      "Qwen Chat (Free)",
		BaseURL:   "https://portal.qwen.ai/v1",
		Notes:     "设备码登录 Qwen Chat（PKCE，无需 API Key）；OpenAI 兼容端点",
		FreeTier:  true,
		LoginFlow: true,
	},
	"kimi-coding": {
		Name:          "Kimi for Coding (Free)",
		BaseURL:       "https://api.kimi.com/coding/v1",
		Notes:         "设备码登录 Kimi（标准 RFC8628，无需 API Key）；OpenAI 兼容 /coding/v1，含 k2 编码模型免费额度",
		FreeTier:      true,
		LoginFlow:     true,
		ModelIDs:      []string{"kimi-k2.6", "kimi-k2.5", "kimi-k2.5-thinking", "kimi-latest"},
		ModelsFetcher: &modelsFetcher{URL: "https://api.kimi.com/coding/v1/models", Type: "openai"},
	},
	"codebuddy-cn": {
		Name:      "CodeBuddy CN (Free)",
		BaseURL:   "https://copilot.tencent.com/v2",
		Notes:     "浏览器授权轮询登录（state-poll，无需 API Key）；腾讯 CodeBuddy 国内站 /v2/chat/completions",
		FreeTier:  true,
		LoginFlow: true,
		ModelIDs:  []string{"glm-5.3", "glm-5.3-flash", "glm-5.2", "minimax-m3", "kimi-k2.7"},
	},
	"codebuddy-intl": {
		Name:      "CodeBuddy Intl (Free)",
		BaseURL:   "https://www.codebuddy.ai/v2",
		Notes:     "浏览器授权轮询登录（state-poll，无需 API Key）；CodeBuddy 国际站 /v2/chat/completions",
		FreeTier:  true,
		LoginFlow: true,
		ModelIDs:  []string{"glm-5.3", "glm-5.3-flash", "glm-5.2", "minimax-m3", "kimi-k3-1"},
	},
	"workbuddy": {
		Name:      "WorkBuddy (Free)",
		BaseURL:   "https://www.workbuddy.ai/v2",
		Notes:     "浏览器授权轮询登录（state-poll，无需 API Key）；WorkBuddy /v2/chat/completions，含混元预览模型",
		FreeTier:  true,
		LoginFlow: true,
		ModelIDs:  []string{"hy3", "hy4-preview", "glm-5.3", "glm-5.3-flash", "minimax-m3"},
	},
}

// syncProviderModels 把启用中 provider 声明的模型同步进账号池（Source="provider"）：
// 模型管理页 / /v1/models / 默认模型下拉 / 回退链均以池为唯一数据源，同步后
// provider 模型与 Cline / zen 模型享有同样的列表展示、启停与自动回退语义。
// 规则：
//   - 仅同步 Enabled provider 的 modelIds；同 ID 已有其他来源条目（remote/zen/
//     用户自定义）时不重复添加，路由仍由 resolveProviderForModel 按 ID 命中
//   - 池内已有 provider 条目时只刷新归属与计费，保留 Disabled / Context /
//     Output / MetaLocked（用户手动改过的值不被覆盖）
//   - provider 删除 / 停用 / 移除模型后，对应池内条目自动清理；指向已清理
//     模型的默认模型一并清空
func syncProviderModels() {
	provs := listProviders() // providersMu 自持自放；之后再拿 poolMu，无反向持锁路径
	type claim struct {
		name   string
		cost   string
		signal string // id-only classifier hit, stamped as TierSignal provenance
	}
	claimed := map[string]claim{}
	var ids []string
	for _, p := range provs {
		if !p.Enabled {
			continue
		}
		cost := "pass"
		if p.Free {
			cost = "free"
		}
		for _, id := range p.ModelIDs {
			if id == "" {
				continue
			}
			if _, ok := claimed[id]; !ok {
				// Classify ids at seed time (same semantics as Cline/Zen sync):
				// :free / -free suffixes, free name tokens and the zen-seed
				// allowlist land in the pool as free immediately, without the
				// import wizard. Id-only signals stamp provenance; pricing and
				// entry-flag signals still require the catalog fetch path.
				c := claim{name: p.Name, cost: cost}
				if tier, sig := classifyUpstreamModel(upstreamModelEntry{ID: id}, p.Free); sig == tierSignalProviderFree {
					// The provider-level free flag is metadata, not a per-model
					// entitlement: record its provenance so Free-only routing
					// treats the route as unknown until verified.
					c.signal = tierSignalProviderFree
				} else if tier == "free" {
					c.cost, c.signal = "free", sig
				}
				claimed[id] = c
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids) // 稳定顺序，避免 map 遍历乱序导致池内顺序抖动

	p := loadPool()
	poolMu.Lock()
	changed := false
	inPool := make(map[string]bool, len(p.Models))
	for _, m := range p.Models {
		inPool[m.ID] = true
	}
	kept := make([]Model, 0, len(p.Models)+len(ids))
	for _, m := range p.Models {
		if m.Source == "provider" {
			if _, ok := claimed[m.ID]; !ok {
				changed = true // provider 已删除 / 停用 / 移除该模型
				continue
			}
		}
		kept = append(kept, m)
	}
	for _, id := range ids {
		c := claimed[id]
		if inPool[id] {
			// 已有条目：provider 条目刷新归属与计费（其他来源条目不动）
			for i := range kept {
				if kept[i].ID == id && kept[i].Source == "provider" {
					if kept[i].Provider != c.name {
						kept[i].Provider = c.name
						changed = true
					}
					// §9.3：分级器批准过的模型保留逐模型层级，不被整源层级覆盖。
					// Untiered entries that hit an id-only free signal are upgraded
					// in place with provenance, repairing entries seeded before the
					// classifier existed; otherwise the provider-wide tier applies.
					if kept[i].TierSignal == "" {
						if c.signal != "" && kept[i].Cost != "free" {
							kept[i].Cost = "free"
							kept[i].TierSignal = c.signal
							kept[i].TierDecidedAt = time.Now().Unix()
							changed = true
						} else if c.signal == "" && kept[i].Cost != c.cost {
							kept[i].Cost = c.cost
							changed = true
						}
					}
					break
				}
			}
			continue
		}
		m := Model{ID: id, Provider: c.name, Cost: c.cost, Status: "active", Source: "provider", TierSignal: c.signal}
		if c.signal != "" {
			m.TierDecidedAt = time.Now().Unix()
		}
		kept = append(kept, m)
		changed = true
	}
	if !changed {
		poolMu.Unlock()
		return
	}
	if p.DefaultModel != "" {
		stillListed := false
		for _, m := range kept {
			if m.ID == p.DefaultModel {
				stillListed = true
				break
			}
		}
		if !stillListed {
			p.DefaultModel = ""
		}
	}
	p.Models = kept
	poolMu.Unlock()
	savePool()
	log.Printf("provider models synced: %d model(s) from %d enabled provider(s)", len(claimed), len(provs))
}

// ============================================================================
// Fallback 集成：自定义 provider 参与 modelFallbackChain
// ============================================================================

// callCustomProviderAPI 先看粘贴会话是否已把该会话钉在本模型某个健康
// provider 上（钉住的不换模型、不随机切换）；未钉住时按冗余组逐家尝试，
// 任一成功即把该会话钉到它身上。全部失败返回最后一个 error，调用方决定
// 是否降级到回退链。模型没有可用 provider 时 attempted=false。
// 第 4 个返回值是实际服务本次尝试的 provider 显示名（成功=命中那家；
// attempted 失败=最后尝试那家；attempted=false 为空），供请求日志归因。
func callCustomProviderAPI(params map[string]any, stream bool) (*http.Response, error, bool, string) {
	model, _ := params["model"].(string)
	// P3 粘贴会话：同一会话（首个 user 消息哈希）在 TTL 内钉到同一家 provider，
	// 中途不因组内竞争或优先级重算而换源；钉住的 provider 冷却/超限/停用后
	// 立即解钉融入组内故障转移。
	sk := sessionKeyFromParams(params)
	if sk != "" {
		if pin := stickyGet(sk); pin != nil && pin.model == model {
			if p := pinProviderPresent(pin.providerID, model); p != nil {
				resp, err := callProvider(p, params, stream)
				if err == nil {
					stickySet(sk, pin.providerID, model)
					return resp, nil, true, p.Name
				}
				log.Printf("  provider attempt failed name=%s model=%q (pinned): %v", p.Name, model, err)
				if stop, cooldown, matched := ruleDecision(err); matched {
					if cooldown {
						setProviderCooldown(p.ID, model, ruleCooldownUntil(clineErrorStatus(err)))
					}
					if stop {
						stickyClear(sk)
						logRuleHit("stop", clineErrorStatus(err), "provider "+p.Name+" (sticky)")
						return nil, &requestErrorStop{err: err}, true, p.Name
					}
				}
			}
			stickyClear(sk) // 解钉：失败/不满足条件时融入正常组内转移
		}
	}
	provs := resolveProvidersForModel(model)
	if len(provs) == 0 {
		return nil, nil, false, ""
	}
	var lastErr error
	var lastName string
	for _, p := range provs {
		resp, err := callProvider(p, params, stream)
		if err == nil {
			if sk != "" {
				stickySet(sk, p.ID, model)
			}
			return resp, nil, true, p.Name
		}
		log.Printf("  provider attempt failed name=%s model=%q: %v", p.Name, model, err)
		lastErr = err
		lastName = p.Name
		if stop, cooldown, matched := ruleDecision(err); matched {
			if cooldown {
				setProviderCooldown(p.ID, model, ruleCooldownUntil(clineErrorStatus(err)))
			}
			if stop {
				logRuleHit("stop", clineErrorStatus(err), "provider "+p.Name)
				return nil, &requestErrorStop{err: err}, true, p.Name
			}
			logRuleHit("continue", clineErrorStatus(err), "provider "+p.Name)
		}
	}
	return nil, lastErr, true, lastName
}

// providerNameServesModel 判断是否存在**启用中**的 provider，其 Name 匹配（去空白、
// 大小写不敏感）且模型列表包含该模型。校验用：不关心冷却/预算（那是运行时的事）。
func providerNameServesModel(name, model string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, p := range listProviders() {
		if !p.Enabled || !strings.EqualFold(strings.TrimSpace(p.Name), name) {
			continue
		}
		for _, id := range p.ModelIDs {
			if id == model {
				return true
			}
		}
	}
	return false
}

// splitProviderPin parses the request-level qualified name "model@ProviderName"
// used to deliberately pick which provider serves a model id that multiple
// providers expose. LastIndexOf so ids containing '@' keep working; the pin is
// only honored when non-empty.
func splitProviderPin(model string) (bare, pin string) {
	i := strings.LastIndex(model, "@")
	if i <= 0 {
		return model, ""
	}
	bare, pin = model[:i], strings.TrimSpace(model[i+1:])
	if pin == "" {
		return model, ""
	}
	return bare, pin
}

// resolveProvidersNamedForModel 与 resolveProvidersForModel 同规则（启用、未冷却、
// 未超预算、服务该模型），但只保留 Name 匹配给定名称（去空白、大小写不敏感）的
// provider。用于组合条目把模型钉到某一家上游。
func resolveProvidersNamedForModel(model, name string) []*CustomProvider {
	loadProviders()
	providersMu.Lock()
	defer providersMu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	var cs []providerCand
	now := time.Now()
	for _, p := range customProviders {
		if !p.Enabled || !strings.EqualFold(strings.TrimSpace(p.Name), name) {
			continue
		}
		for _, m := range p.ModelIDs {
			if m != model {
				continue
			}
			if until, cool := providerCooldowns[p.ID+":"+model]; cool && now.Before(until) {
				break // 该 provider 的该模型冷却中
			}
			if providerBudgetExceeded(p.ID, model, p.RpmLimit, p.DailyLimit) {
				break
			}
			cs = append(cs, providerCand{p: *p})
			break
		}
	}
	cs = orderProviderCands(cs, model)
	out := make([]*CustomProvider, 0, len(cs))
	for i := range cs {
		out = append(out, &cs[i].p)
	}
	return out
}

// callCustomProviderAPINamed 与 callCustomProviderAPI 类似，但只考虑 Name 匹配的
// provider（模型固定到某一家上游）：失败时**不**回退到组内其他 provider。
// 故意不读取/写入粘性会话——显式固定应当压过自动粘贴，且固定到 A 家的调用不应
// 污染另一家的粘贴状态。无匹配候选时 attempted=false（调用方决定是否兜底）。
// 供请求日志归因。
func callCustomProviderAPINamed(params map[string]any, stream bool, name string) (*http.Response, error, bool, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, false, ""
	}
	model, _ := params["model"].(string)
	provs := resolveProvidersNamedForModel(model, name)
	if len(provs) == 0 {
		return nil, nil, false, ""
	}
	var lastErr error
	var lastName string
	for _, p := range provs {
		resp, err := callProvider(p, params, stream)
		if err == nil {
			return resp, nil, true, p.Name
		}
		log.Printf("  provider attempt failed name=%s model=%q (pinned): %v", p.Name, model, err)
		lastErr = err
		lastName = p.Name
		// A pinned-provider failure is already fatal to the caller; rules can
		// only add the cooldown side effect here, not change the flow.
		if _, cooldown, matched := ruleDecision(err); matched && cooldown {
			setProviderCooldown(p.ID, model, ruleCooldownUntil(clineErrorStatus(err)))
			logRuleHit("cooldown", clineErrorStatus(err), "provider "+p.Name+" (pinned)")
		}
	}
	return nil, lastErr, true, lastName
}

// ============================================================================
// Admin API handlers
// ============================================================================

func handleProvidersList(w http.ResponseWriter, r *http.Request) {
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"providers": listProviders()}})
}

func handleProviderSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var p CustomProvider
	if err := json.Unmarshal(body, &p); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	p.Name = strings.TrimSpace(p.Name)
	p.BaseURL = strings.TrimSpace(strings.TrimRight(p.BaseURL, "/"))
	p.APIKey = strings.TrimSpace(p.APIKey)
	if p.Name == "" || p.BaseURL == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "name and baseURL are required"})
		return
	}
	if !strings.HasPrefix(p.BaseURL, "http://") && !strings.HasPrefix(p.BaseURL, "https://") {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "baseURL must start with http:// or https://"})
		return
	}
	// 清洗模型 ID 与请求头
	var models []string
	seen := map[string]bool{}
	for _, m := range p.ModelIDs {
		m = strings.TrimSpace(m)
		if m != "" && !seen[m] {
			seen[m] = true
			models = append(models, m)
		}
	}
	p.ModelIDs = models
	hdrs := map[string]string{}
	for k, v := range p.Headers {
		k = strings.TrimSpace(k)
		if k != "" {
			hdrs[k] = strings.TrimSpace(v)
		}
	}
	p.Headers = hdrs
	if p.TimeoutSec < 0 || p.TimeoutSec > 600 {
		p.TimeoutSec = 0
	}
	normalizedProxy, proxyErr := transport.NormalizeProxySpec(p.ProxyURL)
	if proxyErr != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: proxyErr.Error()})
		return
	}
	p.ProxyURL = normalizedProxy
	if p.Weight < 0 {
		p.Weight = -1 // explicit exclusion from weighted_round_robin
	}
	if p.Weight > 1_000_000 {
		p.Weight = 1_000_000
	}
	if p.RpmLimit < 0 {
		p.RpmLimit = 0
	}
	if p.DailyLimit < 0 {
		p.DailyLimit = 0
	}
	// Wave-4 pool hygiene: keep only connection IDs that still exist.
	seenConn := map[string]bool{}
	var cids []string
	for _, id := range p.ConnectionIDs {
		id = strings.TrimSpace(id)
		if id == "" || seenConn[id] || !connectionExists(id) {
			continue
		}
		seenConn[id] = true
		cids = append(cids, id)
	}
	p.ConnectionIDs = cids
	if p.ConnectionID != "" && !connectionExists(p.ConnectionID) {
		p.ConnectionID = ""
	}
	saved := upsertProvider(&p)
	syncProviderModels() // 模型列表 / 回退链立即反映变更
	log.Printf("admin: provider saved name=%s models=%d enabled=%v", saved.Name, len(saved.ModelIDs), saved.Enabled)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"provider": saved}})
}

func handleProviderDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	if !deleteProvider(req.ID) {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "provider not found"})
		return
	}
	syncProviderModels() // 清理该 provider 声明的池内模型条目
	clearProviderRecheck(req.ID)
	log.Printf("admin: provider deleted id=%s", req.ID)
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleProviderTest 发一条 min-token 请求验证 provider 可用性。
// 两侧重载：
//   - {id, model?}            → 已保存的 provider（model 非空时覆盖默认 ModelIDs[0]）
//   - {baseURL, apiKey?, headers?, model?} → 未保存配置的临时测试：构造临时
//     CustomProvider 走同一 callProvider 路径，不落盘、不产生新状态。
//
// 上游失败 ≠ API 失败：一律 200 包裹 {success:true, data:{ok:false, error:...}}。
func handleProviderTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	var req struct {
		ID      string            `json:"id"`
		BaseURL string            `json:"baseURL"`
		APIKey  string            `json:"apiKey"`
		Headers map[string]string `json:"headers"`
		Model   string            `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	model := strings.TrimSpace(req.Model)

	if strings.TrimSpace(req.BaseURL) != "" {
		// 未保存配置路径：一次性 provider（不进 registry，不落盘）。
		baseURL := strings.TrimSpace(strings.TrimRight(req.BaseURL, "/"))
		if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "baseURL must start with http:// or https://"})
			return
		}
		if model == "" {
			// 临时 provider 未声明模型列表，空 model 无从发起补全，需给出明确说明。
			writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
				"ok": false, "model": "", "durationMs": 0,
				"error": "model is required when testing an unsaved provider",
			}})
			return
		}
		target := &CustomProvider{
			BaseURL:    baseURL,
			APIKey:     strings.TrimSpace(req.APIKey),
			Headers:    req.Headers,
			TimeoutSec: quickTestProviderTimeoutSec, // 探测用短超时
		}
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: runProviderMiniTest(target, model)})
		return
	}

	loadProviders()
	providersMu.Lock()
	var target *CustomProvider
	for _, p := range customProviders {
		if p.ID == req.ID {
			cp := *p
			target = &cp
			break
		}
	}
	providersMu.Unlock()
	if target == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "provider not found"})
		return
	}
	if model == "" && len(target.ModelIDs) > 0 {
		model = target.ModelIDs[0]
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: runProviderMiniTest(target, model)})
}

// runProviderMiniTest 用 callProvider 发一条 min-token（16 tokens / 单条 "hi"）
// 非流式补全，返回测试结果 map：durationMs/model/ok,error?/inputTokens?/outputTokens?。
func runProviderMiniTest(p *CustomProvider, model string) map[string]any {
	params := map[string]any{
		"model":      model,
		"max_tokens": 16,
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
	}
	started := time.Now()
	resp, err := callProvider(p, params, false)
	result := map[string]any{"durationMs": time.Since(started).Milliseconds(), "model": model}
	if err != nil {
		result["ok"] = false
		result["error"] = truncate(err.Error(), 300)
		return result
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		if data, ok := obj["data"].(map[string]any); ok {
			obj = data
		}
		obj = normalizeOpenAIResponse(obj)
	}
	result["ok"] = true
	if u := parseTokenUsage(obj["usage"]); u.Valid {
		result["inputTokens"] = u.Prompt
		result["outputTokens"] = u.Completion
	}
	return result
}

func handleProviderPresets(w http.ResponseWriter, r *http.Request) {
	type presetOut struct {
		Key           string            `json:"key"`
		Name          string            `json:"name"`
		BaseURL       string            `json:"baseURL"`
		ModelIDs      []string          `json:"modelIDs,omitempty"`
		ModelsFetcher *modelsFetcher    `json:"modelsFetcher,omitempty"`
		Headers       map[string]string `json:"headers,omitempty"`
		Notes         string            `json:"notes,omitempty"`
		FreeTier      bool              `json:"freeTier"`
		Kind          string            `json:"kind"`
		LoginFlow     bool              `json:"loginFlow,omitempty"`
	}
	presets := mergePresets(providerreg.Builtin().All(), providerPresetsImported)
	keys := make([]string, 0, len(presets))
	for k := range presets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]presetOut, 0, len(keys))
	for _, k := range keys {
		p := presets[k]
		kind := p.Kind
		if kind == "" {
			if p.FreeTier {
				kind = "freeTier"
			} else {
				kind = "other"
			}
		}
		out = append(out, presetOut{Key: k, Name: p.Name, BaseURL: p.BaseURL, Headers: p.Headers, Notes: p.Notes, FreeTier: p.FreeTier, ModelIDs: p.ModelIDs, ModelsFetcher: p.ModelsFetcher, Kind: kind, LoginFlow: p.LoginFlow})
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"presets": out}})
}
