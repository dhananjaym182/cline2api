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

type fakeAccountStore struct {
	accounts []*domain.Account
	poolIdx  int
	removed  string
}

func (f *fakeAccountStore) List() []*domain.Account { return f.accounts }

func (f *fakeAccountStore) PoolIndex() int { return f.poolIdx }

func (f *fakeAccountStore) FindByRefreshToken(token string) *domain.Account {
	for _, a := range f.accounts {
		if a.RefreshToken == token {
			return a
		}
	}
	return nil
}

func (f *fakeAccountStore) Add(acc *domain.Account) { f.accounts = append(f.accounts, acc) }

func (f *fakeAccountStore) Remove(id string) bool {
	for i, a := range f.accounts {
		if a.AccountID == id {
			f.accounts = append(f.accounts[:i], f.accounts[i+1:]...)
			f.removed = id
			return true
		}
	}
	return false
}

func accountHandler(store AccountStore) *Accounts {
	return &Accounts{
		Store: store,
		Refresh: func(token string) (RefreshedAccount, error) {
			if token == "bad" {
				return RefreshedAccount{}, errors.New("rejected")
			}
			return RefreshedAccount{AccessToken: "access-" + token, ExpiresAt: 1_700_000_000_000}, nil
		},
		Now:       func() time.Time { return time.UnixMilli(1_690_000_000_000) },
		Translate: func(_ *http.Request, key string, _ ...any) string { return key },
	}
}

func TestAccountsList(t *testing.T) {
	store := &fakeAccountStore{
		accounts: []*domain.Account{{AccountID: "a1"}, {AccountID: "a2"}},
		poolIdx:  1,
	}
	rec := httptest.NewRecorder()
	accountHandler(store).List(rec, httptest.NewRequest(http.MethodGet, "/admin/api/accounts", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data := decodeEnvelope(t, rec).Data.(map[string]any)
	if total := data["total"].(float64); total != 2 {
		t.Fatalf("total = %v", total)
	}
	if idx := data["poolIndex"].(float64); idx != 1 {
		t.Fatalf("poolIndex = %v", idx)
	}
}

func TestAccountsListRejectsPost(t *testing.T) {
	rec := httptest.NewRecorder()
	accountHandler(&fakeAccountStore{}).List(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestAccountsAddValidatesAndStores(t *testing.T) {
	store := &fakeAccountStore{accounts: []*domain.Account{{AccountID: "old"}}}
	rec := httptest.NewRecorder()
	accountHandler(store).Add(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/add",
		strings.NewReader(`{"refreshToken":"rt","email":"a@b.c"}`)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if len(store.accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(store.accounts))
	}
	added := store.accounts[1]
	if added.AccountID != "acc_1690000000000" {
		t.Fatalf("accountID = %q", added.AccountID)
	}
	if added.AccessToken != "workos:access-rt" {
		t.Fatalf("accessToken = %q", added.AccessToken)
	}
	if added.ExpiresAt != 1_700_000_000_000-60000 {
		t.Fatalf("expiresAt = %d", added.ExpiresAt)
	}
	if added.Status != "active" || added.Email != "a@b.c" {
		t.Fatalf("account = %+v", added)
	}
}

func TestAccountsAddDefaultsEmail(t *testing.T) {
	store := &fakeAccountStore{accounts: []*domain.Account{{}, {}}}
	rec := httptest.NewRecorder()
	accountHandler(store).Add(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/add",
		strings.NewReader(`{"refreshToken":"rt"}`)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := store.accounts[2].Email; got != "user_3" {
		t.Fatalf("email = %q, want user_3", got)
	}
}

func TestAccountsAddDuplicateReturnsExisting(t *testing.T) {
	store := &fakeAccountStore{accounts: []*domain.Account{{
		AccountID: "a1", Email: "existing@x", RefreshToken: "rt", Status: "active",
	}}}
	rec := httptest.NewRecorder()
	accountHandler(store).Add(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/add",
		strings.NewReader(`{"refreshToken":"rt"}`)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(store.accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(store.accounts))
	}
	data := decodeEnvelope(t, rec).Data.(map[string]any)
	if dup, _ := data["duplicate"].(bool); !dup {
		t.Fatal("expected duplicate flag")
	}
	if data["accountId"] != "a1" {
		t.Fatalf("accountId = %v", data["accountId"])
	}
}

func TestAccountsAddRejectsMissingToken(t *testing.T) {
	rec := httptest.NewRecorder()
	accountHandler(&fakeAccountStore{}).Add(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/add",
		strings.NewReader(`{}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "refresh_token_required" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func TestAccountsAddRejectsRejectedToken(t *testing.T) {
	rec := httptest.NewRecorder()
	accountHandler(&fakeAccountStore{}).Add(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/add",
		strings.NewReader(`{"refreshToken":"bad"}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "invalid_refresh_token" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func TestAccountsAddRejectsInvalidJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	accountHandler(&fakeAccountStore{}).Add(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/add",
		strings.NewReader("{")))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "invalid_json" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func TestAccountsDelete(t *testing.T) {
	store := &fakeAccountStore{accounts: []*domain.Account{{AccountID: "a1"}}}
	h := accountHandler(store)

	rec := httptest.NewRecorder()
	h.Delete(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/delete", strings.NewReader(`{"accountId":"a1"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.Delete(rec, httptest.NewRequest(http.MethodPost, "/admin/api/accounts/delete", strings.NewReader(`{"accountId":"a1"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "account_not_found" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func TestAccountsDeleteRequiresID(t *testing.T) {
	rec := httptest.NewRecorder()
	accountHandler(&fakeAccountStore{}).Delete(rec, httptest.NewRequest(http.MethodPost,
		"/admin/api/accounts/delete", strings.NewReader(`{}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "account_id_required" {
		t.Fatalf("error = %q", resp.Error)
	}
}
