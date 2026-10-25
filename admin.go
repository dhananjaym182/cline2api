package main

import (
	"log"
	"net/http"
	"sync"
	"time"

	"cline-go-proxy/internal/adminapi"
	"cline-go-proxy/internal/config"
	"cline-go-proxy/internal/httpx"
)

type apiResponse = httpx.Response

func writeAPI(w http.ResponseWriter, status int, resp apiResponse) {
	httpx.WriteAPI(w, status, resp)
}

// 管理后台登录会话（内存态，程序重启后需重新登录）。
var adminSessions = adminapi.NewSessionStore()

const adminSessionCookie = adminapi.SessionCookieName

// adminAuth 连接根接线与 adminapi：密码状态来自账号池，错误文案走 i18n。
var adminAuth = &adminapi.Auth{
	Sessions:    adminSessions,
	PasswordSet: func() bool { return loadPool().AdminPasswordHash != "" },
	Unauthorized: func(w http.ResponseWriter, r *http.Request, reason string) {
		writeAPI(w, http.StatusUnauthorized, apiResponse{Error: tAPI(r, reason)})
	},
}

// adminAuthHandlers connect login/logout/password endpoints to the pool-backed
// password store while adminapi owns the wire behavior.
var adminAuthHandlers = &adminapi.AuthHandlers{
	Sessions:       adminSessions,
	PasswordSet:    func() bool { return loadPool().AdminPasswordHash != "" },
	VerifyPassword: verifyAdminPassword,
	SetPassword:    setAdminPassword,
	Translate:      func(r *http.Request, key string) string { return tAPI(r, key) },
}

// adminKeys/adminMonitoring connect the root composition to adminapi handlers
// without moving pool locking or persistence into the package.
var adminKeys = &adminapi.KeyManager{
	ListKeys: func() []string { return loadPool().Keys },
	AddKey: func(key string) {
		p := loadPool()
		poolMu.Lock()
		p.Keys = append(p.Keys, key)
		poolMu.Unlock()
		savePool()
	},
	DeleteKey: func(key string) {
		p := loadPool()
		poolMu.Lock()
		for i, k := range p.Keys {
			if k == key {
				p.Keys = append(p.Keys[:i], p.Keys[i+1:]...)
				break
			}
		}
		poolMu.Unlock()
		savePool()
	},
	Translate: func(r *http.Request, key string) string { return tAPI(r, key) },
}

var adminMonitoring = &adminapi.Monitoring{
	Accounts:      func() []*Account { return loadPool().Accounts },
	Strategy:      func() string { return getProxyConfig().Strategy },
	ModelChain:    func() []string { return getProxyConfig().ModelChain },
	Version:       func() string { return appVersion },
	StartedAt:     func() time.Time { return serverStartedAt },
	OpencodeToday: func() any { return opencodeUsageToday() },
	ListRequestLogs: func(limit int, cursor string) (any, error) {
		return listRequestLogs(limit, cursor)
	},
	Translate: func(r *http.Request, key string) string { return tAPI(r, key) },
}

var adminOpencode = &adminapi.Opencode{
	Get:            getZenConfig,
	Set:            setZenConfig,
	ValidateProxy:  validateProxyList,
	MaskProxy:      maskProxyURL,
	ProxyCooldowns: zenProxyCooldownStatus,
	FailedNow:      zenFailedNow,
	SyncedModels:   func() int { return len(currentZenModels()) },
	LastSync:       func() any { return lastZenModelSync() },
	Translate:      func(r *http.Request, key string) string { return tAPI(r, key) },
}

var adminClineEgress = &adminapi.ClineEgress{
	Get:           getClineProxyConfig,
	Set:           setClineProxyConfig,
	PersistErr:    getClineProxyPersistErr,
	ValidateProxy: validateProxyList,
	MaskProxy:     maskProxyURL,
	Translate:     func(r *http.Request, key string) string { return tAPI(r, key) },
}

func registerAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/", adminStaticHandler)
	// 无需登录的接口
	mux.HandleFunc("/admin/api/login", corsHandler(handleAdminLogin))
	mux.HandleFunc("/admin/api/logout", corsHandler(handleAdminLogout))
	// 其余 API 全部需要后台鉴权（设置了密码后）
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return requireAdminAuth(corsHandler(h))
	}
	mux.HandleFunc("/admin/api/admin-sessions/clear", auth(handleAdminSessionsClear))
	mux.HandleFunc("/admin/api/accounts", auth(handleAdminAccounts))
	mux.HandleFunc("/admin/api/accounts/add", auth(handleAdminAccountAdd))
	mux.HandleFunc("/admin/api/accounts/delete", auth(handleAdminAccountDelete))
	mux.HandleFunc("/admin/api/accounts/export", auth(handleExportAccounts))
	mux.HandleFunc("/admin/api/oauth/start", auth(handleOAuthStart))
	mux.HandleFunc("/admin/api/oauth/status", auth(handleOAuthStatus))
	mux.HandleFunc("/admin/api/sso/import", auth(handleSSOImport))
	mux.HandleFunc("/admin/api/stats", auth(handleAdminStats))
	mux.HandleFunc("/admin/api/dashboard/overview", auth(handleDashboardOverview))
	mux.HandleFunc("/admin/api/batch-import", auth(handleBatchImport))
	mux.HandleFunc("/admin/api/accounts/refresh-all", auth(handleAdminRefreshAll))
	mux.HandleFunc("/admin/api/accounts/delete-all", auth(handleAdminDeleteAll))
	mux.HandleFunc("/admin/api/accounts/reset", auth(handleAdminAccountReset))
	mux.HandleFunc("/admin/api/accounts/test", auth(handleAdminAccountTest))
	mux.HandleFunc("/admin/api/keys", auth(handleAdminGetKeys))
	mux.HandleFunc("/admin/api/keys/generate", auth(handleAdminGenerateKey))
	mux.HandleFunc("/admin/api/keys/delete", auth(handleAdminDeleteKey))
	mux.HandleFunc("/admin/api/models", auth(handleAdminModels))
	mux.HandleFunc("/admin/api/models/sync", auth(handleAdminModelSync))
	mux.HandleFunc("/admin/api/opencode/config", auth(handleOpenCodeConfig))
	mux.HandleFunc("/admin/api/opencode/config/update", auth(handleOpenCodeConfigUpdate))
	mux.HandleFunc("/admin/api/opencode/models/sync", auth(handleOpenCodeModelSync))
	mux.HandleFunc("/admin/api/cline-proxy/config", auth(handleClineProxyConfig))
	mux.HandleFunc("/admin/api/cline-proxy/config/update", auth(handleClineProxyConfigUpdate))
	mux.HandleFunc("/admin/api/models/add", auth(handleAdminModelAdd))
	mux.HandleFunc("/admin/api/models/delete", auth(handleAdminModelDelete))
	mux.HandleFunc("/admin/api/models/context", auth(handleAdminModelContext))
	mux.HandleFunc("/admin/api/models/capabilities", auth(handleAdminModelCapabilities))
	mux.HandleFunc("/admin/api/models/toggle", auth(handleAdminModelToggle))
	mux.HandleFunc("/admin/api/models/test", auth(handleAdminModelTest))
	mux.HandleFunc("/admin/api/config", auth(handleAdminConfig))
	mux.HandleFunc("/admin/api/config/update", auth(handleAdminUpdateConfig))
	mux.HandleFunc("/admin/api/providers", auth(handleProvidersList))
	mux.HandleFunc("/admin/api/providers/save", auth(handleProviderSave))
	mux.HandleFunc("/admin/api/providers/delete", auth(handleProviderDelete))
	mux.HandleFunc("/admin/api/providers/test", auth(handleProviderTest))
	mux.HandleFunc("/admin/api/providers/models", auth(handleProviderModels))
	mux.HandleFunc("/admin/api/providers/presets", auth(handleProviderPresets))
	mux.HandleFunc("/admin/api/providers/recheck", auth(handleProviderRecheck))
	mux.HandleFunc("/admin/api/providers/sync-status", auth(handleProviderSyncStatus))
	mux.HandleFunc("/admin/api/models/import/preview", auth(handleModelImportPreview))
	mux.HandleFunc("/admin/api/models/import/apply", auth(handleModelImportApply))
	mux.HandleFunc("/admin/api/models/import/pending", auth(handleModelImportPending))
	mux.HandleFunc("/admin/api/models/import/batch", auth(handleModelImportBatch))
	mux.HandleFunc("/admin/api/models/import/discard", auth(handleModelImportDiscard))
	mux.HandleFunc("/admin/api/models/cost", auth(handleAdminModelCost))
	mux.HandleFunc("/admin/api/connections", auth(handleConnectionsList))
	mux.HandleFunc("/admin/api/connections/login/start", auth(handleConnectionLoginStart))
	mux.HandleFunc("/admin/api/connections/login/status", auth(handleConnectionLoginStatus))
	mux.HandleFunc("/admin/api/connections/import", auth(handleConnectionImport))
	mux.HandleFunc("/admin/api/connections/delete", auth(handleConnectionDelete))
	mux.HandleFunc("/admin/api/connections/test", auth(handleConnectionTest))
	mux.HandleFunc("/admin/api/combos", auth(handleCombosList))
	mux.HandleFunc("/admin/api/combos/save", auth(handleComboSave))
	mux.HandleFunc("/admin/api/combos/delete", auth(handleComboDelete))
	mux.HandleFunc("/admin/api/combos/toggle", auth(handleComboToggle))
	mux.HandleFunc("/admin/api/combos/clone", auth(handleComboClone))
	mux.HandleFunc("/admin/api/combos/test", auth(handleComboTest))
	mux.HandleFunc("/admin/api/password", auth(handleAdminPassword))
	mux.HandleFunc("/admin/api/request-logs", auth(handleAdminRequestLogs))
	mux.HandleFunc("/admin/api/request-logs/clear", auth(handleAdminRequestLogsClear))
	mux.HandleFunc("/admin/api/providers/cooldown-reset", auth(handleProviderCooldownReset))
	mux.HandleFunc("/admin/api/account-health", auth(handleAccountHealth))
	mux.HandleFunc("/admin/api/version-check", auth(handleAdminVersionCheck))
	mux.HandleFunc("/admin/api/open-external", auth(handleOpenExternal))
}

// requireAdminAuth 后台访问鉴权中间件：未设置密码直接放行，否则校验会话 cookie。
func requireAdminAuth(next http.HandlerFunc) http.HandlerFunc {
	return adminAuth.Middleware(next)
}

// hashAdminPassword 生成加盐密码哈希：hex(sha256(salt+password))。
func hashAdminPassword(saltHex, password string) string {
	return adminapi.HashPassword(saltHex, password)
}

// setAdminPassword 设置/修改/清除后台密码（空 = 清除），并清空所有会话强制重新登录。
func setAdminPassword(password string) {
	p := loadPool()
	poolMu.Lock()
	if password == "" {
		p.AdminPasswordHash = ""
		p.AdminPasswordSalt = ""
	} else {
		salt, err := adminapi.NewSalt()
		if err != nil {
			salt = time.Now().Format("20060102150405")
		}
		p.AdminPasswordSalt = salt
		p.AdminPasswordHash = hashAdminPassword(p.AdminPasswordSalt, password)
	}
	poolMu.Unlock()
	savePool()
	adminSessions.Clear()
}

// verifyAdminPassword 校验后台密码（未设置密码时返回 false）。
func verifyAdminPassword(password string) bool {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	return adminapi.VerifyPassword(p.AdminPasswordSalt, p.AdminPasswordHash, password)
}

// POST /admin/api/login  body: {password}
func handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	adminAuthHandlers.Login(w, r)
}

// POST /admin/api/logout
func handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	adminAuthHandlers.Logout(w, r)
}

// POST /admin/api/admin-sessions/clear 清空全部后台登录会话（含当前浏览器），
// 用于共享机器上强制所有人重新登录。
func handleAdminSessionsClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	adminSessions.Clear()
	log.Printf("admin: cleared all admin login sessions")
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// POST /admin/api/password  body: {password}（空 = 清除密码，恢复无密码访问）
func handleAdminPassword(w http.ResponseWriter, r *http.Request) {
	adminAuthHandlers.Password(w, r)
}

func adminStaticHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/admin/" || r.URL.Path == "/admin" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// 管理页随二进制整版发布：禁止浏览器缓存，避免升级后停在旧版 UI
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(adminHTML))
		return
	}
	http.NotFound(w, r)
}

// GET /admin/api/accounts
func handleAdminAccounts(w http.ResponseWriter, r *http.Request) {
	adminAccounts.List(w, r)
}

// POST /admin/api/accounts/add  body: { refreshToken, email }
func handleAdminAccountAdd(w http.ResponseWriter, r *http.Request) {
	adminAccounts.Add(w, r)
}

// POST /admin/api/accounts/delete  body: { accountId }
func handleAdminAccountDelete(w http.ResponseWriter, r *http.Request) {
	adminAccounts.Delete(w, r)
}

// POST /admin/api/oauth/start  -- Start OAuth device login, returns URL
func handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	adminOAuth.Start(w, r)
}

// GET /admin/api/oauth/status?sessionId=xxx
func handleOAuthStatus(w http.ResponseWriter, r *http.Request) {
	adminOAuth.Status(w, r)
}

// POST /admin/api/sso/import  body: { ssoCookies: string, email?: string }
func handleSSOImport(w http.ResponseWriter, r *http.Request) {
	adminImports.SSOImport(w, r)
}

// POST /admin/api/batch-import  body: { tokens: [{ refreshToken, email }] }
func handleBatchImport(w http.ResponseWriter, r *http.Request) {
	adminImports.BatchImport(w, r)
}

// GET /admin/api/accounts/export — 导出账号为批量导入兼容格式
// POST body: { ids: [accountId...] } — 仅导出勾选的账号（管理页勾选导出）
func handleExportAccounts(w http.ResponseWriter, r *http.Request) {
	adminAccountOps.Export(w, r)
}

// GET /admin/api/open-external?url=... — 用系统默认浏览器打开外部链接
func handleOpenExternal(w http.ResponseWriter, r *http.Request) {
	adminAccountOps.OpenExternal(w, r)
}

// POST /admin/api/accounts/refresh-all
func handleAdminRefreshAll(w http.ResponseWriter, r *http.Request) {
	adminAccountOps.RefreshAll(w, r)
}

// POST /admin/api/accounts/delete-all
func handleAdminDeleteAll(w http.ResponseWriter, r *http.Request) {
	adminAccountOps.DeleteAll(w, r)
}

// POST /admin/api/accounts/reset  body: { accountId }
func handleAdminAccountReset(w http.ResponseWriter, r *http.Request) {
	adminAccountOps.Reset(w, r)
}

// POST /admin/api/accounts/test  body: { accountId?: "" }
func handleAdminAccountTest(w http.ResponseWriter, r *http.Request) {
	adminAccountOps.Test(w, r)
}

// Global proxy config (mutable via API, persisted to .cline-config.json)
var (
	proxyConfig   = loadProxyConfigFromDisk()
	proxyConfigMu sync.Mutex
)

type proxyConfigData = config.Proxy

func defaultProxyConfig() *proxyConfigData {
	return config.DefaultProxy()
}

const proxyConfigPath = config.ProxyPath

// loadProxyConfigFromDisk 启动时加载持久化的代理配置（轮询策略/请求头），
// 文件不存在或损坏时回退默认值（损坏文件改名备份，不静默覆盖）。
func loadProxyConfigFromDisk() *proxyConfigData {
	path := resolveDataPath(proxyConfigPath)
	cfg, err := config.LoadProxy(path)
	if err != nil {
		backupCorruptFile(path, err)
	}
	return cfg
}

// saveProxyConfigLocked 落盘当前配置（调用方需持有 proxyConfigMu）。
func saveProxyConfigLocked() {
	if err := config.SaveProxy(resolveDataPath(proxyConfigPath), proxyConfig); err != nil {
		log.Printf("proxy config save failed: %v", err)
	}
}

func getProxyConfig() *proxyConfigData {
	proxyConfigMu.Lock()
	defer proxyConfigMu.Unlock()
	return proxyConfig
}

func setProxyConfig(c *proxyConfigData) {
	proxyConfigMu.Lock()
	defer proxyConfigMu.Unlock()
	proxyConfig = c
	saveProxyConfigLocked()
}

// GET /admin/api/keys
func handleAdminGetKeys(w http.ResponseWriter, r *http.Request) {
	adminKeys.Get(w, r)
}

// POST /admin/api/keys/generate
func handleAdminGenerateKey(w http.ResponseWriter, r *http.Request) {
	adminKeys.Generate(w, r)
}

// POST /admin/api/keys/delete  body: { key }
func handleAdminDeleteKey(w http.ResponseWriter, r *http.Request) {
	adminKeys.Delete(w, r)
}

// GET /admin/api/config
func handleAdminConfig(w http.ResponseWriter, r *http.Request) {
	adminConfig.Get(w, r)
}

// POST /admin/api/config  body: { strategy?, headers?, defaultModel?, host? }
func handleAdminUpdateConfig(w http.ResponseWriter, r *http.Request) {
	adminConfig.Update(w, r)
}

// GET /admin/api/models
func handleAdminModels(w http.ResponseWriter, r *http.Request) {
	adminModels.List(w, r)
}

// POST /admin/api/models/add  body: { id, provider?, cost? }
func handleAdminModelAdd(w http.ResponseWriter, r *http.Request) {
	adminModels.Add(w, r)
}

// POST /admin/api/models/delete  body: { id }
func handleAdminModelDelete(w http.ResponseWriter, r *http.Request) {
	adminModels.Delete(w, r)
}

// POST /admin/api/models/context  body: { id, context, output }
// 手动设置模型的上下文窗口 / 最大输出 token（压缩阈值与 maybeCompact 按此计算）。
// 0 = 清除为未知（zen 同步会回填默认值）。设置后 zen 模型同步保留该值不再覆盖。
func handleAdminModelContext(w http.ResponseWriter, r *http.Request) {
	adminModels.UpdateContext(w, r)
}

// POST /admin/api/models/capabilities  body: { id, thinkingLevels?, inputModalities?, outputModalities? }
// 声明模型的能力元数据（推理档位/输入输出模态）。未提供的字段保持不变，
// 显式空数组清除该字段。推理请求带不支持的档位时在入口被丢弃。
func handleAdminModelCapabilities(w http.ResponseWriter, r *http.Request) {
	adminModels.UpdateCapabilities(w, r)
}

// POST /admin/api/models/toggle  body: { ids: [...], disabled }
// 启用/停用模型（单个或批量）：停用后不进客户端模型列表、直接请求被拒绝、
// 默认模型与回退链自动跳过；同步保留该标记。停用当前默认模型时清空默认。
func handleAdminModelToggle(w http.ResponseWriter, r *http.Request) {
	adminModels.Toggle(w, r)
}

// GET /admin/api/stats
func handleAdminStats(w http.ResponseWriter, r *http.Request) {
	adminMonitoring.Stats(w, r)
}

// GET /admin/api/request-logs?limit=50&cursor=...
func handleAdminRequestLogs(w http.ResponseWriter, r *http.Request) {
	adminMonitoring.Logs(w, r)
}

// GET /admin/api/opencode/config — opencode zen 配置 + 运行状态
func handleOpenCodeConfig(w http.ResponseWriter, r *http.Request) {
	adminOpencode.Config(w, r)
}

// POST /admin/api/opencode/config/update — 更新 opencode zen 配置（指针式补丁）
func handleOpenCodeConfigUpdate(w http.ResponseWriter, r *http.Request) {
	adminOpencode.Update(w, r)
}

// GET /admin/api/cline-proxy/config — Cline 出口代理配置（代理地址脱敏返回）
func handleClineProxyConfig(w http.ResponseWriter, r *http.Request) {
	adminClineEgress.Config(w, r)
}

// POST /admin/api/cline-proxy/config/update — 更新 Cline 出口代理配置
func handleClineProxyConfigUpdate(w http.ResponseWriter, r *http.Request) {
	adminClineEgress.Update(w, r)
}

// POST /admin/api/opencode/models/sync — 手动触发一次 opencode 模型同步
func handleOpenCodeModelSync(w http.ResponseWriter, r *http.Request) {
	adminSync.ZenModels(w, r)
}
