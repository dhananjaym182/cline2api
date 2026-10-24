package adminapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cline-go-proxy/internal/domain"
)

func newImportsHandler() (*Imports, *fakeAccountStore) {
	store := &fakeAccountStore{}
	h := &Imports{
		Store: store,
		Refresh: func(token string) (RefreshedAccount, error) {
			if strings.HasPrefix(token, "bad") {
				return RefreshedAccount{}, errors.New("rejected")
			}
			return RefreshedAccount{AccessToken: "at-" + token, ExpiresAt: 1_700_000_000_000}, nil
		},
		Now:       func() time.Time { return time.UnixMilli(1_700_000_000_000) },
		Translate: func(_ *http.Request, key string, _ ...any) string { return key },
	}
	return h, store
}

func TestSSOImportAddsTokensAndSkipsShortOrDuplicate(t *testing.T) {
	h, store := newImportsHandler()
	body := strings.Join([]string{
		"workos:tok-one",
		"tok-two-is-long-enough",
		"short",
		"tok-two-is-long-enough",
		"",
	}, "\n")

	rec := httptest.NewRecorder()
	h.SSOImport(rec, httptest.NewRequest(http.MethodPost, "/admin/api/sso/import",
		strings.NewReader(`{"ssoCookies":`+mustJSON(body)+`,"email":"sso@x"}`)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if len(store.accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(store.accounts))
	}
	for _, a := range store.accounts {
		if a.Email != "sso@x" || a.Status != "active" {
			t.Fatalf("account = %+v", a)
		}
		if a.ExpiresAt != 1_700_000_000_000-60000 {
			t.Fatalf("expiresAt = %d", a.ExpiresAt)
		}
	}
	data := decodeEnvelope(t, rec).Data.(map[string]any)
	if data["imported"].(float64) != 2 {
		t.Fatalf("imported = %v", data["imported"])
	}
	if _, ok := data["errors"]; ok {
		t.Fatalf("unexpected errors: %v", data["errors"])
	}
}

func TestSSOImportReportsFailuresAndDefaultsEmail(t *testing.T) {
	h, store := newImportsHandler()
	body := "bad-token-long-enough"

	rec := httptest.NewRecorder()
	h.SSOImport(rec, httptest.NewRequest(http.MethodPost, "/admin/api/sso/import",
		strings.NewReader(`{"ssoCookies":"`+body+`"}`)))

	data := decodeEnvelope(t, rec).Data.(map[string]any)
	if data["failed"].(float64) != 1 {
		t.Fatalf("failed = %v", data["failed"])
	}
	errs := data["errors"].([]any)
	if len(errs) != 1 || !strings.Contains(errs[0].(string), "bad-token-long-e") {
		t.Fatalf("errors = %v", errs)
	}
	if len(store.accounts) != 0 {
		t.Fatalf("accounts = %d", len(store.accounts))
	}

	// Successful import with no email uses the timestamped default.
	rec = httptest.NewRecorder()
	h.SSOImport(rec, httptest.NewRequest(http.MethodPost, "/admin/api/sso/import",
		strings.NewReader(`{"ssoCookies":"good-token-long-enough"}`)))
	if got := store.accounts[0].Email; got != "sso_user_1700000000000" {
		t.Fatalf("email = %q", got)
	}
}

func TestSSOImportRequiresCookies(t *testing.T) {
	h, _ := newImportsHandler()
	rec := httptest.NewRecorder()
	h.SSOImport(rec, httptest.NewRequest(http.MethodPost, "/admin/api/sso/import", strings.NewReader(`{}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "sso_cookies_required" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func TestBatchImportCountsImportedDuplicatesAndFailures(t *testing.T) {
	h, store := newImportsHandler()
	store.accounts = []*domain.Account{{AccountID: "old", RefreshToken: "existing-token"}}
	body := `{"tokens":[
		{"refreshToken":"existing-token","email":"dupe@x"},
		{"refreshToken":"new-token-1","email":"one@x"},
		{"refreshToken":"new-token-1","email":"repeat@x"},
		{"refreshToken":"bad-token-2","email":"bad@x"},
		{"refreshToken":"  ","email":"empty@x"}
	]}`

	rec := httptest.NewRecorder()
	h.BatchImport(rec, httptest.NewRequest(http.MethodPost, "/admin/api/batch-import", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data := decodeEnvelope(t, rec).Data.(map[string]any)
	if data["imported"].(float64) != 1 {
		t.Fatalf("imported = %v", data["imported"])
	}
	if data["duplicates"].(float64) != 2 {
		t.Fatalf("duplicates = %v", data["duplicates"])
	}
	if data["failed"].(float64) != 1 {
		t.Fatalf("failed = %v", data["failed"])
	}
	errs := data["errors"].([]any)
	if len(errs) != 1 || !strings.HasPrefix(errs[0].(string), "bad@x:") {
		t.Fatalf("errors = %v", errs)
	}
	if len(store.accounts) != 2 || store.accounts[1].Email != "one@x" {
		t.Fatalf("accounts = %+v", store.accounts)
	}
}

func TestBatchImportDefaultEmail(t *testing.T) {
	h, store := newImportsHandler()
	rec := httptest.NewRecorder()
	h.BatchImport(rec, httptest.NewRequest(http.MethodPost, "/admin/api/batch-import",
		strings.NewReader(`{"tokens":[{"refreshToken":"tok"}]}`)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := store.accounts[0].Email; got != "batch_1700000000000" {
		t.Fatalf("email = %q", got)
	}
}

func TestBatchImportRejectsEmpty(t *testing.T) {
	h, _ := newImportsHandler()
	rec := httptest.NewRecorder()
	h.BatchImport(rec, httptest.NewRequest(http.MethodPost, "/admin/api/batch-import", strings.NewReader(`{"tokens":[]}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "tokens_empty" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func mustJSON(s string) string {
	b := strings.Builder{}
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
