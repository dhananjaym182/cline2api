package adminapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSessionStoreLifecycle(t *testing.T) {
	s := NewSessionStore()
	now := time.Now()

	s.Add("live", now.Add(time.Hour))
	if !s.Valid("live", now) {
		t.Fatal("session should be valid")
	}
	if s.Valid("missing", now) {
		t.Fatal("unknown token must not be valid")
	}

	s.Remove("live")
	if s.Valid("live", now) {
		t.Fatal("removed session must not be valid")
	}
}

func TestSessionStoreExpiryRemovesEntry(t *testing.T) {
	s := NewSessionStore()
	now := time.Now()
	s.Add("stale", now.Add(-time.Second))
	if s.Valid("stale", now) {
		t.Fatal("expired session must not be valid")
	}
	s.mu.Lock()
	_, exists := s.sessions["stale"]
	s.mu.Unlock()
	if exists {
		t.Fatal("expired session should be deleted on lookup")
	}
}

func TestSessionStoreClear(t *testing.T) {
	s := NewSessionStore()
	now := time.Now()
	s.Add("a", now.Add(time.Hour))
	s.Add("b", now.Add(time.Hour))
	s.Clear()
	if s.Valid("a", now) || s.Valid("b", now) {
		t.Fatal("clear must drop all sessions")
	}
}

func TestHashAndVerifyPassword(t *testing.T) {
	salt := "aabbcc"
	hash := HashPassword(salt, "secret")
	if hash == "" || hash == HashPassword("other", "secret") {
		t.Fatal("hash must depend on salt")
	}
	if !VerifyPassword(salt, hash, "secret") {
		t.Fatal("correct password should verify")
	}
	if VerifyPassword(salt, hash, "wrong") {
		t.Fatal("wrong password must not verify")
	}
	if VerifyPassword(salt, "", "secret") {
		t.Fatal("empty stored hash must never verify")
	}
}

func TestNewSaltAndRandomHex(t *testing.T) {
	salt, err := NewSalt()
	if err != nil || len(salt) != 32 {
		t.Fatalf("salt = %q, err = %v", salt, err)
	}
	token := RandomHex(32)
	if len(token) != 64 {
		t.Fatalf("token length = %d", len(token))
	}
	if strings.ContainsAny(token, "ghijklmnopqrstuvwxyzGHIJKLMNOPQRSTUVWXYZ") {
		t.Fatalf("token is not hex: %q", token)
	}
}

func TestMiddlewarePasswordNotSetPasses(t *testing.T) {
	a := &Auth{Sessions: NewSessionStore(), PasswordSet: func() bool { return false }}
	called := false
	h := a.Middleware(func(w http.ResponseWriter, r *http.Request) { called = true })
	h(httptest.NewRecorder(), httptest.NewRequest("GET", "/admin/api/models", nil))
	if !called {
		t.Fatal("request should pass when no password is configured")
	}
}

func TestMiddlewareMissingCookie(t *testing.T) {
	a := &Auth{
		Sessions:    NewSessionStore(),
		PasswordSet: func() bool { return true },
		Unauthorized: func(w http.ResponseWriter, r *http.Request, reason string) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(reason))
		},
	}
	called := false
	h := a.Middleware(func(w http.ResponseWriter, r *http.Request) { called = true })
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/admin/api/models", nil))
	if called {
		t.Fatal("handler must not run without a session cookie")
	}
	if rec.Code != http.StatusUnauthorized || rec.Body.String() != "login_required" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
}

func TestMiddlewareValidAndExpiredSessions(t *testing.T) {
	sessions := NewSessionStore()
	sessions.Add("good", time.Now().Add(time.Hour))
	sessions.Add("old", time.Now().Add(-time.Hour))
	a := &Auth{
		Sessions:    sessions,
		PasswordSet: func() bool { return true },
		Unauthorized: func(w http.ResponseWriter, r *http.Request, reason string) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(reason))
		},
	}
	called := 0
	h := a.Middleware(func(w http.ResponseWriter, r *http.Request) { called++ })

	req := httptest.NewRequest("GET", "/admin/api/models", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "good"})
	h(httptest.NewRecorder(), req)
	if called != 1 {
		t.Fatal("valid session should reach the handler")
	}

	req = httptest.NewRequest("GET", "/admin/api/models", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "old"})
	rec := httptest.NewRecorder()
	h(rec, req)
	if called != 1 {
		t.Fatal("expired session must not reach the handler")
	}
	if rec.Code != http.StatusUnauthorized || rec.Body.String() != "session_expired" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
}

func TestSessionCookies(t *testing.T) {
	rec := httptest.NewRecorder()
	SetSessionCookie(rec, "tok")
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != SessionCookieName || c.Value != "tok" || c.Path != "/admin" || !c.HttpOnly {
		t.Fatalf("unexpected cookie: %+v", c)
	}
	if c.SameSite != http.SameSiteLaxMode || c.MaxAge != int(SessionTTL.Seconds()) {
		t.Fatalf("unexpected cookie attributes: %+v", c)
	}

	rec = httptest.NewRecorder()
	ClearSessionCookie(rec)
	cleared := rec.Result().Cookies()[0]
	if cleared.Name != SessionCookieName || cleared.MaxAge != -1 {
		t.Fatalf("unexpected clear cookie: %+v", cleared)
	}
}
