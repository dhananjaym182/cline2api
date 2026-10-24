package main

// ============================================================================
// §8.3 Provider Connections：provider 作用域的登录凭据（与 Cline 账号池并存）。
// 引擎按 flowSpec 数据驱动（对齐 ExtremeRouter 的五种 flowType 中最常用的
// device_code），新增一个 provider 登录 = 往 providerDeviceFlows 加一条数据，
// 不新增控制流。Kilo Code 为首个具体移植（§8.3.2，步骤 4）。
// ============================================================================

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Connection 一个 provider 登录连接（OAuth 设备码或粘贴 token 导入）。
type Connection struct {
	ID          string `json:"id"`
	Provider    string `json:"provider"` // flowSpec 的 key，如 "kilocode"
	AuthType    string `json:"authType"` // "device_code" | "import_token"
	Email       string `json:"email,omitempty"`
	AccessToken string `json:"accessToken"`
	// RefreshToken 空 = 上游不签发（如 Kilo）→ 过期即标记 reauth_required，绝不试刷新
	RefreshToken         string            `json:"refreshToken,omitempty"`
	ExpiresAt            int64             `json:"expiresAt,omitempty"`            // unix 毫秒，0=不过期
	ProviderSpecificData map[string]string `json:"providerSpecificData,omitempty"` // 如 orgId
	Status               string            `json:"status"`                         // "active" | "reauth_required"
	CreatedAt            time.Time         `json:"createdAt"`
	// Wave-4 usage attribution (mirrors Account counters; provider traffic used
	// to be recorded nowhere). Safe to omit for connections that never served.
	UsageCount       int64                 `json:"usageCount,omitempty"`
	PromptTokens     int64                 `json:"promptTokens,omitempty"`
	CompletionTokens int64                 `json:"completionTokens,omitempty"`
	TotalTokens      int64                 `json:"totalTokens,omitempty"`
	CachedTokens     int64                 `json:"cachedTokens,omitempty"`
	ModelStats       map[string]*ModelStat `json:"modelStats,omitempty"`
}

var (
	connectionsMu       sync.Mutex
	connections         []*Connection
	connectionsLoaded   bool
	connectionsFilePath string
)

func init() {
	connectionsFilePath = resolveDataPath(".cline-connections.json")
}

func loadConnections() {
	connectionsMu.Lock()
	defer connectionsMu.Unlock()
	if connectionsLoaded {
		return
	}
	connectionsLoaded = true
	data, err := os.ReadFile(connectionsFilePath)
	if err != nil {
		return
	}
	var list []*Connection
	if err := json.Unmarshal(data, &list); err != nil {
		backupCorruptFile(connectionsFilePath, err)
		return
	}
	connections = list
}

// saveConnectionsLocked 调用方持 connectionsMu。
func saveConnectionsLocked() {
	data, err := json.MarshalIndent(connections, "", "  ")
	if err != nil {
		return
	}
	if err := writeFileAtomic(connectionsFilePath, data, 0600); err != nil {
		log.Printf("connections save failed: %v", err)
	}
}

func newConnectionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "conn_" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return "conn_" + hex.EncodeToString(b)
}

// upsertConnection 按 provider+email 去重（无 email 时按 token 指纹）；替换时保留原 ID/CreatedAt。
func upsertConnection(c *Connection) *Connection {
	loadConnections()
	connectionsMu.Lock()
	defer connectionsMu.Unlock()
	fingerprint := ""
	if c.Email == "" && c.AccessToken != "" {
		sum := sha256.Sum256([]byte(c.AccessToken))
		fingerprint = hex.EncodeToString(sum[:])[:12]
	}
	for i, old := range connections {
		same := old.Provider == c.Provider && ((c.Email != "" && old.Email == c.Email) ||
			(fingerprint != "" && old.ProviderSpecificData["tokenFp"] == fingerprint))
		if same {
			c.ID, c.CreatedAt = old.ID, old.CreatedAt
			// Usage history survives re-login/re-import replacements.
			c.UsageCount, c.PromptTokens, c.CompletionTokens = old.UsageCount, old.PromptTokens, old.CompletionTokens
			c.TotalTokens, c.CachedTokens, c.ModelStats = old.TotalTokens, old.CachedTokens, old.ModelStats
			if c.ProviderSpecificData == nil {
				c.ProviderSpecificData = map[string]string{}
			}
			if fingerprint != "" {
				c.ProviderSpecificData["tokenFp"] = fingerprint
			}
			connections[i] = c
			saveConnectionsLocked()
			return c
		}
	}
	c.ID = newConnectionID()
	c.CreatedAt = time.Now()
	if c.ProviderSpecificData == nil {
		c.ProviderSpecificData = map[string]string{}
	}
	if fingerprint != "" {
		c.ProviderSpecificData["tokenFp"] = fingerprint
	}
	connections = append(connections, c)
	saveConnectionsLocked()
	return c
}

func removeConnection(id string) bool {
	loadConnections()
	connectionsMu.Lock()
	removed := false
	for i, c := range connections {
		if c.ID == id {
			connections = append(connections[:i], connections[i+1:]...)
			saveConnectionsLocked()
			removed = true
			break
		}
	}
	connectionsMu.Unlock()
	if removed {
		// Wave-4: dropped connections must not linger in provider pools.
		// Prune outside connectionsMu — pruneConnectionRefs takes
		// providersMu, and candidate resolution holds providersMu while
		// taking connectionsMu (reverse order would deadlock).
		pruneConnectionRefs(id)
	}
	return removed
}

// markConnectionReauth 上游 401/403 且无刷新能力时调用（不锁死，用户重登即恢复）。
func markConnectionReauth(id string) {
	loadConnections()
	connectionsMu.Lock()
	defer connectionsMu.Unlock()
	for _, c := range connections {
		if c.ID == id && c.Status == "active" {
			c.Status = "reauth_required"
			saveConnectionsLocked()
			return
		}
	}
}

// ---------- device-code 流程引擎 ----------

// deviceFlowSpec 一个 provider 的 device_code 登录所需的全部端点与字段差异（数据，非控制流）。
type deviceFlowSpec struct {
	InitiateURL   string // kilo 模式：POST JSON 空体，响应含 code / verificationUrl / expiresIn
	PollURLBase   string // kilo 模式：GET {base}/{code}：202 pending / 403 denied / 410 expired / 200+approved 得 token
	ProfileURL    string // 可选：GET bearer，用于补齐账户信息
	EmailPath     string // ProfileURL 响应里 email 的取值路径，如 "email"
	OrgIDPath     string // ProfileURL 响应里组织 id 的取值路径，如 "organizations.0.id"
	OrgHeader     string // 非空时按 OrgIDPath 注入动态请求头（Kilo: X-Kilocode-OrganizationID）
	ModelsTestURL string // 连接测试 / 模型发现用的 GET 端点（bearer + OrgHeader）
	IntervalSec   int    // 轮询间隔（默认 3）

	// rfc8628 模式（GitHub / Qwen Portal / Kimi）：POST form 授权 + 轮询，error 字段表状态
	Mode          string // "" | "kilo" | "rfc8628" | "state-poll"
	TokenURL      string // rfc8628 轮询端点（POST x-www-form-urlencoded）
	ClientID      string
	Scope         string
	PKCE          bool   // 授权请求带 code_challenge(S256)，轮询带 code_verifier
	UserCodeField string // 用户码字段名（默认 "user_code"；kilo 为 "code"）

	// state-poll 模式（CodeBuddy 家族：codebuddy-cn/intl、workbuddy）：
	// POST {StateURL}?platform= 得 {code:0,data:{state,authUrl}}；
	// GET {TokenURL}?state= 轮询，code 11217=pending / code 0+data.accessToken=成功。
	// 请求需带 X-Domain(=StateURL host)、X-Requested-With、X-No-*、X-Product 与固定 UA。
	StateURL  string // state-poll 授权端点（POST 空 JSON）
	Platform  string // state-poll：platform 查询参数（CLI / ide / workbuddy）
	UserAgent string // state-poll：CLI/... UA

	// qoder 模式（Qoder / Qoder CN）：本地生成 PKCE、nonce 与 machine_id，
	// 浏览器打开 {LoginURL}?challenge=&challenge_method=S256&machine_id=&nonce=；
	// GET {TokenURL}?nonce=&verifier=&challenge_method=S256 轮询，
	// 202/404=pending，200 返回 {token, refresh_token, user_id, expires_at|expires_in}。
	LoginURL string

	// 可选二段兑换：登录 token → 短期作业票（GitHub: OAuth token → copilot job token）
	ExchangeURL       string // GET + Bearer，响应 JSON
	ExchangeTokenPath string // 如 "token"
	ExchangeExpPath   string // 如 "expires_at"（unix 秒）
}

var providerDeviceFlows = map[string]deviceFlowSpec{
	"kilocode": {
		InitiateURL:   "https://api.kilo.ai/api/device-auth/codes",
		PollURLBase:   "https://api.kilo.ai/api/device-auth/codes",
		ProfileURL:    "https://api.kilo.ai/api/profile",
		EmailPath:     "email",
		OrgIDPath:     "organizations.0.id",
		OrgHeader:     "X-Kilocode-OrganizationID",
		ModelsTestURL: "https://api.kilo.ai/api/gateway/models",
		IntervalSec:   3,
	},
	"github": {
		Mode: "rfc8628",
		// GitHub Copilot Free plan：设备码登录 → copilot job token → api.githubcopilot.com（OpenAI 兼容）
		InitiateURL:       "https://github.com/login/device/code",
		TokenURL:          "https://github.com/login/oauth/access_token",
		ClientID:          "Iv1.b507a08c87ecfe98",
		Scope:             "read:user",
		ProfileURL:        "https://api.github.com/user",
		EmailPath:         "login",
		ModelsTestURL:     "https://api.githubcopilot.com/models",
		ExchangeURL:       "https://api.github.com/copilot_internal/v2/token",
		ExchangeTokenPath: "token",
		ExchangeExpPath:   "expires_at",
		IntervalSec:       5,
	},
	"qwen": {
		Mode:        "rfc8628",
		InitiateURL: "https://chat.qwen.ai/api/v1/oauth2/device/code",
		TokenURL:    "https://chat.qwen.ai/api/v1/oauth2/token",
		ClientID:    "f0304373b74a44d2b584a3fb70ca9e56",
		Scope:       "openid profile email model.completion",
		PKCE:        true,
		// Qwen Chat OAuth 票直连 portal.qwen.ai/v1（OpenAI 兼容）
		ModelsTestURL: "https://portal.qwen.ai/v1/models",
		IntervalSec:   3,
	},
	"kimi-coding": {
		Mode:        "rfc8628",
		InitiateURL: "https://auth.kimi.com/api/oauth/device_authorization",
		TokenURL:    "https://auth.kimi.com/api/oauth/token",
		ClientID:    "17e5f671-d194-4dfb-9706-5516cb48c098",
		// 端点形状已实测（2026-10）：initiate 返回标准 device_code/user_code/verification_uri
		ModelsTestURL: "https://api.kimi.com/coding/v1/models",
		IntervalSec:   5,
	},
	// CodeBuddy 家族：同一 /v2/plugin 轮询契约，三个域名各一条（登录成功形状按
	// 上游 services 实现镜像：code 0 + data.accessToken / 11217 pending）
	"codebuddy-cn": {
		Mode:          "state-poll",
		StateURL:      "https://copilot.tencent.com/v2/plugin/auth/state",
		TokenURL:      "https://copilot.tencent.com/v2/plugin/auth/token",
		Platform:      "CLI",
		UserAgent:     "CLI/2.63.2 CodeBuddy/2.63.2",
		ModelsTestURL: "https://copilot.tencent.com/v2/models",
		IntervalSec:   5,
	},
	"codebuddy-intl": {
		Mode:          "state-poll",
		StateURL:      "https://www.codebuddy.ai/v2/plugin/auth/state",
		TokenURL:      "https://www.codebuddy.ai/v2/plugin/auth/token",
		Platform:      "ide",
		UserAgent:     "IDE/2.63.2 CodeBuddy/2.63.2",
		ModelsTestURL: "https://www.codebuddy.ai/v2/models",
		IntervalSec:   5,
	},
	"workbuddy": {
		Mode:          "state-poll",
		StateURL:      "https://www.workbuddy.ai/v2/plugin/auth/state",
		TokenURL:      "https://www.workbuddy.ai/v2/plugin/auth/token",
		Platform:      "workbuddy",
		UserAgent:     "CLI/2.63.2 WorkBuddy/2.63.2",
		ModelsTestURL: "https://www.workbuddy.ai/v2/models",
		IntervalSec:   5,
	},
	// Qoder（国际站 / 国内站）：PKCE S256 + nonce + machine_id 的浏览器轮询授权
	// （对齐 9router 实现）。轮询/用户信息走 openapi 域名；ModelsTestURL 用
	// userinfo 仅做连通性探测（推理协议为 COSY 签名的私有接口，非 OpenAI 兼容）。
	"qoder": {
		Mode:          "qoder",
		LoginURL:      "https://qoder.com/device/selectAccounts",
		TokenURL:      "https://openapi.qoder.sh/api/v1/deviceToken/poll",
		ProfileURL:    "https://openapi.qoder.sh/api/v1/userinfo",
		EmailPath:     "email",
		OrgIDPath:     "organization_id",
		ModelsTestURL: "https://openapi.qoder.sh/api/v1/userinfo",
		UserAgent:     "Go-http-client/2.0",
		IntervalSec:   2,
	},
	"qoder-cn": {
		Mode:          "qoder",
		LoginURL:      "https://qoder.com.cn/device/selectAccounts",
		TokenURL:      "https://openapi.qoder.com.cn/api/v1/deviceToken/poll",
		ProfileURL:    "https://openapi.qoder.com.cn/api/v1/userinfo",
		EmailPath:     "email",
		OrgIDPath:     "organization_id",
		ModelsTestURL: "https://openapi.qoder.com.cn/api/v1/userinfo",
		UserAgent:     "Go-http-client/2.0",
		IntervalSec:   2,
	},
}

// 登录会话：start 建会话并后台轮询；status 读取（对齐 Cline WorkOS 会话 UX）。
type providerLoginSession struct {
	ID         string
	Provider   string
	UserCode   string
	AuthURL    string
	CreatedAt  time.Time
	Done       bool
	Success    bool
	Error      string
	Connection *Connection
}

var (
	providerLoginMu       sync.Mutex
	providerLoginSessions = map[string]*providerLoginSession{}
)

func jsonPathString(body map[string]any, path string) string {
	var cur any = body
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			continue
		}
		if m, ok := cur.(map[string]any); ok {
			cur = m[seg]
			continue
		}
		if arr, ok := cur.([]any); ok {
			var idx int
			if _, err := fmt.Sscanf(seg, "%d", &idx); err == nil && idx >= 0 && idx < len(arr) {
				cur = arr[idx]
				continue
			}
		}
		return ""
	}
	if s, ok := cur.(string); ok {
		return s
	}
	return ""
}

// deviceLoginResult 两种轮询模式（kilo / rfc8628）的统一成功形态。
type deviceLoginResult struct {
	token, refreshToken, email, orgID string
	userID                            string // qoder：上游 user_id，用于邮箱缺失时的账号标识
	expiresAtMs                       int64  // 0 = 不过期（Kilo 语义）
	exchangedToken                    string
	exchangedExpMs                    int64
}

// newUUIDv4 生成 RFC 4122 v4 UUID（qoder 的 nonce / machine_id 形状）。
func newUUIDv4() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// newPKCE S256 verifier/challenge（RFC 7636；Qwen 设备码要求带 challenge 授权、带 verifier 轮询）。
func newPKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// statePollHeaders CodeBuddy 家族固定请求头：X-Domain 取 StateURL host，其余按上游 CLI 常量。
func statePollHeaders(spec deviceFlowSpec, isPoll bool) map[string]string {
	host := ""
	if u, e := url.Parse(spec.StateURL); e == nil {
		host = u.Host
	}
	h := map[string]string{
		"Accept":             "application/json",
		"User-Agent":         spec.UserAgent,
		"X-Requested-With":   "XMLHttpRequest",
		"X-Domain":           host,
		"X-No-Authorization": "true",
		"X-No-User-Id":       "true",
		"X-Product":          "SaaS",
	}
	if isPoll {
		h["X-No-Enterprise-Id"] = "true"
		h["X-No-Department-Info"] = "true"
	} else {
		h["Content-Type"] = "application/json"
	}
	return h
}

// exchangeJobToken 二段兑换（GitHub Copilot）：OAuth token → 短期作业票。
func exchangeJobToken(spec deviceFlowSpec, accessToken string) (token string, expMs int64, err error) {
	req, e := http.NewRequest(http.MethodGet, spec.ExchangeURL, nil)
	if e != nil {
		return "", 0, e
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, e := oauthHTTPClient(15 * time.Second).Do(req)
	if e != nil {
		return "", 0, fmt.Errorf("token exchange failed: %v", e)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("token exchange: %d %s", resp.StatusCode, truncate(strings.TrimSpace(string(body)), 160))
	}
	var data map[string]any
	if json.Unmarshal(body, &data) != nil {
		return "", 0, fmt.Errorf("token exchange decode")
	}
	token = jsonPathString(data, spec.ExchangeTokenPath)
	if token == "" {
		return "", 0, fmt.Errorf("token exchange missing %s", spec.ExchangeTokenPath)
	}
	switch v := data[spec.ExchangeExpPath].(type) {
	case float64:
		expMs = int64(v) * 1000
	case string:
		if n, e2 := strconv.ParseInt(v, 10, 64); e2 == nil {
			expMs = n * 1000
		}
	}
	return token, expMs, nil
}

// oauthHTTPClient routes auth traffic through the shared outbound transport
// (SOCKS5 egress proxy pool + custom dialer) like proxied chat traffic; a bare
// &http.Client{} would bypass the configured egress proxy entirely.
func oauthHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: httpClient.Transport}
}

// htmlBlockedError reports an HTML page where JSON was expected — typically a
// 405 SPA error page from a blocked/moved endpoint — with actionable hints.
func htmlBlockedError(resp *http.Response, body []byte) error {
	snippet := strings.ToLower(strings.TrimSpace(string(body)))
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html") && !strings.HasPrefix(snippet, "<") {
		return nil
	}
	return fmt.Errorf("upstream returned an HTML page (HTTP %d) instead of JSON — the login endpoint is likely blocked locally (hosts-file sinkhole / DNS) or has moved upstream; configure an egress proxy in Settings or use Token import", resp.StatusCode)
}

// decodeJSONBody parses an upstream auth response. On non-200 or unparseable
// bodies it returns an actionable error: HTML bodies get the blocked-endpoint
// hint, everything else keeps the status + body snippet.
func decodeJSONBody(op string, resp *http.Response, body []byte) (map[string]any, error) {
	if resp.StatusCode == http.StatusOK {
		var data map[string]any
		if err := json.Unmarshal(body, &data); err == nil {
			return data, nil
		}
		if htmlErr := htmlBlockedError(resp, body); htmlErr != nil {
			return nil, fmt.Errorf("%s decode: %v", op, htmlErr)
		}
		return nil, fmt.Errorf("%s decode: unexpected JSON payload", op)
	}
	if htmlErr := htmlBlockedError(resp, body); htmlErr != nil {
		return nil, fmt.Errorf("%s failed: %v", op, htmlErr)
	}
	return nil, fmt.Errorf("%s failed: %d %s", op, resp.StatusCode, truncate(strings.TrimSpace(string(body)), 200))
}

// dialHintError flags dial/lookup failures that point at local name resolution
// (e.g. a hosts-file 0.0.0.1 sinkhole) instead of leaving the raw Go error.
func dialHintError(op string, err error) error {
	msg := err.Error()
	if strings.Contains(msg, "dial tcp 0.0.0.1:") || strings.Contains(msg, "no such host") || strings.Contains(msg, "server misbehaving") {
		return fmt.Errorf("%s failed: %v — domain did not resolve (check /etc/hosts or Windows hosts for a block entry) or is unreachable without a proxy; configure the egress proxy in Settings to route this request", op, err)
	}
	return fmt.Errorf("%s failed: %v", op, err)
}

// qoderExpiryMs interprets Qoder's token expiry: numeric expires_at (seconds or
// milliseconds), RFC3339 string, or expires_in seconds. Qoder's refresh endpoint
// rejects device tokens, so an already-expired/short lifetime would drop the
// account into a re-login loop; like 9router we floor it to one day. With no
// expiry hint at all the upstream default is 30 days.
func qoderExpiryMs(data map[string]any) int64 {
	var ms int64
	switch v := data["expires_at"].(type) {
	case float64:
		ms = epochToMs(int64(v))
	case string:
		if n, e := strconv.ParseInt(v, 10, 64); e == nil {
			ms = epochToMs(n)
		} else if ts, e := time.Parse(time.RFC3339, v); e == nil {
			ms = ts.UnixMilli()
		}
	}
	if ms == 0 {
		if n, ok := data["expires_in"].(float64); ok {
			ms = time.Now().Add(time.Duration(n) * time.Second).UnixMilli()
		}
	}
	if ms == 0 {
		return time.Now().Add(30 * 24 * time.Hour).UnixMilli()
	}
	if floor := time.Now().Add(24 * time.Hour).UnixMilli(); ms < floor {
		return floor
	}
	return ms
}

func epochToMs(n int64) int64 {
	if n <= 0 {
		return 0
	}
	if n < 1_000_000_000_000 {
		return n * 1000
	}
	return n
}

// startProviderDeviceLogin  initiate → 会话登记 → 后台轮询 goroutine。
func startProviderDeviceLogin(provider string) (*providerLoginSession, error) {
	spec, ok := providerDeviceFlows[provider]
	if !ok {
		return nil, fmt.Errorf("no device_code flow registered for provider %q", provider)
	}
	var verifier string
	var qoderMachineID string
	integ := func() (code, userDisp, verifURL string, expiresIn int, err error) {
		if spec.Mode == "qoder" {
			v, challenge, e := newPKCE()
			if e != nil {
				return "", "", "", 0, e
			}
			machineID, e := newUUIDv4()
			if e != nil {
				return "", "", "", 0, e
			}
			nonce, e := newUUIDv4()
			if e != nil {
				return "", "", "", 0, e
			}
			verifier, qoderMachineID = v, machineID
			q := url.Values{}
			q.Set("challenge", challenge)
			q.Set("challenge_method", "S256")
			q.Set("machine_id", machineID)
			q.Set("nonce", nonce)
			disp := strings.ToUpper(nonce)
			if len(disp) > 8 {
				disp = disp[:8]
			}
			return nonce, disp, spec.LoginURL + "?" + q.Encode(), 300, nil
		}
		if spec.Mode == "rfc8628" {
			form := url.Values{}
			form.Set("client_id", spec.ClientID)
			if spec.Scope != "" {
				form.Set("scope", spec.Scope)
			}
			if spec.PKCE {
				v, challenge, e := newPKCE()
				if e != nil {
					return "", "", "", 0, e
				}
				verifier = v
				form.Set("code_challenge", challenge)
				form.Set("code_challenge_method", "S256")
			}
			req, e := http.NewRequest(http.MethodPost, spec.InitiateURL, strings.NewReader(form.Encode()))
			if e != nil {
				return "", "", "", 0, e
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Accept", "application/json")
			resp, e := oauthHTTPClient(15 * time.Second).Do(req)
			if e != nil {
				return "", "", "", 0, dialHintError("initiate", e)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
			data, e := decodeJSONBody("device auth initiation", resp, body)
			if e != nil {
				return "", "", "", 0, e
			}
			code = jsonPathString(data, "device_code")
			userDisp = jsonPathString(data, "user_code")
			verifURL = jsonPathString(data, "verification_uri_complete")
			if verifURL == "" {
				verifURL = jsonPathString(data, "verification_uri")
			}
			if n, okk := data["expires_in"].(float64); okk && n > 0 {
				expiresIn = int(n)
			} else {
				expiresIn = 600
			}
			if code == "" {
				return "", "", "", 0, fmt.Errorf("initiate missing device_code")
			}
			if userDisp == "" {
				userDisp = code
			}
			return code, userDisp, verifURL, expiresIn, nil
		}
		if spec.Mode == "state-poll" {
			target := spec.StateURL
			if spec.Platform != "" {
				target += "?platform=" + url.QueryEscape(spec.Platform)
			}
			req, e := http.NewRequest(http.MethodPost, target, bytes.NewReader([]byte("{}")))
			if e != nil {
				return "", "", "", 0, e
			}
			for k, v := range statePollHeaders(spec, false) {
				req.Header.Set(k, v)
			}
			resp, e := oauthHTTPClient(15 * time.Second).Do(req)
			if e != nil {
				return "", "", "", 0, dialHintError("initiate", e)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
			data, e := decodeJSONBody("device auth initiation", resp, body)
			if e != nil {
				return "", "", "", 0, e
			}
			if c, _ := data["code"].(float64); c != 0 {
				msg, _ := data["msg"].(string)
				return "", "", "", 0, fmt.Errorf("state error: %s", truncate(msg, 160))
			}
			inner, _ := data["data"].(map[string]any)
			state, _ := inner["state"].(string)
			authURL, _ := inner["authUrl"].(string)
			if state == "" {
				return "", "", "", 0, fmt.Errorf("state missing in response")
			}
			if authURL == "" {
				authURL = state // 无链接时至少展示 state
			}
			return state, state, authURL, 900, nil
		}
		req, e := http.NewRequest(http.MethodPost, spec.InitiateURL, bytes.NewReader([]byte("{}")))
		if e != nil {
			return "", "", "", 0, e
		}
		req.Header.Set("Content-Type", "application/json")
		resp, e := oauthHTTPClient(15 * time.Second).Do(req)
		if e != nil {
			return "", "", "", 0, dialHintError("initiate", e)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		if resp.StatusCode == http.StatusTooManyRequests {
			return "", "", "", 0, fmt.Errorf("too many pending authorization requests, try again later")
		}
		data, e := decodeJSONBody("device auth initiation", resp, body)
		if e != nil {
			return "", "", "", 0, e
		}
		code = jsonPathString(data, "code")
		verifURL = jsonPathString(data, "verificationUrl")
		if n, okk := data["expiresIn"].(float64); okk && n > 0 {
			expiresIn = int(n)
		} else {
			expiresIn = 300
		}
		return code, code, verifURL, expiresIn, nil
	}
	code, userDisp, verifURL, expiresIn, err := integ()
	if err != nil {
		return nil, err
	}
	sess := &providerLoginSession{
		ID:        fmt.Sprintf("plog_%d_%s", time.Now().UnixNano(), provider),
		Provider:  provider,
		UserCode:  userDisp,
		AuthURL:   verifURL,
		CreatedAt: time.Now(),
	}
	providerLoginMu.Lock()
	for id, old := range providerLoginSessions {
		if len(providerLoginSessions) >= 20 || time.Since(old.CreatedAt) > 15*time.Minute {
			delete(providerLoginSessions, id)
		}
	}
	providerLoginSessions[sess.ID] = sess
	providerLoginMu.Unlock()

	go func() {
		res, pollErr := pollProviderDeviceToken(spec, code, expiresIn, verifier)
		var conn *Connection
		if pollErr == nil {
			conn = upsertConnection(&Connection{
				Provider: provider, AuthType: "device_code", Email: res.email,
				AccessToken: res.token, RefreshToken: res.refreshToken,
				ExpiresAt: res.expiresAtMs, Status: "active",
			})
			psd := map[string]string{}
			if res.orgID != "" {
				psd["orgId"] = res.orgID
			}
			if spec.Mode == "qoder" {
				psd["authMethod"] = "device"
				if res.userID != "" {
					psd["userId"] = res.userID
				}
				if qoderMachineID != "" {
					psd["machineId"] = qoderMachineID
				}
			}
			if res.exchangedToken != "" {
				psd["exchangedToken"] = res.exchangedToken
				psd["exchangedExp"] = fmt.Sprintf("%d", res.exchangedExpMs)
			}
			if len(psd) > 0 {
				connectionsMu.Lock()
				for k, v := range psd {
					conn.ProviderSpecificData[k] = v
				}
				saveConnectionsLocked()
				connectionsMu.Unlock()
			}
		}
		providerLoginMu.Lock()
		if pollErr != nil {
			sess.Done, sess.Success, sess.Error = true, false, pollErr.Error()
		} else {
			sess.Done, sess.Success, sess.Connection = true, true, conn
		}
		providerLoginMu.Unlock()
	}()
	return sess, nil
}

// pollProviderDeviceToken 轮询至拿到票：kilo 语义（202/403/410 + status approved）
// 或 RFC 8628（POST form，error 字段 authorization_pending/slow_down/access_denied/expired_token）。
func pollProviderDeviceToken(spec deviceFlowSpec, code string, expiresIn int, verifier string) (*deviceLoginResult, error) {
	interval := spec.IntervalSec
	if interval <= 0 {
		interval = 3
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	client := oauthHTTPClient(15 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(interval) * time.Second)
		var body []byte
		var statusCode int
		if spec.Mode == "rfc8628" {
			form := url.Values{}
			form.Set("client_id", spec.ClientID)
			form.Set("device_code", code)
			form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
			if spec.PKCE && verifier != "" {
				form.Set("code_verifier", verifier)
			}
			req, e := http.NewRequest(http.MethodPost, spec.TokenURL, strings.NewReader(form.Encode()))
			if e != nil {
				return nil, fmt.Errorf("poll failed: %v", e)
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Accept", "application/json")
			resp, e := client.Do(req)
			if e != nil {
				return nil, fmt.Errorf("poll failed: %v", e)
			}
			body, _ = io.ReadAll(io.LimitReader(resp.Body, 256<<10))
			statusCode = resp.StatusCode
			resp.Body.Close()
		} else if spec.Mode == "state-poll" {
			req, e := http.NewRequest(http.MethodGet, spec.TokenURL+"?state="+url.QueryEscape(code), nil)
			if e != nil {
				return nil, fmt.Errorf("poll failed: %v", e)
			}
			for k, v := range statePollHeaders(spec, true) {
				req.Header.Set(k, v)
			}
			resp, e := client.Do(req)
			if e != nil {
				return nil, fmt.Errorf("poll failed: %v", e)
			}
			body, _ = io.ReadAll(io.LimitReader(resp.Body, 256<<10))
			statusCode = resp.StatusCode
			resp.Body.Close()
		} else if spec.Mode == "qoder" {
			q := url.Values{}
			q.Set("nonce", code)
			q.Set("verifier", verifier)
			q.Set("challenge_method", "S256")
			req, e := http.NewRequest(http.MethodGet, spec.TokenURL+"?"+q.Encode(), nil)
			if e != nil {
				return nil, fmt.Errorf("poll failed: %v", e)
			}
			req.Header.Set("Accept", "application/json")
			if spec.UserAgent != "" {
				req.Header.Set("User-Agent", spec.UserAgent)
			}
			resp, e := client.Do(req)
			if e != nil {
				return nil, dialHintError("poll", e)
			}
			body, _ = io.ReadAll(io.LimitReader(resp.Body, 256<<10))
			statusCode = resp.StatusCode
			resp.Body.Close()
			switch statusCode {
			case http.StatusOK:
			case http.StatusAccepted, http.StatusNotFound: // pending
				continue
			default:
				return nil, fmt.Errorf("poll failed: %d %s", statusCode, truncate(strings.TrimSpace(string(body)), 160))
			}
		} else {
			resp, e := client.Get(spec.PollURLBase + "/" + url.PathEscape(code))
			if e != nil {
				return nil, fmt.Errorf("poll failed: %v", e)
			}
			body, _ = io.ReadAll(io.LimitReader(resp.Body, 256<<10))
			statusCode = resp.StatusCode
			resp.Body.Close()
			switch statusCode {
			case http.StatusAccepted: // 202 pending
				continue
			case http.StatusForbidden:
				return nil, fmt.Errorf("authorization denied by user")
			case http.StatusGone:
				return nil, fmt.Errorf("authorization code expired")
			case http.StatusOK:
			default:
				return nil, fmt.Errorf("poll failed: %d %s", statusCode, truncate(strings.TrimSpace(string(body)), 160))
			}
		}
		var data map[string]any
		if e := json.Unmarshal(body, &data); e != nil {
			if statusCode == http.StatusOK {
				return nil, fmt.Errorf("poll decode: %v", e)
			}
			continue // 非 JSON 的过渡态（代理/限流页）继续等
		}
		res := &deviceLoginResult{}
		if spec.Mode == "state-poll" {
			if statusCode != http.StatusOK {
				continue // 过渡态/代理错误：继续轮询直到过期
			}
			c, _ := data["code"].(float64)
			inner, _ := data["data"].(map[string]any)
			switch {
			case c == 11217: // RetryFetchToken：等待用户在浏览器完成授权
				continue
			case c == 0 && inner != nil:
				res.token, _ = inner["accessToken"].(string)
				if res.token == "" {
					continue
				}
				res.refreshToken, _ = inner["refreshToken"].(string)
				if n, okk := inner["expiresIn"].(float64); okk && n > 0 {
					res.expiresAtMs = time.Now().Add(time.Duration(n) * time.Second).UnixMilli()
				} else {
					res.expiresAtMs = time.Now().Add(24 * time.Hour).UnixMilli() // 上游缺省 86400s
				}
			default:
				msg, _ := data["msg"].(string)
				return nil, fmt.Errorf("poll failed: %s", truncate(msg, 160))
			}
		} else if spec.Mode == "rfc8628" {
			switch errCode, _ := data["error"].(string); errCode {
			case "authorization_pending", "slow_down", "":
			case "access_denied", "expired_token":
				return nil, fmt.Errorf("authorization denied or expired (%s)", errCode)
			default:
				desc, _ := data["error_description"].(string)
				return nil, fmt.Errorf("poll failed: %s %s", errCode, truncate(desc, 160))
			}
			res.token, _ = data["access_token"].(string)
			res.refreshToken, _ = data["refresh_token"].(string)
			if res.token == "" {
				continue // 未批准继续等
			}
			if n, okk := data["expires_in"].(float64); okk && n > 0 {
				res.expiresAtMs = time.Now().Add(time.Duration(n) * time.Second).UnixMilli()
			}
		} else if spec.Mode == "qoder" {
			res.token, _ = data["token"].(string)
			if res.token == "" {
				return nil, fmt.Errorf("poll succeeded but response has no token")
			}
			res.refreshToken, _ = data["refresh_token"].(string)
			res.userID, _ = data["user_id"].(string)
			res.expiresAtMs = qoderExpiryMs(data)
		} else {
			if s, _ := data["status"].(string); s != "approved" {
				continue // 未批准继续等
			}
			res.token, _ = data["token"].(string)
			if res.token == "" {
				return nil, fmt.Errorf("approved response missing token")
			}
			res.email, _ = data["userEmail"].(string)
		}
		if spec.ProfileURL != "" {
			preq, _ := http.NewRequest(http.MethodGet, spec.ProfileURL, nil)
			preq.Header.Set("Authorization", "Bearer "+res.token)
			preq.Header.Set("Accept", "application/json")
			if spec.Mode == "qoder" && spec.UserAgent != "" {
				preq.Header.Set("User-Agent", spec.UserAgent)
			}
			if presp, e2 := client.Do(preq); e2 == nil {
				pbody, _ := io.ReadAll(io.LimitReader(presp.Body, 256<<10))
				presp.Body.Close()
				var pdata map[string]any
				if json.Unmarshal(pbody, &pdata) == nil {
					if res.email == "" && spec.EmailPath != "" {
						res.email = jsonPathString(pdata, spec.EmailPath)
					}
					if spec.OrgIDPath != "" {
						res.orgID = jsonPathString(pdata, spec.OrgIDPath)
					}
				}
			}
		}
		if spec.Mode == "qoder" && res.email == "" && res.userID != "" {
			res.email = "qoder-user-" + res.userID
		}
		if spec.ExchangeURL != "" {
			tok, expMs, e := exchangeJobToken(spec, res.token)
			if e != nil {
				return nil, e
			}
			res.exchangedToken, res.exchangedExpMs = tok, expMs
		}
		return res, nil
	}
	return nil, fmt.Errorf("device authorization expired (timeout)")
}

// updateConnectionPSD 按 id 合并写 ProviderSpecificData（调用方无需持锁）。
func updateConnectionPSD(id string, kv map[string]string) {
	loadConnections()
	connectionsMu.Lock()
	defer connectionsMu.Unlock()
	for _, c := range connections {
		if c.ID == id {
			for k, v := range kv {
				c.ProviderSpecificData[k] = v
			}
			saveConnectionsLocked()
			return
		}
	}
}

// connectionBearer 派发/测试用：默认 OAuth access token；带二段兑换的（Copilot）
// 优先用短期作业票，临期自动重兑换并回存。
// refreshRFC8628Connection 标准 OAuth 刷新：rfc8628 模式且有 refreshToken 且临近过期时，
// 向 TokenURL POST grant_type=refresh_token 换取新 access/refresh 并落盘。成功返回 true。
func refreshRFC8628Connection(conn *Connection, spec deviceFlowSpec) bool {
	if spec.TokenURL == "" || conn.RefreshToken == "" {
		return false
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", conn.RefreshToken)
	if spec.ClientID != "" {
		form.Set("client_id", spec.ClientID)
	}
	req, e := http.NewRequest(http.MethodPost, spec.TokenURL, strings.NewReader(form.Encode()))
	if e != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, e := oauthHTTPClient(15 * time.Second).Do(req)
	if e != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var data map[string]any
	if json.Unmarshal(body, &data) != nil {
		return false
	}
	at, _ := data["access_token"].(string)
	if at == "" {
		return false
	}
	rt, _ := data["refresh_token"].(string)
	if rt == "" {
		rt = conn.RefreshToken
	}
	expMs := int64(0)
	if n, ok := data["expires_in"].(float64); ok && n > 0 {
		expMs = time.Now().Add(time.Duration(n) * time.Second).UnixMilli()
	} else {
		expMs = conn.ExpiresAt
	}
	connectionsMu.Lock()
	for _, c := range connections {
		if c.ID == conn.ID {
			c.AccessToken, c.RefreshToken, c.ExpiresAt, c.Status = at, rt, expMs, "active"
			break
		}
	}
	saveConnectionsLocked()
	connectionsMu.Unlock()
	return true
}

func connectionBearer(conn *Connection, spec deviceFlowSpec) string {
	if spec.Mode == "rfc8628" && conn.RefreshToken != "" &&
		conn.ExpiresAt > 0 && time.Now().UnixMilli() >= conn.ExpiresAt-60_000 {
		if refreshRFC8628Connection(conn, spec) {
			connectionsMu.Lock()
			var fresh string
			for _, c := range connections {
				if c.ID == conn.ID {
					fresh = c.AccessToken
					break
				}
			}
			connectionsMu.Unlock()
			if fresh != "" {
				return fresh
			}
		}
	}
	if spec.ExchangeURL == "" {
		return conn.AccessToken
	}
	exp, _ := strconv.ParseInt(conn.ProviderSpecificData["exchangedExp"], 10, 64)
	if t := conn.ProviderSpecificData["exchangedToken"]; t != "" && time.Now().UnixMilli() < exp-30_000 {
		return t
	}
	if t, expMs, err := exchangeJobToken(spec, conn.AccessToken); err == nil {
		updateConnectionPSD(conn.ID, map[string]string{"exchangedToken": t, "exchangedExp": fmt.Sprintf("%d", expMs)})
		return t
	}
	return conn.AccessToken // 兑换失败让上游 401 → reauth 标记兜底
}

// ---------- admin handlers ----------

func handleConnectionsList(w http.ResponseWriter, r *http.Request) {
	loadConnections()
	connectionsMu.Lock()
	out := make([]Connection, 0, len(connections))
	for _, c := range connections {
		cp := *c
		cp.AccessToken = "" // 列表不外泄 token
		out = append(out, cp)
	}
	connectionsMu.Unlock()
	flows := make([]string, 0, len(providerDeviceFlows))
	for p := range providerDeviceFlows {
		flows = append(flows, p)
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"connections": out, "flows": flows}})
}

// POST /admin/api/connections/login/start  body: {provider}
func handleConnectionLoginStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	sess, err := startProviderDeviceLogin(strings.TrimSpace(req.Provider))
	if err != nil {
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"error": err.Error()}})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"sessionId": sess.ID, "userCode": sess.UserCode, "authUrl": sess.AuthURL,
	}})
}

// GET /admin/api/connections/login/status?sessionId=x
func handleConnectionLoginStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("sessionId")
	providerLoginMu.Lock()
	sess, ok := providerLoginSessions[id]
	var out map[string]any
	if ok {
		out = map[string]any{"done": sess.Done, "success": sess.Success, "error": sess.Error, "userCode": sess.UserCode, "authUrl": sess.AuthURL}
		if sess.Connection != nil {
			cp := *sess.Connection
			cp.AccessToken = ""
			out["connection"] = cp
		}
	}
	providerLoginMu.Unlock()
	if !ok {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "session not found"})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: out})
}

// POST /admin/api/connections/import  body: {provider, accessToken, email?, orgId?}
// 粘贴 token 路径（Kilo 这类无 refresh 的上游同样适用），复用去重与状态语义。
func handleConnectionImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider    string `json:"provider"`
		AccessToken string `json:"accessToken"`
		Email       string `json:"email"`
		OrgID       string `json:"orgId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Provider == "" || req.AccessToken == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "provider and accessToken required"})
		return
	}
	conn := upsertConnection(&Connection{
		Provider: req.Provider, AuthType: "import_token", Email: req.Email,
		AccessToken: req.AccessToken, Status: "active",
	})
	if req.OrgID != "" {
		connectionsMu.Lock()
		conn.ProviderSpecificData["orgId"] = req.OrgID
		saveConnectionsLocked()
		connectionsMu.Unlock()
	}
	cp := *conn
	cp.AccessToken = ""
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"connection": cp}})
}

// POST /admin/api/connections/delete  body: {id}
func handleConnectionDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	if !removeConnection(req.ID) {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "connection not found"})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// POST /admin/api/connections/test  body: {id}
// bearer + 动态头探测 ModelsTestURL；同时把 401/403 反馈到 reauth 标记。
func handleConnectionTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	loadConnections()
	connectionsMu.Lock()
	var conn *Connection
	for _, c := range connections {
		if c.ID == req.ID {
			cp := *c
			conn = &cp
			break
		}
	}
	connectionsMu.Unlock()
	if conn == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "connection not found"})
		return
	}
	spec, ok := providerDeviceFlows[conn.Provider]
	if !ok || spec.ModelsTestURL == "" {
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"ok": false, "error": "no test endpoint for provider " + conn.Provider}})
		return
	}
	if conn.ExpiresAt > 0 && time.Now().UnixMilli() >= conn.ExpiresAt && conn.RefreshToken == "" {
		markConnectionReauth(conn.ID)
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"ok": false, "error": "token expired; please re-login"}})
		return
	}
	hreq, _ := http.NewRequest(http.MethodGet, spec.ModelsTestURL, nil)
	hreq.Header.Set("Authorization", "Bearer "+connectionBearer(conn, spec))
	if spec.UserAgent != "" {
		hreq.Header.Set("User-Agent", spec.UserAgent)
	}
	if spec.OrgHeader != "" && conn.ProviderSpecificData["orgId"] != "" {
		hreq.Header.Set(spec.OrgHeader, conn.ProviderSpecificData["orgId"])
	}
	resp, err := oauthHTTPClient(20 * time.Second).Do(hreq)
	if err != nil {
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"ok": false, "error": err.Error()}})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		markConnectionReauth(conn.ID)
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"ok": resp.StatusCode == http.StatusOK, "status": resp.StatusCode,
		"body": truncate(strings.TrimSpace(string(body)), 240),
	}})
}
