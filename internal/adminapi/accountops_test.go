package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cline-go-proxy/internal/domain"
)

type accountOpsFixture struct {
	accounts  []*domain.Account
	refreshed int
	refreshEr error
	cleared   int
	opened    string
	openErr   error
}

func newAccountOps() (*AccountOps, *accountOpsFixture) {
	f := &accountOpsFixture{accounts: []*domain.Account{
		{AccountID: "a1", Email: "a1@x", RefreshToken: "rt1"},
		{AccountID: "a2", Email: "a2@x"},
	}}
	h := &AccountOps{
		All: func() []*domain.Account {
			out := make([]*domain.Account, len(f.accounts))
			copy(out, f.accounts)
			return out
		},
		Get: func(id string) *domain.Account {
			for _, a := range f.accounts {
				if a.AccountID == id {
					return a
				}
			}
			return nil
		},
		DoRefreshAll: func() { f.refreshed += len(f.accounts) },
		Refresh: func(*domain.Account) error {
			if f.refreshEr != nil {
				return f.refreshEr
			}
			f.refreshed++
			return nil
		},
		ClearAll: func() { f.cleared++; f.accounts = nil },
		Probe: func(a *domain.Account) any {
			return map[string]any{"accountId": a.AccountID, "ok": true}
		},
		OpenBrowser: func(url string) error {
			f.opened = url
			return f.openErr
		},
		Now:       func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
		Translate: func(_ *http.Request, key string, _ ...any) string { return key },
	}
	return h, f
}

func TestAccountOpsRefreshAll(t *testing.T) {
	h, f := newAccountOps()
	rec := httptest.NewRecorder()
	h.RefreshAll(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/refresh-all", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if f.refreshed != 2 {
		t.Fatalf("refreshed = %d, want 2", f.refreshed)
	}

	rec = httptest.NewRecorder()
	h.RefreshAll(rec, httptest.NewRequest(http.MethodGet, "/admin/api/accounts/refresh-all", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestAccountOpsDeleteAll(t *testing.T) {
	h, f := newAccountOps()
	rec := httptest.NewRecorder()
	h.DeleteAll(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/delete-all", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if f.cleared != 1 || len(f.accounts) != 0 {
		t.Fatalf("cleared = %d, accounts = %d", f.cleared, len(f.accounts))
	}
}

func TestAccountOpsReset(t *testing.T) {
	h, f := newAccountOps()
	f.accounts[0].Status = "error"

	rec := httptest.NewRecorder()
	h.Reset(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/reset", strings.NewReader(`{"accountId":"a1"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if f.accounts[0].Status != "active" {
		t.Fatalf("status = %q", f.accounts[0].Status)
	}
	if f.refreshed != 1 {
		t.Fatalf("refreshed = %d", f.refreshed)
	}

	rec = httptest.NewRecorder()
	h.Reset(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/reset", strings.NewReader(`{"accountId":"missing"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAccountOpsResetRefreshFailure(t *testing.T) {
	h, f := newAccountOps()
	f.refreshEr = errors.New("upstream down")

	rec := httptest.NewRecorder()
	h.Reset(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/reset", strings.NewReader(`{"accountId":"a1"}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "reset_failed" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func TestAccountOpsTestAllAndSingle(t *testing.T) {
	h, _ := newAccountOps()

	rec := httptest.NewRecorder()
	h.Test(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/test", strings.NewReader(`{}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data := decodeEnvelope(t, rec).Data.(map[string]any)
	results := data["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}

	rec = httptest.NewRecorder()
	h.Test(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/test", strings.NewReader(`{"accountId":"a2"}`)))
	results = decodeEnvelope(t, rec).Data.(map[string]any)["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["accountId"] != "a2" {
		t.Fatalf("results = %v", results)
	}

	rec = httptest.NewRecorder()
	h.Test(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/test", strings.NewReader(`{"accountId":"nope"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAccountOpsExportGet(t *testing.T) {
	h, _ := newAccountOps()
	rec := httptest.NewRecorder()
	h.Export(rec, httptest.NewRequest(http.MethodGet, "/admin/api/accounts/export", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "cline-accounts-export.json") {
		t.Fatalf("disposition = %q", got)
	}
	var body struct {
		Tokens []struct {
			RefreshToken string `json:"refreshToken"`
			Email        string `json:"email"`
		} `json:"tokens"`
		ExportedAt string `json:"exportedAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tokens) != 1 || body.Tokens[0].Email != "a1@x" {
		t.Fatalf("tokens = %v", body.Tokens)
	}
	if body.ExportedAt != "2026-01-02T03:04:05Z" {
		t.Fatalf("exportedAt = %q", body.ExportedAt)
	}
}

func TestAccountOpsExportFiltered(t *testing.T) {
	h, f := newAccountOps()
	f.accounts[1].RefreshToken = "rt2"

	rec := httptest.NewRecorder()
	h.Export(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/export", strings.NewReader(`{"ids":["a2"]}`)))
	var body struct {
		Tokens []struct {
			Email string `json:"email"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tokens) != 1 || body.Tokens[0].Email != "a2@x" {
		t.Fatalf("tokens = %v", body.Tokens)
	}
}

func TestAccountOpsOpenExternal(t *testing.T) {
	h, f := newAccountOps()

	for _, raw := range []string{"", "ftp://x", "javascript:alert(1)"} {
		rec := httptest.NewRecorder()
		h.OpenExternal(rec, httptest.NewRequest(http.MethodGet, "/admin/api/open-external?url="+raw, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("url %q status = %d, want 400", raw, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	h.OpenExternal(rec, httptest.NewRequest(http.MethodGet, "/admin/api/open-external?url=https%3A%2F%2Fexample.com", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if f.opened != "https://example.com" {
		t.Fatalf("opened = %q", f.opened)
	}
}

func TestAccountOpsOpenExternalBrowserError(t *testing.T) {
	h, f := newAccountOps()
	f.openErr = errors.New("no browser")

	rec := httptest.NewRecorder()
	h.OpenExternal(rec, httptest.NewRequest(http.MethodGet, "/admin/api/open-external?url=http%3A%2F%2Fexample.com", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "no browser" {
		t.Fatalf("error = %q", resp.Error)
	}
}
