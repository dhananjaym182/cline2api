package main

// ============================================================================
// §8.3 Provider Connections：provider 作用域的登录凭据（与 Cline 账号池并存）。
// 引擎按 flowSpec 数据驱动（对齐 ExtremeRouter 的五种 flowType 中最常用的
// device_code），新增一个 provider 登录 = 往 providerDeviceFlows 加一条数据，
// 不新增控制流。Kilo Code 为首个具体移植（§8.3.2，步骤 4）。
// ============================================================================

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"cline-go-proxy/internal/deviceflow"
	connectionstore "cline-go-proxy/internal/store/connections"
)

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
	list, err := connectionstore.Load(connectionsFilePath)
	if err != nil {
		backupCorruptFile(connectionsFilePath, err)
		return
	}
	connections = list
}

// saveConnectionsLocked 调用方持 connectionsMu。
func saveConnectionsLocked() {
	if err := connectionstore.Save(connectionsFilePath, connections); err != nil {
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

// deviceFlowSpec aliases the deviceflow package spec so existing call sites and
// tests can keep using the local name while the engine lives in its own package.
type deviceFlowSpec = deviceflow.Spec

// providerDeviceFlows is the mutable flow registry. Built-in specs come from
// the deviceflow package; tests may register additional entries.
var providerDeviceFlows = deviceflow.Specs()

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

// jsonPathString resolves a dotted JSON path; implementation lives in deviceflow.
func jsonPathString(body map[string]any, path string) string {
	return deviceflow.JSONPathString(body, path)
}

// deviceLoginResult 两种轮询模式（kilo / rfc8628）的统一成功形态。
type deviceLoginResult struct {
	token, refreshToken, email, orgID string
	userID                            string // qoder：上游 user_id，用于邮箱缺失时的账号标识
	expiresAtMs                       int64  // 0 = 不过期（Kilo 语义）
	exchangedToken                    string
	exchangedExpMs                    int64
}

func newUUIDv4() (string, error) { return deviceflow.NewUUIDv4() }

func newPKCE() (verifier, challenge string, err error) { return deviceflow.NewPKCE() }

func statePollHeaders(spec deviceFlowSpec, isPoll bool) map[string]string {
	return deviceflow.StatePollHeaders(spec, isPoll)
}

// exchangeJobToken performs the optional second exchange (GitHub Copilot).
func exchangeJobToken(spec deviceFlowSpec, accessToken string) (token string, expMs int64, err error) {
	return deviceflow.ExchangeJobToken(context.Background(), oauthHTTPClient(15*time.Second), spec, accessToken)
}

// oauthHTTPClient routes auth traffic through the shared outbound transport
// (SOCKS5 egress proxy pool + custom dialer) like proxied chat traffic; a bare
// &http.Client{} would bypass the configured egress proxy entirely.
func oauthHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: httpClient.Transport}
}

func qoderExpiryMs(data map[string]any) int64 { return deviceflow.QoderExpiryMs(data) }

func epochToMs(n int64) int64 { return deviceflow.EpochToMs(n) }

// startProviderDeviceLogin initiates a flow, registers a session, and polls in
// the background. Persistence stays here; the deviceflow package only performs
// the upstream authentication exchange.
func startProviderDeviceLogin(provider string) (*providerLoginSession, error) {
	spec, ok := providerDeviceFlows[provider]
	if !ok {
		return nil, fmt.Errorf("no device_code flow registered for provider %q", provider)
	}
	init, err := deviceflow.Initiate(context.Background(), oauthHTTPClient(15*time.Second), spec)
	if err != nil {
		return nil, err
	}
	sess := &providerLoginSession{
		ID:        fmt.Sprintf("plog_%d_%s", time.Now().UnixNano(), provider),
		Provider:  provider,
		UserCode:  init.UserCode,
		AuthURL:   init.VerificationURL,
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
		res, pollErr := pollProviderDeviceToken(spec, init.Code, init.ExpiresIn, init.Verifier)
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
				if init.MachineID != "" {
					psd["machineId"] = init.MachineID
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

// pollProviderDeviceToken polls until a token arrives and maps the deviceflow
// package result onto the in-package result shape.
func pollProviderDeviceToken(spec deviceFlowSpec, code string, expiresIn int, verifier string) (*deviceLoginResult, error) {
	res, err := deviceflow.Poll(context.Background(), oauthHTTPClient(15*time.Second), spec, &deviceflow.Initiation{
		Code: code, ExpiresIn: expiresIn, Verifier: verifier,
	})
	if err != nil {
		return nil, err
	}
	return &deviceLoginResult{
		token: res.Token, refreshToken: res.RefreshToken, email: res.Email,
		orgID: res.OrgID, userID: res.UserID, expiresAtMs: res.ExpiresAtMs,
		exchangedToken: res.ExchangedToken, exchangedExpMs: res.ExchangedExpMs,
	}, nil
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
// refreshRFC8628Connection performs a standard OAuth refresh via deviceflow and
// persists the new tokens. Returns false when the upstream cannot refresh.
func refreshRFC8628Connection(conn *Connection, spec deviceFlowSpec) bool {
	at, rt, expMs, ok := deviceflow.RefreshRFC8628(
		context.Background(), oauthHTTPClient(15*time.Second), spec, conn.RefreshToken, conn.ExpiresAt)
	if !ok {
		return false
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
