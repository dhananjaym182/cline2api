package adminapi

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"cline-go-proxy/internal/domain"
	"cline-go-proxy/internal/httpx"
)

// AccountOps serves account-pool maintenance: bulk refresh, bulk delete,
// reset, connectivity tests, export, and opening external links.
type AccountOps struct {
	// All returns a snapshot of every account.
	All func() []*domain.Account
	// Get returns one account by ID, or nil.
	Get func(accountID string) *domain.Account
	// DoRefreshAll refreshes every account token under the pool lock.
	DoRefreshAll func()
	// Refresh refreshes one account token.
	Refresh func(*domain.Account) error
	// ClearAll clears the account pool and persists it.
	ClearAll func()
	// Probe checks one account against the upstream and returns a JSON-able result.
	Probe func(*domain.Account) any
	// OpenBrowser launches the OS default browser for an already-validated URL.
	OpenBrowser func(url string) error

	Now       func() time.Time
	Translate func(*http.Request, string, ...any) string
}

func (h *AccountOps) msg(r *http.Request, key string, args ...any) string {
	if h.Translate != nil {
		return h.Translate(r, key, args...)
	}
	return key
}

func (h *AccountOps) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// RefreshAll handles POST /admin/api/accounts/refresh-all.
func (h *AccountOps) RefreshAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: h.msg(r, "method_not_allowed")})
		return
	}
	h.DoRefreshAll()
}

// DeleteAll handles POST /admin/api/accounts/delete-all.
func (h *AccountOps) DeleteAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: h.msg(r, "method_not_allowed")})
		return
	}
	h.ClearAll()
}

// Reset handles POST /admin/api/accounts/reset with body {accountId}.
func (h *AccountOps) Reset(w http.ResponseWriter, r *http.Request) {
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

	acc := h.Get(req.AccountID)
	if acc == nil {
		httpx.WriteAPI(w, http.StatusNotFound, httpx.Response{Error: h.msg(r, "account_not_found")})
		return
	}

	// Reset status to active and refresh the token; usage statistics are kept.
	acc.Status = "active"
	if err := h.Refresh(acc); err != nil {
		httpx.WriteAPI(w, http.StatusInternalServerError, httpx.Response{Error: h.msg(r, "reset_failed", err.Error())})
		return
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Message: h.msg(r, "account_reset")})
}

// Test handles POST /admin/api/accounts/test with an optional {accountId}.
func (h *AccountOps) Test(w http.ResponseWriter, r *http.Request) {
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
	_ = json.Unmarshal(body, &req)

	var targets []*domain.Account
	if req.AccountID != "" {
		acc := h.Get(req.AccountID)
		if acc == nil {
			httpx.WriteAPI(w, http.StatusNotFound, httpx.Response{Error: h.msg(r, "account_not_found")})
			return
		}
		targets = []*domain.Account{acc}
	} else {
		targets = h.All()
	}

	results := make([]any, 0, len(targets))
	for _, acc := range targets {
		results = append(results, h.Probe(acc))
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: map[string]any{"results": results}})
}

// Export handles GET/POST /admin/api/accounts/export. POST accepts {ids} to
// restrict the export; GET exports every account with a refresh token.
func (h *AccountOps) Export(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: h.msg(r, "method_not_allowed")})
		return
	}

	want := map[string]bool{}
	if r.Method == http.MethodPost {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: err.Error()})
			return
		}
		defer r.Body.Close()
		var req struct {
			IDs []string `json:"ids"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_json")})
			return
		}
		for _, id := range req.IDs {
			if id != "" {
				want[id] = true
			}
		}
	}

	type exportToken struct {
		RefreshToken string `json:"refreshToken"`
		Email        string `json:"email"`
	}
	accounts := h.All()
	tokens := make([]exportToken, 0, len(accounts))
	for _, acc := range accounts {
		if acc.RefreshToken != "" && (len(want) == 0 || want[acc.AccountID]) {
			tokens = append(tokens, exportToken{RefreshToken: acc.RefreshToken, Email: acc.Email})
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="cline-accounts-export.json"`)
	json.NewEncoder(w).Encode(map[string]any{
		"tokens":     tokens,
		"exportedAt": h.now().Format(time.RFC3339),
	})
}

// OpenExternal handles GET /admin/api/open-external?url=... by handing an
// http(s) URL to the OS default browser.
func (h *AccountOps) OpenExternal(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "url_required")})
		return
	}
	if !hasHTTPPrefix(url) {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "url_http_only")})
		return
	}
	if err := h.OpenBrowser(url); err != nil {
		httpx.WriteAPI(w, http.StatusInternalServerError, httpx.Response{Error: err.Error()})
		return
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true})
}

func hasHTTPPrefix(url string) bool {
	if len(url) >= 7 && url[:7] == "http://" {
		return true
	}
	return len(url) >= 8 && url[:8] == "https://"
}
