package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminLoginLogoutHandlers(t *testing.T) {
	oldPool := pool
	t.Cleanup(func() {
		adminSessions.Clear()
		pool = oldPool
	})
	pool = &AccountPool{}
	setAdminPassword("secret")
	adminSessions.Clear()

	login := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/admin/api/login", strings.NewReader(body))
		handleAdminLogin(rec, req)
		return rec
	}

	if rec := login(`{"password":"wrong"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d", rec.Code)
	}

	rec := login(`{"password":"secret"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != adminSessionCookie {
		t.Fatalf("cookies = %+v", cookies)
	}
	token := cookies[0].Value
	if !adminSessions.Valid(token, time.Now()) {
		t.Fatal("login must create a valid session")
	}

	called := false
	protected := requireAdminAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("GET", "/admin/api/models", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookie, Value: token})
	rec = httptest.NewRecorder()
	protected(rec, req)
	if !called || rec.Code != http.StatusOK {
		t.Fatalf("session request: called=%v status=%d", called, rec.Code)
	}

	logoutReq := httptest.NewRequest("POST", "/admin/api/logout", nil)
	logoutReq.AddCookie(&http.Cookie{Name: adminSessionCookie, Value: token})
	rec = httptest.NewRecorder()
	handleAdminLogout(rec, logoutReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d", rec.Code)
	}
	if adminSessions.Valid(token, time.Now()) {
		t.Fatal("logout must invalidate the session")
	}
}

func TestAdminLoginWithoutPassword(t *testing.T) {
	oldPool := pool
	t.Cleanup(func() {
		pool = oldPool
		adminSessions.Clear()
	})
	pool = &AccountPool{}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/api/login", strings.NewReader(`{"password":"x"}`))
	handleAdminLogin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
