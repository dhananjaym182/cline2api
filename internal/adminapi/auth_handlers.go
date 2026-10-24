package adminapi

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"cline-go-proxy/internal/httpx"
)

// AuthHandlers serves administrator login, logout, and password changes.
// Password storage and verification are injected by the composition layer.
type AuthHandlers struct {
	Sessions       *SessionStore
	PasswordSet    func() bool
	VerifyPassword func(password string) bool
	SetPassword    func(password string)
	Translate      func(*http.Request, string) string
	// Sleep is used before rejecting a wrong password; tests inject a no-op.
	Sleep func(time.Duration)
}

func (h *AuthHandlers) msg(r *http.Request, key string) string {
	if h.Translate != nil {
		return h.Translate(r, key)
	}
	return key
}

func (h *AuthHandlers) sleep(d time.Duration) {
	if h.Sleep != nil {
		h.Sleep(d)
		return
	}
	time.Sleep(d)
}

// Login handles POST /admin/api/login with body {"password": "..."}.
func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: h.msg(r, "method_not_allowed")})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_json")})
		return
	}
	if h.PasswordSet == nil || !h.PasswordSet() {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "password_not_enabled")})
		return
	}
	if h.VerifyPassword == nil || !h.VerifyPassword(req.Password) {
		h.sleep(500 * time.Millisecond)
		httpx.WriteAPI(w, http.StatusUnauthorized, httpx.Response{Error: h.msg(r, "wrong_password")})
		return
	}
	token := RandomHex(32)
	h.Sessions.Add(token, time.Now().Add(SessionTTL))
	SetSessionCookie(w, token)
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Message: h.msg(r, "login_ok")})
}

// Logout handles POST /admin/api/logout.
func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookieName); err == nil {
		h.Sessions.Remove(c.Value)
	}
	ClearSessionCookie(w)
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Message: h.msg(r, "logout_ok")})
}

// Password handles POST /admin/api/password with body {"password": "..."}.
// An empty password clears the configured password.
func (h *AuthHandlers) Password(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: h.msg(r, "method_not_allowed")})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_json")})
		return
	}
	if h.SetPassword == nil {
		httpx.WriteAPI(w, http.StatusInternalServerError, httpx.Response{Error: "password store unavailable"})
		return
	}
	h.SetPassword(req.Password)
	if req.Password == "" {
		httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Message: h.msg(r, "password_cleared")})
		return
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Message: h.msg(r, "password_updated")})
}
