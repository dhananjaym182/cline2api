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
	defer connectionsMu.Unlock()
	for i, c := range connections {
		if c.ID == id {
			connections = append(connections[:i], connections[i+1:]...)
			saveConnectionsLocked()
			return true
		}
	}
	return false
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

// activeConnectionForProvider callProvider 用：连接存在、active、未过期才返回。
// 过期且无 refreshToken → 就地标 reauth_required 并返回 nil。
func activeConnectionForProvider(p *CustomProvider) *Connection {
	if p.ConnectionID == "" {
		return nil
	}
	loadConnections()
	connectionsMu.Lock()
	var out *Connection
	for _, c := range connections {
		if c.ID == p.ConnectionID && c.Status == "active" {
			if c.ExpiresAt > 0 && time.Now().UnixMilli() >= c.ExpiresAt {
				if c.RefreshToken == "" {
					c.Status = "reauth_required"
					saveConnectionsLocked()
					break
				}
			}
			cp := *c
			out = &cp
			break
		}
	}
	connectionsMu.Unlock()
	return out
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

	// rfc8628 模式（GitHub / Qwen Portal）：POST form 授权 + 轮询，error 字段表状态
	Mode          string // "" | "kilo" | "rfc8628"
	TokenURL      string // rfc8628 轮询端点（POST x-www-form-urlencoded）
	ClientID      string
	Scope         string
	PKCE          bool   // 授权请求带 code_challenge(S256)，轮询带 code_verifier
	UserCodeField string // 用户码字段名（默认 "user_code"；kilo 为 "code"）

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
	expiresAtMs                       int64 // 0 = 不过期（Kilo 语义）
	exchangedToken                    string
	exchangedExpMs                    int64
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

// exchangeJobToken 二段兑换（GitHub Copilot）：OAuth token → 短期作业票。
func exchangeJobToken(spec deviceFlowSpec, accessToken string) (token string, expMs int64, err error) {
	req, e := http.NewRequest(http.MethodGet, spec.ExchangeURL, nil)
	if e != nil {
		return "", 0, e
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, e := (&http.Client{Timeout: 15 * time.Second}).Do(req)
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

// startProviderDeviceLogin  initiate → 会话登记 → 后台轮询 goroutine。
func startProviderDeviceLogin(provider string) (*providerLoginSession, error) {
	spec, ok := providerDeviceFlows[provider]
	if !ok {
		return nil, fmt.Errorf("no device_code flow registered for provider %q", provider)
	}
	var verifier string
	integ := func() (code, userDisp, verifURL string, expiresIn int, err error) {
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
			resp, e := (&http.Client{Timeout: 15 * time.Second}).Do(req)
			if e != nil {
				return "", "", "", 0, fmt.Errorf("initiate failed: %v", e)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
			if resp.StatusCode != http.StatusOK {
				return "", "", "", 0, fmt.Errorf("device auth initiation failed: %d %s", resp.StatusCode, truncate(strings.TrimSpace(string(body)), 200))
			}
			var data map[string]any
			if e := json.Unmarshal(body, &data); e != nil {
				return "", "", "", 0, fmt.Errorf("initiate decode: %v", e)
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
		req, e := http.NewRequest(http.MethodPost, spec.InitiateURL, bytes.NewReader([]byte("{}")))
		if e != nil {
			return "", "", "", 0, e
		}
		req.Header.Set("Content-Type", "application/json")
		resp, e := (&http.Client{Timeout: 15 * time.Second}).Do(req)
		if e != nil {
			return "", "", "", 0, fmt.Errorf("initiate failed: %v", e)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		if resp.StatusCode == http.StatusTooManyRequests {
			return "", "", "", 0, fmt.Errorf("too many pending authorization requests, try again later")
		}
		if resp.StatusCode != http.StatusOK {
			return "", "", "", 0, fmt.Errorf("device auth initiation failed: %d %s", resp.StatusCode, truncate(strings.TrimSpace(string(body)), 200))
		}
		var data map[string]any
		if e := json.Unmarshal(body, &data); e != nil {
			return "", "", "", 0, fmt.Errorf("initiate decode: %v", e)
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
	client := &http.Client{Timeout: 15 * time.Second}
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
		if spec.Mode == "rfc8628" {
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
func connectionBearer(conn *Connection, spec deviceFlowSpec) string {
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
	if spec.OrgHeader != "" && conn.ProviderSpecificData["orgId"] != "" {
		hreq.Header.Set(spec.OrgHeader, conn.ProviderSpecificData["orgId"])
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(hreq)
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
