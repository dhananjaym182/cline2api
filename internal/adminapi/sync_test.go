package adminapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newSyncHandler() *Sync {
	return &Sync{
		ModelSync: func() (any, string) {
			return map[string]any{"total": 3}, ""
		},
		ZenModelSync: func() (any, string) {
			return map[string]any{"total": 7}, ""
		},
		RecheckAll: func() any {
			return []map[string]any{{"id": "p1"}}
		},
		RecheckOne: func(id string) (any, error) {
			if id == "missing" {
				return nil, errors.New("provider not found")
			}
			return map[string]any{"id": id, "ok": true}, nil
		},
		SyncStatus: func() map[string]any {
			return map[string]any{"p1": map[string]any{"lastCheck": "now"}}
		},
		Translate: func(_ *http.Request, key string) string { return key },
	}
}

func TestSyncModelsSuccess(t *testing.T) {
	rec := httptest.NewRecorder()
	newSyncHandler().Models(rec, httptest.NewRequest(http.MethodPost, "/admin/api/models/sync", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := decodeEnvelope(t, rec)
	if !resp.Success || resp.Message != "model_sync_done" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestSyncModelsError(t *testing.T) {
	h := newSyncHandler()
	h.ModelSync = func() (any, string) { return nil, "upstream down" }

	rec := httptest.NewRecorder()
	h.Models(rec, httptest.NewRequest(http.MethodPost, "/admin/api/models/sync", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	resp := decodeEnvelope(t, rec)
	if resp.Error != "upstream down" || resp.Message != "model_sync_failed" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestSyncZenModels(t *testing.T) {
	rec := httptest.NewRecorder()
	newSyncHandler().ZenModels(rec, httptest.NewRequest(http.MethodPost, "/admin/api/opencode/models/sync", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	newSyncHandler().ZenModels(rec, httptest.NewRequest(http.MethodGet, "/admin/api/opencode/models/sync", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestSyncProviderRecheckAll(t *testing.T) {
	rec := httptest.NewRecorder()
	newSyncHandler().ProviderRecheck(rec, httptest.NewRequest(http.MethodPost, "/admin/api/providers/recheck",
		strings.NewReader(`{"all":true}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data := decodeEnvelope(t, rec).Data.(map[string]any)
	if results, ok := data["results"].([]any); !ok || len(results) != 1 {
		t.Fatalf("data = %v", data)
	}
}

func TestSyncProviderRecheckAllBusy(t *testing.T) {
	h := newSyncHandler()
	h.RecheckAll = func() any { return nil }

	rec := httptest.NewRecorder()
	h.ProviderRecheck(rec, httptest.NewRequest(http.MethodPost, "/admin/api/providers/recheck",
		strings.NewReader(`{"all":true}`)))

	data := decodeEnvelope(t, rec).Data.(map[string]any)
	if busy, _ := data["busy"].(bool); !busy {
		t.Fatalf("data = %v", data)
	}
}

func TestSyncProviderRecheckOne(t *testing.T) {
	h := newSyncHandler()

	rec := httptest.NewRecorder()
	h.ProviderRecheck(rec, httptest.NewRequest(http.MethodPost, "/admin/api/providers/recheck",
		strings.NewReader(`{"id":"p1"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ProviderRecheck(rec, httptest.NewRequest(http.MethodPost, "/admin/api/providers/recheck",
		strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "id or all required" {
		t.Fatalf("error = %q", resp.Error)
	}

	rec = httptest.NewRecorder()
	h.ProviderRecheck(rec, httptest.NewRequest(http.MethodPost, "/admin/api/providers/recheck",
		strings.NewReader(`{"id":"missing"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "provider not found" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func TestSyncProviderStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	newSyncHandler().ProviderStatus(rec, httptest.NewRequest(http.MethodGet, "/admin/api/providers/sync-status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data := decodeEnvelope(t, rec).Data.(map[string]any)
	providers := data["providers"].(map[string]any)
	if _, ok := providers["p1"]; !ok {
		t.Fatalf("providers = %v", providers)
	}
}
