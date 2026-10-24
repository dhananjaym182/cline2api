package adminapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cline-go-proxy/internal/domain"
)

type fakeModelStore struct {
	models    []domain.Model
	added     []domain.Model
	deleted   []string
	deleteRes DeleteModelResult
	ctxCalls  []string
	ctxFound  bool
	disabled  []string
	changed   int
}

func (f *fakeModelStore) List() []domain.Model { return f.models }

func (f *fakeModelStore) Add(m domain.Model) { f.added = append(f.added, m) }

func (f *fakeModelStore) Delete(id string) DeleteModelResult {
	f.deleted = append(f.deleted, id)
	return f.deleteRes
}

func (f *fakeModelStore) UpdateContext(id string, context, output int) bool {
	f.ctxCalls = append(f.ctxCalls, id)
	return f.ctxFound
}

func (f *fakeModelStore) SetDisabled(ids []string, disabled bool) int {
	f.disabled = ids
	return f.changed
}

func newModels(store *fakeModelStore) *Models {
	return &Models{Store: store, Logf: func(string, ...any) {}}
}

func postJSON(path, body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
}

func TestModelsListNormalizes(t *testing.T) {
	store := &fakeModelStore{models: []domain.Model{{ID: "z", Cost: "pass"}}}
	m := newModels(store)
	m.Normalize = func(models []domain.Model) []domain.Model {
		models[0].Cost = "free"
		return models
	}
	m.LastSync = func() any { return map[string]any{"at": 1} }
	rec := httptest.NewRecorder()
	m.List(rec, httptest.NewRequest(http.MethodGet, "/admin/api/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"cost":"free"`) || !strings.Contains(body, `"lastSync":{"at":1}`) {
		t.Fatalf("body = %s", body)
	}
}

func TestModelsAddValidationAndDefaults(t *testing.T) {
	store := &fakeModelStore{models: []domain.Model{{ID: "existing"}}}
	m := newModels(store)

	rec := httptest.NewRecorder()
	m.Add(rec, postJSON("/admin/api/models/add", `{"id":"existing"}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	m.Add(rec, postJSON("/admin/api/models/add", `{"id":""}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty id status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	m.Add(rec, postJSON("/admin/api/models/add", `{"id":"vendor/model"}`))
	if rec.Code != http.StatusOK || len(store.added) != 1 {
		t.Fatalf("status = %d added = %+v", rec.Code, store.added)
	}
	got := store.added[0]
	if got.Provider != "vendor" || got.Cost != "pass" || got.Status != "active" || !got.Custom {
		t.Fatalf("added = %+v", got)
	}

	rec = httptest.NewRecorder()
	m.Add(rec, postJSON("/admin/api/models/add", `{"id":"plain","cost":"free","provider":"p"}`))
	got = store.added[1]
	if got.Provider != "p" || got.Cost != "free" {
		t.Fatalf("added = %+v", got)
	}

	rec = httptest.NewRecorder()
	m.Add(rec, postJSON("/admin/api/models/add", `{"id":"other"}`))
	if store.added[2].Provider != "custom" {
		t.Fatalf("provider = %q", store.added[2].Provider)
	}
}

func TestModelsDeleteResults(t *testing.T) {
	cases := []struct {
		result DeleteModelResult
		status int
		msgKey string
	}{
		{ModelDeleted, http.StatusOK, "model_deleted"},
		{ModelBuiltin, http.StatusBadRequest, "cannot_delete_builtin"},
		{ModelNotFound, http.StatusNotFound, "model_not_found"},
	}
	for _, tc := range cases {
		store := &fakeModelStore{deleteRes: tc.result}
		m := newModels(store)
		rec := httptest.NewRecorder()
		m.Delete(rec, postJSON("/admin/api/models/delete", `{"id":"m"}`))
		if rec.Code != tc.status {
			t.Fatalf("%v status = %d", tc.result, rec.Code)
		}
		if len(store.deleted) != 1 || store.deleted[0] != "m" {
			t.Fatalf("deleted = %v", store.deleted)
		}
		if tc.status != http.StatusOK && !strings.Contains(rec.Body.String(), tc.msgKey) {
			t.Fatalf("%v body = %s", tc.result, rec.Body.String())
		}
	}

	m := newModels(&fakeModelStore{})
	rec := httptest.NewRecorder()
	m.Delete(rec, postJSON("/admin/api/models/delete", `{"id":""}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty id status = %d", rec.Code)
	}
}

func TestModelsUpdateContext(t *testing.T) {
	store := &fakeModelStore{ctxFound: true}
	m := newModels(store)

	rec := httptest.NewRecorder()
	m.UpdateContext(rec, postJSON("/admin/api/models/context", `{"id":"m","context":-1}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	m.UpdateContext(rec, postJSON("/admin/api/models/context", `{"id":"m","context":100,"output":10}`))
	if rec.Code != http.StatusOK || len(store.ctxCalls) != 1 {
		t.Fatalf("status = %d calls = %v", rec.Code, store.ctxCalls)
	}

	store.ctxFound = false
	rec = httptest.NewRecorder()
	m.UpdateContext(rec, postJSON("/admin/api/models/context", `{"id":"missing","context":1}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", rec.Code)
	}
}

func TestModelsToggle(t *testing.T) {
	store := &fakeModelStore{changed: 2}
	m := newModels(store)

	rec := httptest.NewRecorder()
	m.Toggle(rec, postJSON("/admin/api/models/toggle", `{"ids":["a"," b ",""]}`))
	if rec.Code != http.StatusOK || len(store.disabled) != 2 {
		t.Fatalf("status = %d disabled = %v", rec.Code, store.disabled)
	}

	store.changed = 0
	rec = httptest.NewRecorder()
	m.Toggle(rec, postJSON("/admin/api/models/toggle", `{"ids":["nope"]}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unchanged status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	m.Toggle(rec, postJSON("/admin/api/models/toggle", `{"ids":[]}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty ids status = %d", rec.Code)
	}
}
