package deviceflow

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestJSONPathString(t *testing.T) {
	m := map[string]any{
		"email": "e@x",
		"organizations": []any{
			map[string]any{"id": "o1"},
			map[string]any{"id": "o2"},
		},
	}
	if got := JSONPathString(m, "organizations.1.id"); got != "o2" {
		t.Fatalf("array path = %q", got)
	}
	if got := JSONPathString(m, "email"); got != "e@x" {
		t.Fatalf("email path = %q", got)
	}
	if got := JSONPathString(m, "organizations.5.id"); got != "" {
		t.Fatalf("out-of-range path = %q", got)
	}
	if got := JSONPathString(m, ""); got != "" {
		t.Fatalf("empty path = %q", got)
	}
}

func TestNewPKCE(t *testing.T) {
	verifier, challenge, err := NewPKCE()
	if err != nil {
		t.Fatal(err)
	}
	if len(verifier) < 40 {
		t.Fatalf("verifier too short: %q", verifier)
	}
	sum := sha256.Sum256([]byte(verifier))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); challenge != want {
		t.Fatalf("challenge = %q, want %q", challenge, want)
	}
}

func TestQoderExpiryMs(t *testing.T) {
	if got := QoderExpiryMs(map[string]any{"expires_in": float64(0)}); got < time.Now().Add(23*time.Hour).UnixMilli() {
		t.Fatalf("short lifetime not floored: %d", got)
	}
	sec := time.Now().Add(72 * time.Hour).Unix()
	if got := QoderExpiryMs(map[string]any{"expires_at": float64(sec)}); got != sec*1000 {
		t.Fatalf("seconds expiry = %d, want %d", got, sec*1000)
	}
	if got := QoderExpiryMs(map[string]any{"expires_at": float64(sec * 1000)}); got != sec*1000 {
		t.Fatalf("millis expiry = %d, want %d", got, sec*1000)
	}
	futureRFC := time.Unix(sec, 0).UTC().Format(time.RFC3339)
	if got, want := QoderExpiryMs(map[string]any{"expires_at": futureRFC}), sec*1000; got < want-2000 || got > want+2000 {
		t.Fatalf("rfc3339 expiry = %d, want ~%d", got, want)
	}
	if got := QoderExpiryMs(map[string]any{}); got <= time.Now().UnixMilli() {
		t.Fatalf("default expiry not in the future: %d", got)
	}
}

func TestInitiateRFC8628PKCE(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":               "dev-1",
			"user_code":                 "user-1",
			"verification_uri_complete": "https://example.test/activate",
			"expires_in":                900,
		})
	}))
	defer srv.Close()

	spec := Spec{Mode: "rfc8628", InitiateURL: srv.URL, ClientID: "cid", Scope: "openid", PKCE: true}
	init, err := Initiate(context.Background(), srv.Client(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if init.Code != "dev-1" || init.UserCode != "user-1" || init.VerificationURL != "https://example.test/activate" || init.ExpiresIn != 900 {
		t.Fatalf("init = %+v", init)
	}
	if gotForm.Get("client_id") != "cid" || gotForm.Get("scope") != "openid" {
		t.Fatalf("form = %v", gotForm)
	}
	if gotForm.Get("code_challenge") == "" || gotForm.Get("code_challenge_method") != "S256" {
		t.Fatalf("pkce form = %v", gotForm)
	}
	if init.Verifier == "" {
		t.Fatal("missing verifier")
	}
	sum := sha256.Sum256([]byte(init.Verifier))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); gotForm.Get("code_challenge") != want {
		t.Fatalf("challenge mismatch")
	}
}

func TestInitiateStatePoll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Query().Get("platform") != "CLI" {
			t.Errorf("platform = %q", r.URL.Query().Get("platform"))
		}
		if r.Header.Get("X-Product") != "SaaS" || r.Header.Get("X-No-Authorization") != "true" {
			t.Errorf("headers = %v", r.Header)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"state": "st-1", "authUrl": "https://auth.test/x"},
		})
	}))
	defer srv.Close()

	spec := Spec{Mode: "state-poll", StateURL: srv.URL, Platform: "CLI"}
	init, err := Initiate(context.Background(), srv.Client(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if init.Code != "st-1" || init.VerificationURL != "https://auth.test/x" || init.ExpiresIn != 900 {
		t.Fatalf("init = %+v", init)
	}
}

func TestInitiateStatePollError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 40001, "msg": "bad request"})
	}))
	defer srv.Close()

	_, err := Initiate(context.Background(), srv.Client(), Spec{Mode: "state-poll", StateURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("err = %v", err)
	}
}

func TestPollRFC8628PendingThenToken(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Errorf("grant_type = %q", form.Get("grant_type"))
		}
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-1", "refresh_token": "rt-1", "expires_in": 3600,
		})
	}))
	defer srv.Close()

	spec := Spec{Mode: "rfc8628", TokenURL: srv.URL, ClientID: "cid", IntervalSec: 1}
	res, err := Poll(context.Background(), srv.Client(), spec, &Initiation{Code: "dev-1", ExpiresIn: 30})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != "at-1" || res.RefreshToken != "rt-1" || res.ExpiresAtMs <= time.Now().UnixMilli() {
		t.Fatalf("result = %+v", res)
	}
	if calls < 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestPollRFC8628Denied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "access_denied"})
	}))
	defer srv.Close()

	_, err := Poll(context.Background(), srv.Client(), Spec{Mode: "rfc8628", TokenURL: srv.URL, IntervalSec: 1},
		&Initiation{Code: "dev", ExpiresIn: 30})
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestPollKiloApprovedWithProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/codes"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "approved", "token": "kilo-token", "userEmail": "",
			})
		case strings.HasSuffix(r.URL.Path, "/profile"):
			if r.Header.Get("Authorization") != "Bearer kilo-token" {
				t.Errorf("auth = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"email":         "k@example.test",
				"organizations": []any{map[string]any{"id": "org-1"}},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	spec := Spec{
		PollURLBase: srv.URL + "/codes",
		ProfileURL:  srv.URL + "/profile",
		EmailPath:   "email",
		OrgIDPath:   "organizations.0.id",
		IntervalSec: 1,
	}
	res, err := Poll(context.Background(), srv.Client(), spec, &Initiation{Code: "code-1", ExpiresIn: 30})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != "kilo-token" || res.Email != "k@example.test" || res.OrgID != "org-1" {
		t.Fatalf("result = %+v", res)
	}
}

func TestPollKiloDeniedAndExpired(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{http.StatusForbidden, "authorization denied by user"},
		{http.StatusGone, "authorization code expired"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
		}))
		_, err := Poll(context.Background(), srv.Client(), Spec{PollURLBase: srv.URL, IntervalSec: 1},
			&Initiation{Code: "c", ExpiresIn: 30})
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("status %d: err = %v", tc.status, err)
		}
	}
}

func TestPollStatePollPendingThenSuccess(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-No-Enterprise-Id") != "true" {
			t.Errorf("missing poll header: %v", r.Header)
		}
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 11217})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"accessToken": "cb-at", "refreshToken": "cb-rt", "expiresIn": 3600},
		})
	}))
	defer srv.Close()

	spec := Spec{Mode: "state-poll", StateURL: "https://copilot.test", TokenURL: srv.URL, UserAgent: "CLI/test", IntervalSec: 1}
	res, err := Poll(context.Background(), srv.Client(), spec, &Initiation{Code: "st", ExpiresIn: 30})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != "cb-at" || res.RefreshToken != "cb-rt" || res.ExpiresAtMs <= time.Now().UnixMilli() {
		t.Fatalf("result = %+v", res)
	}
}

func TestPollQoderPendingThenTokenWithProfile(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/poll"):
			calls++
			if r.URL.Query().Get("challenge_method") != "S256" || r.URL.Query().Get("verifier") != "ver-1" {
				t.Errorf("query = %v", r.URL.Query())
			}
			if calls == 1 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "qd-at", "refresh_token": "qd-rt", "user_id": "u-1", "expires_in": 0,
			})
		case strings.HasSuffix(r.URL.Path, "/userinfo"):
			_ = json.NewEncoder(w).Encode(map[string]any{"email": "q@example.test", "organization_id": "org-q"})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	spec := Spec{
		Mode: "qoder", TokenURL: srv.URL + "/poll", ProfileURL: srv.URL + "/userinfo",
		EmailPath: "email", OrgIDPath: "organization_id", UserAgent: "Go-http-client/2.0", IntervalSec: 1,
	}
	res, err := Poll(context.Background(), srv.Client(), spec, &Initiation{Code: "nonce-1", Verifier: "ver-1", ExpiresIn: 30})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != "qd-at" || res.UserID != "u-1" || res.Email != "q@example.test" || res.OrgID != "org-q" {
		t.Fatalf("result = %+v", res)
	}
	if res.ExpiresAtMs < time.Now().Add(23*time.Hour).UnixMilli() {
		t.Fatalf("expiry not floored: %d", res.ExpiresAtMs)
	}
}

func TestPollQoderEmailFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/poll") {
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "qd", "user_id": "u-9"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	spec := Spec{Mode: "qoder", TokenURL: srv.URL + "/poll", ProfileURL: srv.URL + "/me", IntervalSec: 1}
	res, err := Poll(context.Background(), srv.Client(), spec, &Initiation{Code: "n", Verifier: "v", ExpiresIn: 30})
	if err != nil {
		t.Fatal(err)
	}
	if res.Email != "qoder-user-u-9" {
		t.Fatalf("email = %q", res.Email)
	}
}

func TestPollCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := Poll(ctx, srv.Client(), Spec{PollURLBase: srv.URL, IntervalSec: 1}, &Initiation{Code: "c", ExpiresIn: 30})
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
}

func TestExchangeJobToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer oauth-1" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "job-1", "expires_at": float64(1700000000)})
	}))
	defer srv.Close()

	spec := Spec{ExchangeURL: srv.URL, ExchangeTokenPath: "token", ExchangeExpPath: "expires_at"}
	tok, exp, err := ExchangeJobToken(context.Background(), srv.Client(), spec, "oauth-1")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "job-1" || exp != 1700000000*1000 {
		t.Fatalf("tok = %q exp = %d", tok, exp)
	}
}

func TestExchangeJobTokenError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	_, _, err := ExchangeJobToken(context.Background(), srv.Client(), Spec{ExchangeURL: srv.URL, ExchangeTokenPath: "token"}, "oauth-1")
	if err == nil || !strings.Contains(err.Error(), "token exchange") {
		t.Fatalf("err = %v", err)
	}
}

func TestRefreshRFC8628(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "old-rt" {
			t.Errorf("form = %v", form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-at", "refresh_token": "new-rt", "expires_in": 120})
	}))
	defer srv.Close()

	spec := Spec{Mode: "rfc8628", TokenURL: srv.URL, ClientID: "cid"}
	at, rt, exp, ok := RefreshRFC8628(context.Background(), srv.Client(), spec, "old-rt", 5)
	if !ok || at != "new-at" || rt != "new-rt" || exp <= time.Now().UnixMilli() {
		t.Fatalf("ok=%v at=%q rt=%q exp=%d", ok, at, rt, exp)
	}

	if _, _, _, ok := RefreshRFC8628(context.Background(), srv.Client(), spec, "", 5); ok {
		t.Fatal("empty refresh token should not refresh")
	}
}

func TestDecodeJSONBodyHTMLHint(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusMethodNotAllowed,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
	}
	_, err := decodeJSONBody("device auth initiation", resp, []byte("<!doctype html><html></html>"))
	if err == nil || !strings.Contains(err.Error(), "HTML page") {
		t.Fatalf("err = %v", err)
	}
}
