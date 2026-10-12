package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	workosClientID       = "client_01K3A541FN8TA3EPPHTD2325AR"
	workosDeviceAuthURL  = "https://api.workos.com/user_management/authorize/device"
	workosAuthenticateURL = "https://api.workos.com/user_management/authenticate"
	clineAPIBase         = "https://api.cline.bot/api/v1"
)

type credentials struct {
	RefreshToken string `json:"refreshToken"`
}

type deviceAuthResp struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	Interval                int    `json:"interval"`
	ExpiresIn               int    `json:"expires_in"`
}

type authenticateResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

type clineAuthResp struct {
	Data struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    any    `json:"expiresAt"`
		UserInfo     *struct {
			Email string `json:"email"`
		} `json:"userInfo"`
	} `json:"data"`
}

type clineRefreshResp struct {
	Data struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    any    `json:"expiresAt"`
	} `json:"data"`
}

var (
	cachedToken      string
	cachedExpiry     int64
	cachedRefreshTok string
	credentialsPath  string
)

func init() {
	credentialsPath = findCredentialsFile()
}

func findCredentialsFile() string {
	// CLINE2API_DATA_DIR 设置时固定使用该目录（与 resolveDataPath 保持一致）
	if dir := envDataDir(); dir != "" {
		migrateOnce.Do(func() { migrateLegacyDataFiles(dir) })
		return filepath.Join(dir, ".cline-credentials.json")
	}
	// First, try next to the executable
	exe, err := os.Executable()
	if err == nil {
		p := filepath.Join(filepath.Dir(exe), ".cline-credentials.json")
		if fileExists(p) {
			return p
		}
	}
	// Second, try current working directory
	pwd, err := os.Getwd()
	if err == nil {
		p := filepath.Join(pwd, ".cline-credentials.json")
		if fileExists(p) {
			return p
		}
	}
	// Default to executable directory
	if err == nil {
		return filepath.Join(filepath.Dir(exe), ".cline-credentials.json")
	}
	pwd, _ = os.Getwd()
	return filepath.Join(pwd, ".cline-credentials.json")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func loadCredentials() *credentials {
	data, err := os.ReadFile(credentialsPath)
	if err != nil {
		return nil
	}
	var c credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	return &c
}

func saveCredentials(rt string) {
	c := credentials{RefreshToken: rt}
	data, _ := json.MarshalIndent(c, "", "  ")
	if err := writeFileAtomic(credentialsPath, data, 0600); err != nil {
		log.Printf("Failed to save credentials: %v", err)
		return
	}
	log.Printf("Credentials saved to %s", credentialsPath)
}

func workosDeviceAuth() (*deviceAuthResp, error) {
	form := url.Values{"client_id": {workosClientID}}
	resp, err := httpPostForm(workosDeviceAuthURL, form)
	if err != nil {
		return nil, fmt.Errorf("workos device auth: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body := readBody(resp)
		return nil, fmt.Errorf("workos device auth failed: %d %s", resp.StatusCode, truncate(body, 200))
	}

	var d deviceAuthResp
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("workos device auth decode: %w", err)
	}
	return &d, nil
}

func pollWorkosToken(deviceCode string, interval, expiresIn int) (*authenticateResp, error) {
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	currentInterval := interval
	if currentInterval < 5 {
		currentInterval = 5
	}

	for time.Now().Before(deadline) {
		form := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {deviceCode},
			"client_id":   {workosClientID},
		}
		resp, err := httpPostForm(workosAuthenticateURL, form)
		if err != nil {
			return nil, fmt.Errorf("workos poll: %w", err)
		}

		var a authenticateResp
		if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("workos poll decode: %w", err)
		}
		resp.Body.Close()

		if resp.StatusCode == 200 {
			return &a, nil
		}

		switch a.Error {
		case "authorization_pending":
			time.Sleep(time.Duration(currentInterval) * time.Second)
		case "slow_down":
			currentInterval += 5
			time.Sleep(time.Duration(currentInterval) * time.Second)
		default:
			errDesc := a.ErrorDesc
			if errDesc == "" {
				errDesc = a.Error
			}
			return nil, fmt.Errorf("workos polling error: %s", errDesc)
		}
	}
	return nil, fmt.Errorf("device authorization expired (timeout)")
}

func registerWithCline(workosAccess, workosRefresh string) (*clineAuthResp, error) {
	body := map[string]string{
		"accessToken":  workosAccess,
		"refreshToken": workosRefresh,
	}
	resp, err := httpPostJSON(clineAPIBase+"/auth/register", body)
	if err != nil {
		return nil, fmt.Errorf("cline register: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b := readBody(resp)
		return nil, fmt.Errorf("cline register failed: %d %s", resp.StatusCode, truncate(b, 200))
	}

	var c clineAuthResp
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return nil, fmt.Errorf("cline register decode: %w", err)
	}
	return &c, nil
}

// refreshRejectedError 表示 Cline 明確拒絕了這把 refresh token；只有這種失敗才代表帳號真的失效。
// 網路中斷、DNS 失敗、5xx 都是暫時性的，若也當成失效，一次斷網就會把整個帳號池永久標成 expired。
type refreshRejectedError struct {
	statusCode int
}

func (e *refreshRejectedError) Error() string {
	return fmt.Sprintf("cline refresh rejected: %d", e.statusCode)
}

func isRefreshRejectionStatus(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusUnauthorized || status == http.StatusForbidden
}

func isRefreshRejected(err error) bool {
	var rejected *refreshRejectedError
	return errors.As(err, &rejected)
}

func refreshClineToken(refreshToken string) (*clineRefreshResp, error) {
	body := map[string]string{
		"refreshToken": refreshToken,
		"grantType":    "refresh_token",
	}
	resp, err := httpPostJSON(clineAPIBase+"/auth/refresh", body)
	if err != nil {
		return nil, fmt.Errorf("cline refresh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		if isRefreshRejectionStatus(resp.StatusCode) {
			return nil, &refreshRejectedError{statusCode: resp.StatusCode}
		}
		return nil, fmt.Errorf("cline refresh failed: %d", resp.StatusCode)
	}

	var c clineRefreshResp
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return nil, fmt.Errorf("cline refresh decode: %w", err)
	}
	return &c, nil
}

// refreshViaWorkOS 用 WorkOS 的 authenticate 端点直接刷新：cline 的 refresh
// token 本质就是 WorkOS refresh token（/auth/register 只是换皮封装），cline
// 自家 /auth/refresh 端点调整或 rotate 残留时可能 400，但 WorkOS 通道仍可
// 自愈 —— 无需重新走设备授权（用户不必再登录一次）。成功返回新的 workos
// access/refresh token，失败（含 invalid_grant：token 真的被吊销）返回 nil。
func refreshViaWorkOS(refreshToken string) *authenticateResp {
	body := map[string]any{
		"grant_type":    "refresh_token",
		"client_id":     workosClientID,
		"refresh_token": refreshToken,
	}
	resp, err := httpPostJSON(workosAuthenticateURL, body)
	if err != nil {
		log.Printf("  WorkOS refresh fallback failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// 失败原因要可见：invalid_grant（Session has already ended / token revoked）
		// = 会话已被服务端终结，只能重新授权；其余为瞬时/格式问题
		log.Printf("  WorkOS refresh fallback rejected: %d %s", resp.StatusCode, truncate(readBody(resp), 200))
		return nil
	}
	var a authenticateResp
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil || a.AccessToken == "" {
		return nil
	}
	return &a
}

func getToken() (string, error) {
	if cachedToken != "" && time.Now().UnixMilli() < cachedExpiry {
		return cachedToken, nil
	}

	creds := loadCredentials()
	if creds != nil && creds.RefreshToken != "" {
		resp, err := refreshClineToken(creds.RefreshToken)
		if err == nil && resp.Data.AccessToken != "" {
			cachedToken = "workos:" + resp.Data.AccessToken
			cachedRefreshTok = resp.Data.RefreshToken
			if cachedRefreshTok == "" {
				cachedRefreshTok = creds.RefreshToken
			}
			cachedExpiry = parseExpiry(resp.Data.ExpiresAt) - 60000
			saveCredentials(cachedRefreshTok)
			return cachedToken, nil
		}
		// cline 端点拒绝时走 WorkOS 通道自愈（refresh token 即 WorkOS token）
		if isRefreshRejected(err) {
			if wos := refreshViaWorkOS(creds.RefreshToken); wos != nil {
				cachedToken = "workos:" + wos.AccessToken
				cachedRefreshTok = wos.RefreshToken
				if cachedRefreshTok == "" {
					cachedRefreshTok = creds.RefreshToken
				}
				cachedExpiry = time.Now().Add(time.Hour).UnixMilli() - 60000
				saveCredentials(cachedRefreshTok)
				log.Printf("Token refreshed via WorkOS fallback")
				return cachedToken, nil
			}
		}
		log.Printf("Token refresh failed: %v", err)
	}
	return "", fmt.Errorf("no valid credentials. Run with --login flag first")
}

func parseExpiry(exp any) int64 {
	switch v := exp.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err == nil {
			return t.UnixMilli()
		}
		t, err = time.Parse(time.RFC3339Nano, v)
		if err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

func doLogin() error {
	fmt.Println()
	fmt.Println("Starting Cline OAuth login...")
	fmt.Println()

	device, err := workosDeviceAuth()
	if err != nil {
		return err
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

	// Try to open browser automatically
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
		return err
	}

	fmt.Println("  WorkOS authorized. Registering with Cline...")

	cline, err := registerWithCline(workosTok.AccessToken, workosTok.RefreshToken)
	if err != nil {
		return err
	}

	if cline.Data.RefreshToken == "" {
		return fmt.Errorf("cline registration missing refresh token")
	}

	saveCredentials(cline.Data.RefreshToken)
	cachedToken = "workos:" + cline.Data.AccessToken
	cachedRefreshTok = cline.Data.RefreshToken
	cachedExpiry = parseExpiry(cline.Data.ExpiresAt) - 60000

	email := "unknown"
	if cline.Data.UserInfo != nil && cline.Data.UserInfo.Email != "" {
		email = cline.Data.UserInfo.Email
	}
	fmt.Printf("  Login successful! Account: %s\n", email)
	return nil
}

func openBrowser(url string) error {
	var cmd string
	var args []string

	switch {
	case isWindows():
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		// Try common browser openers
		for _, candidate := range []string{"xdg-open", "open", "gnome-open"} {
			if _, err := os.Stat("/usr/bin/" + candidate); err == nil {
				cmd = candidate
				break
			}
			if _, err := os.Stat("/usr/local/bin/" + candidate); err == nil {
				cmd = candidate
				break
			}
		}
	}

	if cmd == "" {
		return fmt.Errorf("no browser opener found")
	}

	return runCommand(cmd, args...)
}

func isWindows() bool {
	return strings.Contains(strings.ToLower(os.Getenv("OS")), "windows")
}
