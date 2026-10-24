package adminapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"cline-go-proxy/internal/domain"
	"cline-go-proxy/internal/httpx"
)

// AccountStore is the Cline account pool as seen by account administration.
// Persistence and pool locking stay in the composition layer.
type AccountStore interface {
	List() []*domain.Account
	PoolIndex() int
	FindByRefreshToken(token string) *domain.Account
	Add(*domain.Account)
	Remove(accountID string) bool
}

// RefreshedAccount is the outcome of validating a refresh token upstream.
// ExpiresAt is an absolute unix-millisecond deadline.
type RefreshedAccount struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
}

// Accounts serves Cline account pool administration.
type Accounts struct {
	Store     AccountStore
	Refresh   func(refreshToken string) (RefreshedAccount, error)
	Now       func() time.Time
	Translate func(*http.Request, string, ...any) string
	Logf      func(format string, args ...any)
}

func (h *Accounts) msg(r *http.Request, key string, args ...any) string {
	if h.Translate != nil {
		return h.Translate(r, key, args...)
	}
	return key
}

func (h *Accounts) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *Accounts) logf(format string, args ...any) {
	if h.Logf != nil {
		h.Logf(format, args...)
	}
}

// List handles GET /admin/api/accounts.
func (h *Accounts) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: h.msg(r, "method_not_allowed")})
		return
	}
	accounts := h.Store.List()
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: map[string]any{
		"accounts":  accounts,
		"total":     len(accounts),
		"poolIndex": h.Store.PoolIndex(),
	}})
}

// Add handles POST /admin/api/accounts/add with body {refreshToken, email}.
func (h *Accounts) Add(w http.ResponseWriter, r *http.Request) {
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
		RefreshToken string `json:"refreshToken"`
		Email        string `json:"email"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_json")})
		return
	}
	if req.RefreshToken == "" {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "refresh_token_required")})
		return
	}

	if existing := h.Store.FindByRefreshToken(req.RefreshToken); existing != nil {
		httpx.WriteAPI(w, http.StatusOK, httpx.Response{
			Success: true,
			Message: h.msg(r, "account_exists", existing.Email),
			Data: map[string]any{
				"accountId": existing.AccountID,
				"email":     existing.Email,
				"status":    existing.Status,
				"duplicate": true,
			},
		})
		return
	}

	if h.Refresh == nil {
		httpx.WriteAPI(w, http.StatusInternalServerError, httpx.Response{Error: "refresh validation unavailable"})
		return
	}
	refreshed, err := h.Refresh(req.RefreshToken)
	if err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_refresh_token", err.Error())})
		return
	}

	now := h.now()
	if req.Email == "" {
		req.Email = fmt.Sprintf("user_%d", len(h.Store.List())+1)
	}
	acc := &domain.Account{
		AccountID:    fmt.Sprintf("acc_%d", now.UnixMilli()),
		Email:        req.Email,
		RefreshToken: req.RefreshToken,
		AccessToken:  "workos:" + refreshed.AccessToken,
		ExpiresAt:    refreshed.ExpiresAt - 60000,
		Status:       "active",
		CreatedAt:    now,
	}
	if refreshed.RefreshToken != "" {
		acc.RefreshToken = refreshed.RefreshToken
	}

	h.Store.Add(acc)
	h.logf("Account added via API: %s", req.Email)

	httpx.WriteAPI(w, http.StatusOK, httpx.Response{
		Success: true,
		Message: h.msg(r, "account_added", req.Email),
		Data: map[string]any{
			"accountId": acc.AccountID,
			"email":     acc.Email,
			"status":    acc.Status,
		},
	})
}

// Delete handles POST /admin/api/accounts/delete with body {accountId}.
func (h *Accounts) Delete(w http.ResponseWriter, r *http.Request) {
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
		AccountID string `json:"accountId"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_json")})
		return
	}
	if req.AccountID == "" {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "account_id_required")})
		return
	}

	if h.Store.Remove(req.AccountID) {
		httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Message: h.msg(r, "account_deleted")})
		return
	}
	httpx.WriteAPI(w, http.StatusNotFound, httpx.Response{Error: h.msg(r, "account_not_found")})
}
