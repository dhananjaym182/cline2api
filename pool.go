package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	pool     *AccountPool
	poolMu   sync.Mutex
	poolPath string
)

func init() {
	poolPath = resolveDataPath(".cline-accounts.json")
}

// dataDirEnv 数据目录环境变量。设置后所有数据文件固定存放在该目录（最高优先级），
// Docker 部署依赖它把数据落到挂载卷（Dockerfile 中 ENV CLINE2API_DATA_DIR=/app/data），
// 否则数据会写进容器可写层、重建容器即丢失。
const dataDirEnv = "CLINE2API_DATA_DIR"

// knownDataFiles 全部已知数据文件名，切换数据目录时用于旧数据的一次性迁移。
var knownDataFiles = []string{
	".cline-accounts.json",
	".cline-config.json",
	".cline-providers.json",
	".cline-request-logs.json",
	".cline-zen.json",
	".cline-proxy.json",
	".cline-credentials.json",
}

// migrateOnce 保证旧数据迁移在进程生命周期内只执行一次。
var migrateOnce sync.Once

// envDataDir 返回环境变量指定的数据目录（未设置返回空串），并确保目录存在。
func envDataDir() string {
	dir := strings.TrimSpace(os.Getenv(dataDirEnv))
	if dir == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Printf("Failed to create data dir %s: %v", dir, err)
	}
	return dir
}

// resolveDataPath 解析数据文件路径：
//  1. 设置了 CLINE2API_DATA_DIR 时固定使用该目录（首次调用会把旧位置的已有数据迁移过来）；
//  2. 未设置时按优先级查找：exe 目录 → 工作目录 → 用户主目录。
//     找到则用该路径（兼容旧版本在项目根目录存储的文件）；
//     都找不到则回退到 exe 目录（首次运行在该位置创建）。
func resolveDataPath(filename string) string {
	// 0. 环境变量指定的数据目录（Docker 卷挂载点）
	if dir := envDataDir(); dir != "" {
		migrateOnce.Do(func() { migrateLegacyDataFiles(dir) })
		return filepath.Join(dir, filename)
	}
	// 1. exe 所在目录
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), filename)
		if fileExists(p) {
			return p
		}
	}
	// 2. 当前工作目录
	if pwd, err := os.Getwd(); err == nil {
		p := filepath.Join(pwd, filename)
		if fileExists(p) {
			return p
		}
	}
	// 3. 用户主目录下的 .cline2api/
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".cline2api", filename)
		if fileExists(p) {
			return p
		}
	}
	// 回退：exe 目录（首次运行在此创建）
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), filename)
	}
	pwd, _ := os.Getwd()
	return filepath.Join(pwd, filename)
}

// currentDataDir 返回当前生效的数据目录（新文件将创建的位置），用于启动日志与排查。
func currentDataDir() string {
	if dir := envDataDir(); dir != "" {
		return dir
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	pwd, _ := os.Getwd()
	return pwd
}

// legacyDataDirsOverride 仅供测试替换旧目录探测逻辑（nil 时使用默认探测）。
var legacyDataDirsOverride func() []string

// legacyDataCandidates 返回旧版本可能存放数据文件的目录（去重后的绝对路径）。
func legacyDataCandidates() []string {
	if legacyDataDirsOverride != nil {
		return legacyDataDirsOverride()
	}
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if pwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, pwd)
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".cline2api"))
	}
	seen := make(map[string]bool, len(dirs))
	out := dirs[:0]
	for _, d := range dirs {
		abs, err := filepath.Abs(d)
		if err != nil {
			abs = d
		}
		if !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	return out
}

// migrateLegacyDataFiles 把旧位置已有的数据文件拷贝到数据目录（仅当目标不存在时）。
// 设置 CLINE2API_DATA_DIR 后升级部署时自动接管历史数据；幂等，可重复调用。
func migrateLegacyDataFiles(dir string) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Printf("Failed to create data dir %s for migration: %v", dir, err)
		return
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	for _, name := range knownDataFiles {
		dst := filepath.Join(dir, name)
		if fileExists(dst) {
			continue
		}
		for _, d := range legacyDataCandidates() {
			if d == absDir {
				continue
			}
			src := filepath.Join(d, name)
			data, err := os.ReadFile(src)
			if err != nil {
				continue
			}
			if err := writeFileAtomic(dst, data, 0600); err != nil {
				log.Printf("Failed to migrate %s from %s to %s: %v", name, d, dst, err)
				break
			}
			log.Printf("Migrated %s from %s to %s", name, d, dst)
			break
		}
	}
}

// writeFileAtomic 原子写文件：先写临时文件再 rename 覆盖。
// 直接 WriteFile 会截断原文件，进程写一半被杀（如 docker stop）会留下损坏的 JSON，
// 下次启动加载失败被当作空数据，再被空数据写回覆盖，造成数据永久丢失。
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// backupCorruptFile 把解析失败的数据文件改名备份（.corrupt-<unix 秒>），
// 保留原始内容供人工恢复，避免后续保存把损坏文件静默覆盖。
func backupCorruptFile(path string, cause error) {
	backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
	if err := os.Rename(path, backup); err != nil {
		log.Printf("CORRUPT data file %s (%v); rename to backup failed: %v", path, cause, err)
		return
	}
	log.Printf("CORRUPT data file %s (%v); backed up to %s", path, cause, backup)
}

func loadPool() *AccountPool {
	poolMu.Lock()
	defer poolMu.Unlock()

	if pool != nil {
		return pool
	}

	data, err := os.ReadFile(poolPath)
	if err != nil {
		pool = &AccountPool{Accounts: []*Account{}, Keys: []string{}, Models: []Model{}}
		return pool
	}

	var p AccountPool
	if err := json.Unmarshal(data, &p); err != nil {
		backupCorruptFile(poolPath, err)
		pool = &AccountPool{Accounts: []*Account{}, Keys: []string{}, Models: []Model{}}
		return pool
	}

	if p.Accounts == nil {
		p.Accounts = []*Account{}
	}
	if p.Keys == nil {
		p.Keys = []string{}
	}
	if p.Models == nil {
		p.Models = []Model{}
	}
	pool = &p
	return pool
}

func savePool() {
	data, _ := json.MarshalIndent(pool, "", "  ")
	if err := writeFileAtomic(poolPath, data, 0600); err != nil {
		log.Printf("Failed to save accounts to %s: %v", poolPath, err)
	}
}

// addAccount 添加账号。同一邮箱（不区分大小写，来自 Cline userInfo，即同一
// Cline 账号）已存在时**原地更新凭据**：重新授权/重复导入不该产生重复条目 ——
// 保留原 AccountID、创建时间与用量统计，只更新令牌并置回 active。
// 已存在的同邮箱重复条目（历史遗留）一并合并进首个条目后移除（用量计数求和）。
func addAccount(acc *Account) {
	p := loadPool()
	poolMu.Lock()
	newEmail := strings.TrimSpace(acc.Email)
	var kept *Account
	var dups []*Account
	for _, a := range p.Accounts {
		if newEmail != "" && strings.EqualFold(strings.TrimSpace(a.Email), newEmail) {
			if kept == nil {
				kept = a
			} else {
				dups = append(dups, a)
			}
		}
	}
	if kept != nil {
		kept.RefreshToken = acc.RefreshToken
		kept.AccessToken = acc.AccessToken
		kept.ExpiresAt = acc.ExpiresAt
		kept.Status = "active"
		if len(dups) > 0 {
			for _, d := range dups {
				kept.UsageCount += d.UsageCount
				kept.PromptTokens += d.PromptTokens
				kept.CompletionTokens += d.CompletionTokens
				kept.TotalTokens += d.TotalTokens
				kept.CachedTokens += d.CachedTokens
			}
			out := p.Accounts[:0]
			for _, a := range p.Accounts {
				isDup := false
				for _, d := range dups {
					if a == d {
						isDup = true
						break
					}
				}
				if !isDup {
					out = append(out, a)
				}
			}
			p.Accounts = out
			log.Printf("Account %s: merged %d duplicate entr(y/ies), stats preserved", acc.Email, len(dups))
		}
		log.Printf("Account re-authorized in place: %s (AccountID/stats preserved)", acc.Email)
	} else {
		p.Accounts = append(p.Accounts, acc)
	}
	poolMu.Unlock()
	savePool()
}

// findAccountByRefreshToken 按 refreshToken 查找已有账号（不存在返回 nil）。
// 用于导入时的去重：同一个 refreshToken 只应存在一个账号。
func findAccountByRefreshToken(refreshToken string) *Account {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil
	}
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	for _, a := range p.Accounts {
		if strings.TrimSpace(a.RefreshToken) == refreshToken {
			return a
		}
	}
	return nil
}

// accountExists 判断该 refreshToken 是否已在账号池中。
func accountExists(refreshToken string) bool {
	return findAccountByRefreshToken(refreshToken) != nil
}

// isDuplicateImportToken 判断待导入的 refreshToken 是否应跳过（导入去重）：
// 账号池中已存在同一 refreshToken，或本批次内已处理过（seen）。
// seen 由调用方维护、在此更新，用于同一批次内的去重。
func isDuplicateImportToken(refreshToken string, seen map[string]bool) bool {
	if seen[refreshToken] || accountExists(refreshToken) {
		return true
	}
	seen[refreshToken] = true
	return false
}

func removeAccount(accountID string) bool {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	for i, a := range p.Accounts {
		if a.AccountID == accountID {
			p.Accounts = append(p.Accounts[:i], p.Accounts[i+1:]...)
			savePool()
			return true
		}
	}
	return false
}

func getAccountByID(accountID string) *Account {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	for _, a := range p.Accounts {
		if a.AccountID == accountID {
			return a
		}
	}
	return nil
}

func refreshAccountToken(acc *Account) error {
	resp, err := refreshClineToken(acc.RefreshToken)
	if err != nil {
		if isRefreshRejected(err) {
			// cline 自家 refresh 端点拒绝 ≠ 账号真失效：refresh token 本质是
			// WorkOS token，走 WorkOS authenticate 端点仍可能自愈，不必重新授权。
			if wos := refreshViaWorkOS(acc.RefreshToken); wos != nil {
				applyWorkOSTokens(acc, wos)
				return nil
			}
			acc.Status = "expired"
			savePool()
			// 双通道都拒绝 = WorkOS 会话已被服务端终结（invalid_grant: Session
			// has already ended / token revoked），刷新救不回，必须重新授权
			return fmt.Errorf("refresh token rejected by both Cline and WorkOS (session ended or revoked) — account requires re-authorization: 管理页「导入账号」→ OAuth 登录")
		}
		acc.Status = "cooldown"
		acc.CooldownUntil = time.Now().Add(5 * time.Minute)
		savePool()
		return fmt.Errorf("token refresh failed (transient): %w", err)
	}

	acc.AccessToken = "workos:" + resp.Data.AccessToken
	if resp.Data.RefreshToken != "" {
		acc.RefreshToken = resp.Data.RefreshToken
	}
	acc.ExpiresAt = parseExpiry(resp.Data.ExpiresAt) - 60000
	acc.Status = "active"
	savePool()
	return nil
}

// applyWorkOSTokens 用 WorkOS 直接刷新到的令牌续命账号：优先回 cline
// /auth/register 换正式 cline 令牌；注册也失败时直接用 workos 前缀令牌
// （Cline 上游接受），轮换出的新 refresh token 一并落盘。
func applyWorkOSTokens(acc *Account, wos *authenticateResp) {
	if cline, err := registerWithCline(wos.AccessToken, wos.RefreshToken); err == nil && cline.Data.AccessToken != "" {
		acc.AccessToken = "workos:" + cline.Data.AccessToken
		if cline.Data.RefreshToken != "" {
			acc.RefreshToken = cline.Data.RefreshToken
		}
		acc.ExpiresAt = parseExpiry(cline.Data.ExpiresAt) - 60000
	} else {
		acc.AccessToken = "workos:" + wos.AccessToken
		if wos.RefreshToken != "" {
			acc.RefreshToken = wos.RefreshToken
		}
		acc.ExpiresAt = time.Now().Add(time.Hour).UnixMilli() - 60000
	}
	acc.Status = "active"
	savePool()
	log.Printf("  account %s: token refreshed via WorkOS fallback (no re-auth needed)", acc.Email)
}

func pickAccount() *Account {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	return pickAccountLocked(p)
}

// pickAccountForModel 按轮询/策略挑选一个「该模型未处于模型级冷却」的账号；
// 所有 active 账号对该模型都冷却时回退到普通 pickAccount（请求会得到模型级 429 提示）。
// 空模型名等同于 pickAccount。
func pickAccountForModel(model string) *Account {
	return pickAccountForModelWithFallback(model, true)
}

func pickAccountForModelStrict(model string) *Account {
	return pickAccountForModelWithFallback(model, false)
}

func pickAccountForModelWithFallback(model string, fallbackToActive bool) *Account {
	if model == "" {
		return pickAccount()
	}

	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	active := make([]*Account, 0)
	for _, a := range p.Accounts {
		if a.Status == "active" {
			active = append(active, a)
		}
	}
	if len(active) == 0 {
		return nil
	}

	// 该模型未冷却的账号列表
	eligible := make([]*Account, 0, len(active))
	for _, a := range active {
		until, cool := a.ModelCooldowns[model]
		if !cool || time.Now().After(until) {
			if cool {
				delete(a.ModelCooldowns, model)
			}
			eligible = append(eligible, a)
		}
	}

	if len(eligible) == 0 {
		if fallbackToActive {
			return pickAccountLocked(p)
		}
		return nil
	}

	cfg := getProxyConfig()
	var acc *Account
	switch cfg.Strategy {
	case "fill":
		acc = eligible[0]
	case "random":
		n := time.Now().UnixNano() % int64(len(eligible))
		acc = eligible[n]
	default: // round_robin
		if p.CurrentIdx >= len(eligible) {
			p.CurrentIdx = 0
		}
		acc = eligible[p.CurrentIdx]
		p.CurrentIdx = (p.CurrentIdx + 1) % len(eligible)
	}
	savePool()
	return acc
}

// pickAccountForModelLeastUsed 在所有「模型未冷却」的 active 账号中，选择该模型
// 历史用量最少的账号（并清掉已过期的冷却记录）。等量时按轮询索引取，保持原有
// 公平性；全部不可用返回 nil。供回退链上的非首选模型使用：流量应摊到较少
// 使用的账号上，而不是每次都砸在第一个可用账号。
func pickAccountForModelLeastUsed(model string) *Account {
	if model == "" {
		return pickAccount()
	}

	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	var best *Account
	var bestCount int64
	for _, a := range p.Accounts {
		if a.Status != "active" {
			continue
		}
		if until, cool := a.ModelCooldowns[model]; cool {
			if time.Now().After(until) {
				delete(a.ModelCooldowns, model)
			} else {
				continue // 该账号此模型冷却中
			}
		}
		var cnt int64
		if st, ok := a.ModelStats[model]; ok {
			cnt = st.UsageCount
		}
		if best == nil || cnt < bestCount {
			best, bestCount = a, cnt
		}
	}
	if best == nil {
		return nil
	}
	savePool()
	return best
}

// sortModelsByAvailability 将回退链按「可用性优先」重排：
//  1. 有未冷却账号的模型在前；
//  2. 同组内按该模型的账号总用量升序——把流量摊到用得少的模型上，
//     避免每次都选第一个可用模型、把它的额度打到冷却。
//  3. 稳定排序：可用性与用量相同时保持管理员配置的优先级顺序。
func sortModelsByAvailability(chain []string) []string {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	now := time.Now()
	type cand struct {
		model     string
		avail     bool
		minUsage  int64
		origOrder int
	}
	cs := make([]cand, 0, len(chain))
	for i, m := range chain {
		avail := false
		minUsage := int64(-1)
		for _, a := range p.Accounts {
			if a.Status != "active" {
				continue
			}
			if until, cool := a.ModelCooldowns[m]; cool {
				if now.After(until) {
					delete(a.ModelCooldowns, m)
				} else {
					continue
				}
			}
			avail = true
			var cnt int64
			if st, ok := a.ModelStats[m]; ok {
				cnt = st.UsageCount
			}
			if minUsage < 0 || cnt < minUsage {
				minUsage = cnt
			}
		}
		cs = append(cs, cand{model: m, avail: avail, minUsage: minUsage, origOrder: i})
	}
	sort.SliceStable(cs, func(x, y int) bool {
		if cs[x].avail != cs[y].avail {
			return cs[x].avail
		}
		if cs[x].avail && cs[x].minUsage != cs[y].minUsage {
			return cs[x].minUsage < cs[y].minUsage
		}
		return cs[x].origOrder < cs[y].origOrder
	})
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.model
	}
	savePool()
	return out
}

// pickAccountLocked 在已持有 poolMu 的前提下执行普通轮询挑选（供 pickAccountForModel 回退用）。
func pickAccountLocked(p *AccountPool) *Account {
	active := make([]*Account, 0)
	for _, a := range p.Accounts {
		if a.Status == "active" {
			active = append(active, a)
		}
	}
	if len(active) == 0 {
		return nil
	}
	cfg := getProxyConfig()
	var acc *Account
	switch cfg.Strategy {
	case "fill":
		acc = active[0]
	case "random":
		n := time.Now().UnixNano() % int64(len(active))
		acc = active[n]
	default:
		if p.CurrentIdx >= len(active) {
			p.CurrentIdx = 0
		}
		acc = active[p.CurrentIdx]
		p.CurrentIdx = (p.CurrentIdx + 1) % len(active)
	}
	savePool()
	return acc
}

func ensureAccountToken(acc *Account) (string, error) {
	if acc.AccessToken != "" && time.Now().UnixMilli() < acc.ExpiresAt {
		return acc.AccessToken, nil
	}

	if err := refreshAccountToken(acc); err != nil {
		return "", err
	}

	return acc.AccessToken, nil
}

func listAccounts() []*Account {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	result := make([]*Account, len(p.Accounts))
	for i, a := range p.Accounts {
		// Don't expose tokens
		cp := &Account{
			AccountID:        a.AccountID,
			Email:            a.Email,
			Status:           a.Status,
			CooldownUntil:    a.CooldownUntil,
			LastUsed:         a.LastUsed,
			UsageCount:       a.UsageCount,
			PromptTokens:     a.PromptTokens,
			CompletionTokens: a.CompletionTokens,
			TotalTokens:      a.TotalTokens,
			CachedTokens:     a.CachedTokens,
			CreatedAt:        a.CreatedAt,
		}
		// 按模型细分统计（脱敏拷贝）
		if len(a.ModelStats) > 0 {
			cp.ModelStats = make(map[string]*ModelStat, len(a.ModelStats))
			for mid, st := range a.ModelStats {
				sc := *st
				cp.ModelStats[mid] = &sc
			}
		}
		// 模型级冷却（脱敏拷贝）
		if len(a.ModelCooldowns) > 0 {
			cp.ModelCooldowns = make(map[string]time.Time, len(a.ModelCooldowns))
			for mid, until := range a.ModelCooldowns {
				cp.ModelCooldowns[mid] = until
			}
		}
		result[i] = cp
	}
	return result
}

func addAccountFromDeviceAuth() (*Account, error) {
	fmt.Println()
	fmt.Println("=== Add New Cline Account (OAuth) ===")
	fmt.Println()

	device, err := workosDeviceAuth()
	if err != nil {
		return nil, err
	}

	authURL := device.VerificationURIComplete
	if authURL == "" {
		authURL = device.VerificationURI
	}

	fmt.Println("  1. Open this URL in your browser:")
	fmt.Println("     " + authURL)
	fmt.Println("  2. Enter code: " + device.UserCode)
	fmt.Println("  3. Log in with Google, GitHub, or email")
	fmt.Println()

	_ = openBrowser(authURL)
	fmt.Println("  Waiting for authorization...")

	interval := device.Interval
	if interval < 5 {
		interval = 5
	}
	expiresIn := device.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 300
	}

	workosTok, err := pollWorkosToken(device.DeviceCode, interval, expiresIn)
	if err != nil {
		return nil, err
	}

	fmt.Println("  WorkOS authorized. Registering with Cline...")

	cline, err := registerWithCline(workosTok.AccessToken, workosTok.RefreshToken)
	if err != nil {
		return nil, err
	}

	if cline.Data.RefreshToken == "" {
		return nil, fmt.Errorf("cline registration missing refresh token")
	}

	email := "unknown"
	if cline.Data.UserInfo != nil && cline.Data.UserInfo.Email != "" {
		email = cline.Data.UserInfo.Email
	}

	acc := &Account{
		AccountID:    fmt.Sprintf("acc_%d", time.Now().UnixMilli()),
		Email:        email,
		RefreshToken: cline.Data.RefreshToken,
		AccessToken:  "workos:" + cline.Data.AccessToken,
		ExpiresAt:    parseExpiry(cline.Data.ExpiresAt) - 60000,
		Status:       "active",
		CreatedAt:    time.Now(),
	}

	addAccount(acc)
	fmt.Printf("  Account added! Email: %s\n", email)
	return acc, nil
}
