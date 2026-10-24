package adminapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthHandlersLoginRequiresConfiguredPassword(t *testing.T) {
	h := &AuthHandlers{Sessions: NewSessionStore(), PasswordSet: func() bool { return false }}
	rec := httptest.NewRecorder()
	h.Login(rec, httptest.NewRequest(http.MethodPost, "/admin/api/login", strings.NewReader(`{"password":"x"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if resp := decodeResponse(t, rec); resp.Error != "password_not_enabled" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestAuthHandlersLoginRejectsWrongPassword(t *testing.T) {
	var slept time.Duration
	h := &AuthHandlers{
		Sessions:       NewSessionStore(),
		PasswordSet:    func() bool { return true },
		VerifyPassword: func(string) bool { return false },
		Sleep:          func(d time.Duration) { slept = d },
		Translate:      func(*http.Request, string) string { return "nope" },
	}
	rec := httptest.NewRecorder()
	h.Login(rec, httptest.NewRequest(http.MethodPost, "/admin/api/login", strings.NewReader(`{"password":"x"}`)))
	if rec.Code != http.StatusUnauthorized || slept != 500*time.Millisecond {
		t.Fatalf("status = %d slept = %v", rec.Code, slept)
	}
	if resp := decodeResponse(t, rec); resp.Error != "nope" || resp.Success {
		t.Fatalf("response = %+v", resp)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("wrong password must not set a session cookie")
	}
}

func TestAuthHandlersLoginIssuesSession(t *testing.T) {
	sessions := NewSessionStore()
	h := &AuthHandlers{
		Sessions:       sessions,
		PasswordSet:    func() bool { return true },
		VerifyPassword: func(p string) bool { return p == "secret" },
	}
	rec := httptest.NewRecorder()
	h.Login(rec, httptest.NewRequest(http.MethodPost, "/admin/api/login", strings.NewReader(`{"password":"secret"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != SessionCookieName || cookies[0].Value == "" {
		t.Fatalf("cookies = %+v", cookies)
	}
	if !sessions.Valid(cookies[0].Value, time.Now()) {
		t.Fatal("issued session must be valid")
	}
}

func TestAuthHandlersLogoutClearsSession(t *testing.T) {
	sessions := NewSessionStore()
	token := "tok"
	sessions.Add(token, time.Now().Add(time.Hour))
	h := &AuthHandlers{Sessions: sessions}
	req := httptest.NewRequest(http.MethodPost, "/admin/api/logout", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	h.Logout(rec, req)
	if sessions.Valid(token, time.Now()) {
		t.Fatal("logout must remove the session")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatalf("cookies = %+v", cookies)
	}
}

func TestAuthHandlersPasswordSetAndClear(t *testing.T) {
	var applied []string
	h := &AuthHandlers{SetPassword: func(p string) { applied = append(applied, p) }}

	rec := httptest.NewRecorder()
	h.Password(rec, httptest.NewRequest(http.MethodPost, "/admin/api/password", strings.NewReader(`{"password":"new"}`)))
	if resp := decodeResponse(t, rec); !resp.Success || resp.Message != "password_updated" {
		t.Fatalf("response = %+v", resp)
	}

	rec = httptest.NewRecorder()
	h.Password(rec, httptest.NewRequest(http.MethodPost, "/admin/api/password", strings.NewReader(`{"password":""}`)))
	if resp := decodeResponse(t, rec); !resp.Success || resp.Message != "password_cleared" {
		t.Fatalf("response = %+v", resp)
	}
	if len(applied) != 2 || applied[0] != "new" || applied[1] != "" {
		t.Fatalf("applied = %v", applied)
	}
}
