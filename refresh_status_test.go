package main

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func isolateRefreshState(t *testing.T, transport freeModelRoundTripper) {
	t.Helper()
	oldPool, oldPoolPath, oldTransport := pool, poolPath, httpClient.Transport
	poolPath = filepath.Join(t.TempDir(), ".cline-accounts.json")
	httpClient.Transport = transport
	t.Cleanup(func() {
		pool, poolPath, httpClient.Transport = oldPool, oldPoolPath, oldTransport
	})
}

func refreshStatusResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}
}

func TestRefreshAccountTokenCoolsDownInsteadOfExpiringOnTransportError(t *testing.T) {
	isolateRefreshState(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: lookup api.cline.bot: no such host")
	})
	acc := &Account{AccountID: "net", Email: "net@example.com", RefreshToken: "rt", Status: "active"}
	pool = &AccountPool{Accounts: []*Account{acc}}

	if err := refreshAccountToken(acc); err == nil {
		t.Fatal("refreshAccountToken should report the transport error")
	}
	if acc.Status != "cooldown" {
		t.Fatalf("status = %q, want a short cooldown after a transient network failure", acc.Status)
	}
}

func TestRefreshAccountTokenCoolsDownInsteadOfExpiringOnServerError(t *testing.T) {
	isolateRefreshState(t, func(req *http.Request) (*http.Response, error) {
		return refreshStatusResponse(req, http.StatusServiceUnavailable, `{"error":"unavailable"}`), nil
	})
	acc := &Account{AccountID: "5xx", Email: "5xx@example.com", RefreshToken: "rt", Status: "active"}
	pool = &AccountPool{Accounts: []*Account{acc}}

	if err := refreshAccountToken(acc); err == nil {
		t.Fatal("refreshAccountToken should report the server error")
	}
	if acc.Status != "cooldown" {
		t.Fatalf("status = %q, want a short cooldown after an upstream 503", acc.Status)
	}
}

func TestRefreshAccountTokenExpiresAccountWhenRefreshIsRejected(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden} {
		isolateRefreshState(t, func(req *http.Request) (*http.Response, error) {
			return refreshStatusResponse(req, status, `{"error":"invalid_grant"}`), nil
		})
		acc := &Account{AccountID: "rejected", Email: "rejected@example.com", RefreshToken: "rt", Status: "active"}
		pool = &AccountPool{Accounts: []*Account{acc}}

		if err := refreshAccountToken(acc); err == nil {
			t.Fatalf("status %d: refreshAccountToken should fail", status)
		}
		if acc.Status != "expired" {
			t.Fatalf("status %d: account status = %q, want expired", status, acc.Status)
		}
	}
}

func TestCallClineAPIWithAccountDoesNotExpireWhenRefreshAfter401HitsNetworkError(t *testing.T) {
	isolateRefreshState(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/auth/refresh") {
			return nil, errors.New("dial tcp: lookup api.cline.bot: no such host")
		}
		return refreshStatusResponse(req, http.StatusUnauthorized, `{"error":"unauthorized"}`), nil
	})
	acc := &Account{
		AccountID:    "chat-401",
		Email:        "chat-401@example.com",
		AccessToken:  "stale",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(time.Hour).UnixMilli(),
		Status:       "active",
	}
	pool = &AccountPool{Accounts: []*Account{acc}}

	_, _, err := callClineAPIWithAccount(acc, map[string]any{
		"model":    freeModelFallback,
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}, false)
	if err == nil {
		t.Fatal("callClineAPIWithAccount should fail when refresh cannot reach Cline")
	}
	if acc.Status == "expired" {
		t.Fatalf("status = %q, want a recoverable state after a transient refresh failure", acc.Status)
	}
}

// cline 自家 refresh 400 时，WorkOS 通道兜底：refreshed via workos →
// registerWithCline 换正式令牌 → 账号复活，无需重新授权。
func TestRefreshAccountTokenRecoversViaWorkOSFallback(t *testing.T) {
	var sawRegister bool
	isolateRefreshState(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/auth/refresh"):
			return refreshStatusResponse(req, http.StatusBadRequest, `{"error":"invalid_grant"}`), nil
		case strings.HasSuffix(req.URL.Path, "/user_management/authenticate"):
			return refreshStatusResponse(req, http.StatusOK, `{"access_token":"workos-new-access","refresh_token":"workos-new-refresh","expires_in":3600}`), nil
		case strings.HasSuffix(req.URL.Path, "/auth/register"):
			sawRegister = true
			return refreshStatusResponse(req, http.StatusOK, `{"data":{"accessToken":"cline-new-access","refreshToken":"cline-new-refresh","expiresAt":4102444800000}}`), nil
		}
		t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
		return nil, nil
	})
	acc := &Account{AccountID: "wos", Email: "wos@example.com", RefreshToken: "rt", Status: "active"}
	pool = &AccountPool{Accounts: []*Account{acc}}

	if err := refreshAccountToken(acc); err != nil {
		t.Fatalf("refreshAccountToken should recover via WorkOS fallback, got: %v", err)
	}
	if !sawRegister {
		t.Error("expected re-registration with Cline after WorkOS refresh")
	}
	if acc.Status != "active" {
		t.Errorf("status = %q, want active", acc.Status)
	}
	if acc.AccessToken != "workos:cline-new-access" {
		t.Errorf("AccessToken = %q, want workos:cline-new-access", acc.AccessToken)
	}
	if acc.RefreshToken != "cline-new-refresh" {
		t.Errorf("RefreshToken = %q, want cline-new-refresh", acc.RefreshToken)
	}
}

// cline refresh 400 且 WorkOS 也拒绝（invalid_grant，token 真被吊销）→ expired。
func TestRefreshAccountTokenExpiresWhenWorkOSAlsoRejects(t *testing.T) {
	isolateRefreshState(t, func(req *http.Request) (*http.Response, error) {
		return refreshStatusResponse(req, http.StatusBadRequest, `{"error":"invalid_grant"}`), nil
	})
	acc := &Account{AccountID: "dead", Email: "dead@example.com", RefreshToken: "rt", Status: "active"}
	pool = &AccountPool{Accounts: []*Account{acc}}

	if err := refreshAccountToken(acc); err == nil {
		t.Fatal("refreshAccountToken should fail when both channels reject")
	}
	if acc.Status != "expired" {
		t.Fatalf("status = %q, want expired when the token is truly revoked", acc.Status)
	}
}
