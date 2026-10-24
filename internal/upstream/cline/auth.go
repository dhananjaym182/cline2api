// Package cline implements the upstream Cline authentication calls (WorkOS
// device authorization plus Cline token registration/refresh). It performs HTTP
// only; credential persistence and token caching stay with the caller.
package cline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cline-go-proxy/internal/platform"
)

const (
	DefaultClientID        = "client_01K3A541FN8TA3EPPHTD2325AR"
	DefaultDeviceAuthURL   = "https://api.workos.com/user_management/authorize/device"
	DefaultAuthenticateURL = "https://api.workos.com/user_management/authenticate"
	DefaultAPIBase         = "https://api.cline.bot/api/v1"
)

// Config overrides the upstream endpoints (tests point these at local servers).
type Config struct {
	ClientID        string
	DeviceAuthURL   string
	AuthenticateURL string
	APIBase         string
}

// DefaultConfig returns the production endpoints.
func DefaultConfig() Config {
	return Config{
		ClientID:        DefaultClientID,
		DeviceAuthURL:   DefaultDeviceAuthURL,
		AuthenticateURL: DefaultAuthenticateURL,
		APIBase:         DefaultAPIBase,
	}
}

// DeviceAuth is the device-authorization response.
type DeviceAuth struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	Interval                int    `json:"interval"`
	ExpiresIn               int    `json:"expires_in"`
}

// Authenticate is the WorkOS token response.
type Authenticate struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// AuthResponse is the Cline /auth/register response envelope.
type AuthResponse struct {
	Data struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    any    `json:"expiresAt"`
		UserInfo     *struct {
			Email string `json:"email"`
		} `json:"userInfo"`
	} `json:"data"`
}

// RefreshResponse is the Cline /auth/refresh response envelope.
type RefreshResponse struct {
	Data struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    any    `json:"expiresAt"`
	} `json:"data"`
}

// RefreshRejectedError means Cline explicitly rejected the refresh token; only
// this failure means the account is truly invalid. Network/DNS/5xx failures are
// transient and must not mark the account expired.
type RefreshRejectedError struct {
	StatusCode int
}

func (e *RefreshRejectedError) Error() string {
	return fmt.Sprintf("cline refresh rejected: %d", e.StatusCode)
}

// IsRefreshRejected reports whether err is an explicit refresh rejection.
func IsRefreshRejected(err error) bool {
	var rejected *RefreshRejectedError
	return errors.As(err, &rejected)
}

func isRejectionStatus(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusUnauthorized || status == http.StatusForbidden
}

// DeviceAuthorization starts the WorkOS device-code flow.
func DeviceAuthorization(ctx context.Context, client *http.Client, cfg Config) (*DeviceAuth, error) {
	form := url.Values{"client_id": {cfg.ClientID}}
	resp, err := postForm(ctx, client, cfg.DeviceAuthURL, form)
	if err != nil {
		return nil, fmt.Errorf("workos device auth: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("workos device auth failed: %d %s", resp.StatusCode, platform.Truncate(readBody(resp), 200))
	}
	var d DeviceAuth
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("workos device auth decode: %w", err)
	}
	return &d, nil
}

// PollDeviceToken polls the WorkOS authenticate endpoint until the user
// approves. It respects ctx cancellation and the authorization lifetime.
func PollDeviceToken(ctx context.Context, client *http.Client, cfg Config, deviceCode string, interval, expiresIn int) (*Authenticate, error) {
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	currentInterval := interval
	if currentInterval < 5 {
		currentInterval = 5
	}

	for time.Now().Before(deadline) {
		form := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {deviceCode},
			"client_id":   {cfg.ClientID},
		}
		resp, err := postForm(ctx, client, cfg.AuthenticateURL, form)
		if err != nil {
			return nil, fmt.Errorf("workos poll: %w", err)
		}
		var a Authenticate
		if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("workos poll decode: %w", err)
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return &a, nil
		}
		switch a.Error {
		case "authorization_pending":
		case "slow_down":
			currentInterval += 5
		default:
			errDesc := a.ErrorDesc
			if errDesc == "" {
				errDesc = a.Error
			}
			return nil, fmt.Errorf("workos polling error: %s", errDesc)
		}
		timer := time.NewTimer(time.Duration(currentInterval) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("device authorization expired (timeout)")
		case <-timer.C:
		}
	}
	return nil, fmt.Errorf("device authorization expired (timeout)")
}

// Register exchanges WorkOS tokens for Cline account tokens.
func Register(ctx context.Context, client *http.Client, cfg Config, workosAccess, workosRefresh string) (*AuthResponse, error) {
	body := map[string]string{"accessToken": workosAccess, "refreshToken": workosRefresh}
	resp, err := postJSON(ctx, client, cfg.APIBase+"/auth/register", body)
	if err != nil {
		return nil, fmt.Errorf("cline register: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cline register failed: %d %s", resp.StatusCode, platform.Truncate(readBody(resp), 200))
	}
	var c AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return nil, fmt.Errorf("cline register decode: %w", err)
	}
	return &c, nil
}

// Refresh renews Cline tokens. An explicit rejection returns
// *RefreshRejectedError; transient failures return a plain error.
func Refresh(ctx context.Context, client *http.Client, cfg Config, refreshToken string) (*RefreshResponse, error) {
	body := map[string]string{"refreshToken": refreshToken, "grantType": "refresh_token"}
	resp, err := postJSON(ctx, client, cfg.APIBase+"/auth/refresh", body)
	if err != nil {
		return nil, fmt.Errorf("cline refresh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if isRejectionStatus(resp.StatusCode) {
			return nil, &RefreshRejectedError{StatusCode: resp.StatusCode}
		}
		return nil, fmt.Errorf("cline refresh failed: %d", resp.StatusCode)
	}
	var c RefreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return nil, fmt.Errorf("cline refresh decode: %w", err)
	}
	return &c, nil
}

// RefreshViaWorkOS refreshes directly against WorkOS. Cline's refresh token is
// a WorkOS refresh token, so this self-heals when Cline's own endpoint rejects
// a still-valid session. Returns nil when the token is truly revoked.
func RefreshViaWorkOS(ctx context.Context, client *http.Client, cfg Config, refreshToken string) *Authenticate {
	body := map[string]any{
		"grant_type":    "refresh_token",
		"client_id":     cfg.ClientID,
		"refresh_token": refreshToken,
	}
	resp, err := postJSON(ctx, client, cfg.AuthenticateURL, body)
	if err != nil {
		log.Printf("  WorkOS refresh fallback failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("  WorkOS refresh fallback rejected: %d %s", resp.StatusCode, platform.Truncate(readBody(resp), 200))
		return nil
	}
	var a Authenticate
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil || a.AccessToken == "" {
		return nil
	}
	return &a
}

// ParseExpiry normalizes the several expiry encodings Cline has used.
func ParseExpiry(exp any) int64 {
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

func postForm(ctx context.Context, client *http.Client, rawURL string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return client.Do(req)
}

func postJSON(ctx context.Context, client *http.Client, rawURL string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return client.Do(req)
}

func readBody(resp *http.Response) string {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("<read error: %v>", err)
	}
	return string(data)
}
