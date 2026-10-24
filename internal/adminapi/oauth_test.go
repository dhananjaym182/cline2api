package adminapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newOAuthHandler(store *fakeAccountStore) *OAuth {
	return &OAuth{
		Store: store,
		StartDevice: func() (DeviceAuthorization, error) {
			return DeviceAuthorization{
				DeviceCode:      "dev",
				UserCode:        "user",
				VerificationURI: "https://verify",
				Interval:        1,
				ExpiresIn:       1,
			}, nil
		},
		PollToken: func(string, int, int) (OAuthToken, error) {
			return OAuthToken{AccessToken: "wa", RefreshToken: "wr"}, nil
		},
		RegisterAccount: func(access, refresh string) (OAuthAccount, error) {
			return OAuthAccount{
				Email:        "oauth@x",
				AccessToken:  "ca-" + access,
				RefreshToken: "cr-" + refresh,
				ExpiresAt:    1_700_000_000_000,
			}, nil
		},
		Now:       func() time.Time { return time.UnixMilli(1_700_000_000_000) },
		Translate: func(_ *http.Request, key string) string { return key },
	}
}

func waitOAuthDone(t *testing.T, sessionID string, h *OAuth) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec := httptest.NewRecorder()
		h.Status(rec, httptest.NewRequest(http.MethodGet, "/admin/api/oauth/status?sessionId="+sessionID, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		data := decodeEnvelope(t, rec).Data.(map[string]any)
		if done, _ := data["done"].(bool); done {
			return data
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("oauth session did not finish")
	return nil
}

func TestOAuthStartAndComplete(t *testing.T) {
	store := &fakeAccountStore{}
	h := newOAuthHandler(store)

	rec := httptest.NewRecorder()
	h.Start(rec, httptest.NewRequest(http.MethodPost, "/admin/api/oauth/start", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	data := decodeEnvelope(t, rec).Data.(map[string]any)
	sessionID := data["sessionId"].(string)
	if sessionID != "oauth_1700000000000" {
		t.Fatalf("sessionId = %q", sessionID)
	}
	if data["verificationUri"] != "https://verify" || data["userCode"] != "user" {
		t.Fatalf("data = %v", data)
	}

	status := waitOAuthDone(t, sessionID, h)
	if status["success"] != true || status["email"] != "oauth@x" {
		t.Fatalf("status = %v", status)
	}
	if len(store.accounts) != 1 {
		t.Fatalf("accounts = %d", len(store.accounts))
	}
	acc := store.accounts[0]
	if acc.AccessToken != "workos:ca-wa" || acc.RefreshToken != "cr-wr" {
		t.Fatalf("account = %+v", acc)
	}
	if acc.ExpiresAt != 1_700_000_000_000-60000 {
		t.Fatalf("expiresAt = %d", acc.ExpiresAt)
	}
}

func TestOAuthPollFailureMarksSessionFailed(t *testing.T) {
	h := newOAuthHandler(&fakeAccountStore{})
	h.PollToken = func(string, int, int) (OAuthToken, error) {
		return OAuthToken{}, errors.New("denied")
	}

	rec := httptest.NewRecorder()
	h.Start(rec, httptest.NewRequest(http.MethodPost, "/admin/api/oauth/start", nil))
	sessionID := decodeEnvelope(t, rec).Data.(map[string]any)["sessionId"].(string)

	status := waitOAuthDone(t, sessionID, h)
	if status["success"] != false || status["error"] != "denied" {
		t.Fatalf("status = %v", status)
	}
}

func TestOAuthRegisterFailureMarksSessionFailed(t *testing.T) {
	h := newOAuthHandler(&fakeAccountStore{})
	h.RegisterAccount = func(string, string) (OAuthAccount, error) {
		return OAuthAccount{}, errors.New("register failed")
	}

	rec := httptest.NewRecorder()
	h.Start(rec, httptest.NewRequest(http.MethodPost, "/admin/api/oauth/start", nil))
	sessionID := decodeEnvelope(t, rec).Data.(map[string]any)["sessionId"].(string)

	status := waitOAuthDone(t, sessionID, h)
	if status["success"] != false || status["error"] != "register failed" {
		t.Fatalf("status = %v", status)
	}
}

func TestOAuthStartDeviceError(t *testing.T) {
	h := newOAuthHandler(&fakeAccountStore{})
	h.StartDevice = func() (DeviceAuthorization, error) { return DeviceAuthorization{}, errors.New("upstream") }

	rec := httptest.NewRecorder()
	h.Start(rec, httptest.NewRequest(http.MethodPost, "/admin/api/oauth/start", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "upstream" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func TestOAuthStatusValidation(t *testing.T) {
	h := newOAuthHandler(&fakeAccountStore{})

	rec := httptest.NewRecorder()
	h.Status(rec, httptest.NewRequest(http.MethodGet, "/admin/api/oauth/status", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "session_id_required" {
		t.Fatalf("error = %q", resp.Error)
	}

	rec = httptest.NewRecorder()
	h.Status(rec, httptest.NewRequest(http.MethodGet, "/admin/api/oauth/status?sessionId=nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "session_not_found" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func TestOAuthStartRejectsGet(t *testing.T) {
	h := newOAuthHandler(&fakeAccountStore{})
	rec := httptest.NewRecorder()
	h.Start(rec, httptest.NewRequest(http.MethodGet, "/admin/api/oauth/start", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
