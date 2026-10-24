package adminapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cline-go-proxy/internal/domain"
	"cline-go-proxy/internal/httpx"
)

// Imports serves SSO-cookie and batch refresh-token account imports.
type Imports struct {
	Store       AccountStore
	Refresh     func(refreshToken string) (RefreshedAccount, error)
	IsDuplicate func(token string, batchSeen map[string]bool) bool
	Truncate    func(s string, maxLen int) string
	Now         func() time.Time
	Translate   func(*http.Request, string, ...any) string
}

func (h *Imports) msg(r *http.Request, key string, args ...any) string {
	if h.Translate != nil {
		return h.Translate(r, key, args...)
	}
	return key
}

func (h *Imports) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *Imports) truncate(s string, maxLen int) string {
	if h.Truncate != nil {
		return h.Truncate(s, maxLen)
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func (h *Imports) duplicate(token string, seen map[string]bool) bool {
	if h.IsDuplicate != nil {
		return h.IsDuplicate(token, seen)
	}
	if seen[token] {
		return true
	}
	for _, a := range h.Store.List() {
		if a.RefreshToken == token {
			return true
		}
	}
	seen[token] = true
	return false
}

func (h *Imports) newAccount(email, token string, refreshed RefreshedAccount) *domain.Account {
	now := h.now()
	return &domain.Account{
		AccountID:    fmt.Sprintf("acc_%d", now.UnixMilli()),
		Email:        email,
		RefreshToken: token,
		AccessToken:  "workos:" + refreshed.AccessToken,
		ExpiresAt:    refreshed.ExpiresAt - 60000,
		Status:       "active",
		CreatedAt:    now,
	}
}

// SSOImport handles POST /admin/api/sso/import with {ssoCookies, email}.
func (h *Imports) SSOImport(w http.ResponseWriter, r *http.Request) {
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
		SSOCookies string `json:"ssoCookies"`
		Email      string `json:"email"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_json")})
		return
	}
	if req.SSOCookies == "" {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "sso_cookies_required")})
		return
	}

	imported := 0
	duplicates := 0
	failures := []string{}
	seen := make(map[string]bool)

	for _, line := range strings.Split(req.SSOCookies, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Accept either a bare refresh token or a workos:-prefixed cookie value.
		if !strings.HasPrefix(line, "workos:") && len(line) <= 20 {
			continue
		}
		token := strings.TrimSpace(strings.TrimPrefix(line, "workos:"))
		if token == "" {
			continue
		}
		if h.duplicate(token, seen) {
			duplicates++
			continue
		}
		refreshed, err := h.Refresh(token)
		if err != nil {
			failures = append(failures, fmt.Sprintf("token %s...: %v", h.truncate(token, 16), err))
			continue
		}
		email := req.Email
		if email == "" {
			email = fmt.Sprintf("sso_user_%d", h.now().UnixMilli())
		}
		h.Store.Add(h.newAccount(email, token, refreshed))
		imported++
	}

	result := map[string]any{
		"imported":   imported,
		"failed":     len(failures),
		"duplicates": duplicates,
	}
	if len(failures) > 0 {
		result["errors"] = failures
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{
		Success: true,
		Message: h.msg(r, "imported_accounts", imported, len(failures), duplicates),
		Data:    result,
	})
}

// BatchImport handles POST /admin/api/batch-import with {tokens:[{refreshToken,email}]}.
func (h *Imports) BatchImport(w http.ResponseWriter, r *http.Request) {
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
		Tokens []struct {
			RefreshToken string `json:"refreshToken"`
			Email        string `json:"email"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_json")})
		return
	}
	if len(req.Tokens) == 0 {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "tokens_empty")})
		return
	}

	imported := 0
	duplicates := 0
	failures := []string{}
	seen := make(map[string]bool)

	for _, t := range req.Tokens {
		token := strings.TrimSpace(t.RefreshToken)
		if token == "" {
			continue
		}
		if h.duplicate(token, seen) {
			duplicates++
			continue
		}
		refreshed, err := h.Refresh(token)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", t.Email, err))
			continue
		}
		email := t.Email
		if email == "" {
			email = fmt.Sprintf("batch_%d", h.now().UnixMilli())
		}
		h.Store.Add(h.newAccount(email, token, refreshed))
		imported++
	}

	httpx.WriteAPI(w, http.StatusOK, httpx.Response{
		Success: true,
		Message: h.msg(r, "imported_accounts", imported, len(failures), duplicates),
		Data: map[string]any{
			"imported":   imported,
			"failed":     len(failures),
			"duplicates": duplicates,
			"errors":     failures,
		},
	})
}
