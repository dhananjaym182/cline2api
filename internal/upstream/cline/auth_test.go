package cline

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func testConfig(serverURL string) Config {
	return Config{
		ClientID:        "cid",
		DeviceAuthURL:   serverURL + "/device",
		AuthenticateURL: serverURL + "/authenticate",
		APIBase:         serverURL + "/api",
	}
}

func TestDeviceAuthorization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("client_id") != "cid" {
			t.Errorf("client_id = %q", form.Get("client_id"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code": "dc", "user_code": "uc", "verification_uri_complete": "https://x/activate",
			"interval": 5, "expires_in": 300,
		})
	}))
	defer srv.Close()

	d, err := DeviceAuthorization(context.Background(), srv.Client(), testConfig(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if d.DeviceCode != "dc" || d.UserCode != "uc" || d.VerificationURIComplete != "https://x/activate" || d.ExpiresIn != 300 {
		t.Fatalf("device auth = %+v", d)
	}
}

func TestDeviceAuthorizationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	if _, err := DeviceAuthorization(context.Background(), srv.Client(), testConfig(srv.URL)); err == nil {
		t.Fatal("expected error")
	}
}

func TestPollDeviceTokenImmediate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("device_code") != "dc" || form.Get("grant_type") == "" {
			t.Errorf("form = %v", form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "refresh_token": "rt"})
	}))
	defer srv.Close()

	a, err := PollDeviceToken(context.Background(), srv.Client(), testConfig(srv.URL), "dc", 5, 30)
	if err != nil {
		t.Fatal(err)
	}
	if a.AccessToken != "at" || a.RefreshToken != "rt" {
		t.Fatalf("token = %+v", a)
	}
}

func TestPollDeviceTokenDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "access_denied", "error_description": "user said no"})
	}))
	defer srv.Close()

	_, err := PollDeviceToken(context.Background(), srv.Client(), testConfig(srv.URL), "dc", 5, 30)
	if err == nil || err.Error() != "workos polling error: user said no" {
		t.Fatalf("err = %v", err)
	}
}

func TestRegister(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/register" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"accessToken": "cat", "refreshToken": "crt", "expiresAt": 1700000000000,
				"userInfo": map[string]any{"email": "a@b.test"},
			},
		})
	}))
	defer srv.Close()

	c, err := Register(context.Background(), srv.Client(), testConfig(srv.URL), "wat", "wrt")
	if err != nil {
		t.Fatal(err)
	}
	if c.Data.AccessToken != "cat" || c.Data.RefreshToken != "crt" || c.Data.UserInfo == nil || c.Data.UserInfo.Email != "a@b.test" {
		t.Fatalf("register = %+v", c)
	}
}

func TestRefreshRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := Refresh(context.Background(), srv.Client(), testConfig(srv.URL), "rt")
	if !IsRefreshRejected(err) {
		t.Fatalf("err = %v (want rejection)", err)
	}
	var rejected *RefreshRejectedError
	if !errors.As(err, &rejected) || rejected.StatusCode != http.StatusUnauthorized {
		t.Fatalf("rejected = %+v", rejected)
	}
}

func TestRefreshTransientIsNotRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := Refresh(context.Background(), srv.Client(), testConfig(srv.URL), "rt")
	if err == nil || IsRefreshRejected(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestRefreshSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"accessToken": "new-at", "refreshToken": "new-rt", "expiresAt": 1700000000000},
		})
	}))
	defer srv.Close()

	resp, err := Refresh(context.Background(), srv.Client(), testConfig(srv.URL), "rt")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Data.AccessToken != "new-at" || resp.Data.RefreshToken != "new-rt" {
		t.Fatalf("refresh = %+v", resp)
	}
}

func TestRefreshViaWorkOS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var data map[string]any
		_ = json.Unmarshal(body, &data)
		if data["grant_type"] != "refresh_token" || data["client_id"] != "cid" {
			t.Errorf("body = %v", data)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "wat", "refresh_token": "wrt"})
	}))
	defer srv.Close()

	a := RefreshViaWorkOS(context.Background(), srv.Client(), testConfig(srv.URL), "rt")
	if a == nil || a.AccessToken != "wat" || a.RefreshToken != "wrt" {
		t.Fatalf("authenticate = %+v", a)
	}
}

func TestRefreshViaWorkOSRevoked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
	}))
	defer srv.Close()

	if a := RefreshViaWorkOS(context.Background(), srv.Client(), testConfig(srv.URL), "rt"); a != nil {
		t.Fatalf("authenticate = %+v", a)
	}
}

func TestParseExpiry(t *testing.T) {
	if got := ParseExpiry(float64(1700000000000)); got != 1700000000000 {
		t.Fatalf("float = %d", got)
	}
	if got := ParseExpiry(int64(42)); got != 42 {
		t.Fatalf("int64 = %d", got)
	}
	if got := ParseExpiry(int(7)); got != 7 {
		t.Fatalf("int = %d", got)
	}
	if got := ParseExpiry("2023-11-14T22:13:20Z"); got != 1700000000000 {
		t.Fatalf("rfc3339 = %d", got)
	}
	if got := ParseExpiry("not-a-time"); got != 0 {
		t.Fatalf("bad string = %d", got)
	}
	if got := ParseExpiry(nil); got != 0 {
		t.Fatalf("nil = %d", got)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ClientID != DefaultClientID || cfg.APIBase != DefaultAPIBase || cfg.DeviceAuthURL != DefaultDeviceAuthURL || cfg.AuthenticateURL != DefaultAuthenticateURL {
		t.Fatalf("defaults = %+v", cfg)
	}
}
