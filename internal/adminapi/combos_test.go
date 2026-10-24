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
	"cline-go-proxy/internal/httpx"
)

type fakeComboStore struct {
	combos    []domain.ModelCombo
	catalog   []map[string]any
	upsertErr error
	toggleErr error
	cloneErr  error
	lastSaved domain.ModelCombo
}

func (f *fakeComboStore) List() []domain.ModelCombo { return f.combos }

func (f *fakeComboStore) Catalog() []map[string]any { return f.catalog }

func (f *fakeComboStore) Find(id string) (domain.ModelCombo, bool) {
	for _, c := range f.combos {
		if c.ID == id {
			return c, true
		}
	}
	return domain.ModelCombo{}, false
}

func (f *fakeComboStore) Upsert(c domain.ModelCombo) (domain.ModelCombo, error) {
	if f.upsertErr != nil {
		return domain.ModelCombo{}, f.upsertErr
	}
	f.lastSaved = c
	return c, nil
}

func (f *fakeComboStore) Delete(id string) bool {
	for i, c := range f.combos {
		if c.ID == id {
			f.combos = append(f.combos[:i], f.combos[i+1:]...)
			return true
		}
	}
	return false
}

func (f *fakeComboStore) SetEnabled(id string, enabled bool) (domain.ModelCombo, error) {
	if f.toggleErr != nil {
		return domain.ModelCombo{}, f.toggleErr
	}
	for i := range f.combos {
		if f.combos[i].ID == id {
			f.combos[i].Enabled = enabled
			return f.combos[i], nil
		}
	}
	return domain.ModelCombo{}, errors.New("not found")
}

func (f *fakeComboStore) Clone(id string) (domain.ModelCombo, error) {
	if f.cloneErr != nil {
		return domain.ModelCombo{}, f.cloneErr
	}
	c, ok := f.Find(id)
	if !ok {
		return domain.ModelCombo{}, errors.New("not found")
	}
	c.ID += "-copy"
	c.Name += " Copy"
	return c, nil
}

func comboHandler(store ComboStore) *Combos {
	return &Combos{
		Store:         store,
		LocalizeError: func(_ *http.Request, err error) string { return "localized:" + err.Error() },
		Translate:     func(_ *http.Request, key string) string { return key },
	}
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) httpx.Response {
	t.Helper()
	var resp httpx.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (%s)", err, rec.Body.String())
	}
	return resp
}

func TestCombosListIncludesReservedAlias(t *testing.T) {
	store := &fakeComboStore{
		combos:  []domain.ModelCombo{{ID: "fast", Name: "Fast"}},
		catalog: []map[string]any{{"id": "m1"}},
	}
	rec := httptest.NewRecorder()
	comboHandler(store).List(rec, httptest.NewRequest(http.MethodGet, "/admin/api/combos", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := decodeEnvelope(t, rec)
	if !resp.Success {
		t.Fatal("expected success")
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T", resp.Data)
	}
	aliases, ok := data["reservedAliases"].([]any)
	if !ok || len(aliases) != 1 || aliases[0] != "free" {
		t.Fatalf("reservedAliases = %v", data["reservedAliases"])
	}
	if combos, ok := data["combos"].([]any); !ok || len(combos) != 1 {
		t.Fatalf("combos = %v", data["combos"])
	}
}

func TestCombosSavePreservesCreatedAtOnUpdate(t *testing.T) {
	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	store := &fakeComboStore{combos: []domain.ModelCombo{{ID: "fast", CreatedAt: created}}}
	body := `{"id":"fast","name":"Fast v2","entries":[{"model":"m1"}]}`

	rec := httptest.NewRecorder()
	comboHandler(store).Save(rec, httptest.NewRequest(http.MethodPost, "/admin/api/combos/save", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !store.lastSaved.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt = %v, want %v", store.lastSaved.CreatedAt, created)
	}
	if store.lastSaved.Name != "Fast v2" {
		t.Fatalf("Name = %q", store.lastSaved.Name)
	}
}

func TestCombosSaveKeepsExplicitCreatedAt(t *testing.T) {
	created := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeComboStore{combos: []domain.ModelCombo{{ID: "fast", CreatedAt: time.Now()}}}
	body := `{"id":"fast","name":"Fast","entries":[{"model":"m1"}],"createdAt":"2024-06-01T00:00:00Z"}`

	rec := httptest.NewRecorder()
	comboHandler(store).Save(rec, httptest.NewRequest(http.MethodPost, "/admin/api/combos/save", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !store.lastSaved.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt = %v, want %v", store.lastSaved.CreatedAt, created)
	}
}

func TestCombosSaveLocalizesValidationError(t *testing.T) {
	store := &fakeComboStore{upsertErr: errors.New("chain must not be empty")}
	rec := httptest.NewRecorder()
	comboHandler(store).Save(rec, httptest.NewRequest(http.MethodPost, "/admin/api/combos/save",
		strings.NewReader(`{"id":"fast","name":"Fast","entries":[]}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	resp := decodeEnvelope(t, rec)
	if resp.Error != "localized:chain must not be empty" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func TestCombosSaveRejectsInvalidJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	comboHandler(&fakeComboStore{}).Save(rec, httptest.NewRequest(http.MethodPost, "/admin/api/combos/save",
		strings.NewReader("{")))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "invalid_json" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func TestCombosSaveRejectsWrongMethod(t *testing.T) {
	rec := httptest.NewRecorder()
	comboHandler(&fakeComboStore{}).Save(rec, httptest.NewRequest(http.MethodGet, "/admin/api/combos/save", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestCombosDelete(t *testing.T) {
	store := &fakeComboStore{combos: []domain.ModelCombo{{ID: "fast"}}}
	h := comboHandler(store)

	rec := httptest.NewRecorder()
	h.Delete(rec, httptest.NewRequest(http.MethodPost, "/admin/api/combos/delete", strings.NewReader(`{"id":"fast"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.Delete(rec, httptest.NewRequest(http.MethodPost, "/admin/api/combos/delete", strings.NewReader(`{"id":"fast"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "combo_not_found" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func TestCombosToggle(t *testing.T) {
	store := &fakeComboStore{combos: []domain.ModelCombo{{ID: "fast"}}}
	rec := httptest.NewRecorder()
	comboHandler(store).Toggle(rec, httptest.NewRequest(http.MethodPost, "/admin/api/combos/toggle",
		strings.NewReader(`{"id":"fast","enabled":true}`)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !store.combos[0].Enabled {
		t.Fatal("combo should be enabled")
	}
}

func TestCombosToggleReturnsStoreError(t *testing.T) {
	store := &fakeComboStore{toggleErr: errors.New("no such combo")}
	rec := httptest.NewRecorder()
	comboHandler(store).Toggle(rec, httptest.NewRequest(http.MethodPost, "/admin/api/combos/toggle",
		strings.NewReader(`{"id":"missing","enabled":true}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "no such combo" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func TestCombosClone(t *testing.T) {
	store := &fakeComboStore{combos: []domain.ModelCombo{{ID: "fast", Name: "Fast"}}}
	rec := httptest.NewRecorder()
	comboHandler(store).Clone(rec, httptest.NewRequest(http.MethodPost, "/admin/api/combos/clone",
		strings.NewReader(`{"id":"fast"}`)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := decodeEnvelope(t, rec)
	data := resp.Data.(map[string]any)
	combo := data["combo"].(map[string]any)
	if combo["id"] != "fast-copy" {
		t.Fatalf("cloned id = %v", combo["id"])
	}
}

func TestCombosCloneLocalizesError(t *testing.T) {
	store := &fakeComboStore{cloneErr: errors.New("boom")}
	rec := httptest.NewRecorder()
	comboHandler(store).Clone(rec, httptest.NewRequest(http.MethodPost, "/admin/api/combos/clone",
		strings.NewReader(`{"id":"fast"}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "localized:boom" {
		t.Fatalf("error = %q", resp.Error)
	}
}
