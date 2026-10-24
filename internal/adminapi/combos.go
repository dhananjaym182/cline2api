package adminapi

import (
	"encoding/json"
	"io"
	"net/http"

	"cline-go-proxy/internal/domain"
	"cline-go-proxy/internal/httpx"
)

// ComboStore is the model-combo registry as seen by the admin API. Locking,
// persistence, and validation stay in the root composition layer.
type ComboStore interface {
	List() []domain.ModelCombo
	Catalog() []map[string]any
	Find(id string) (domain.ModelCombo, bool)
	Upsert(domain.ModelCombo) (domain.ModelCombo, error)
	Delete(id string) bool
	SetEnabled(id string, enabled bool) (domain.ModelCombo, error)
	Clone(id string) (domain.ModelCombo, error)
}

// Combos serves named model-combo administration.
type Combos struct {
	Store ComboStore
	// LocalizeError renders combo validation errors for the request locale.
	LocalizeError func(*http.Request, error) string
	Translate     func(*http.Request, string) string
}

func (h *Combos) msg(r *http.Request, key string) string {
	if h.Translate != nil {
		return h.Translate(r, key)
	}
	return key
}

func (h *Combos) localize(r *http.Request, err error) string {
	if h.LocalizeError != nil {
		return h.LocalizeError(r, err)
	}
	return err.Error()
}

// List handles GET /admin/api/combos.
func (h *Combos) List(w http.ResponseWriter, r *http.Request) {
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: map[string]any{
		"combos":          h.Store.List(),
		"modelCatalog":    h.Store.Catalog(),
		"reservedAliases": []string{"free"},
	}})
}

// Save handles POST /admin/api/combos/save with a ModelCombo body.
func (h *Combos) Save(w http.ResponseWriter, r *http.Request) {
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
	var combo domain.ModelCombo
	if err := json.Unmarshal(body, &combo); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_json")})
		return
	}
	// Preserve creation time when the client sends an update without it.
	if existing, ok := h.Store.Find(combo.ID); ok && combo.CreatedAt.IsZero() {
		combo.CreatedAt = existing.CreatedAt
	}
	saved, err := h.Store.Upsert(combo)
	if err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.localize(r, err)})
		return
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{
		Success: true, Message: h.msg(r, "combo_saved"), Data: map[string]any{"combo": saved},
	})
}

// Delete handles POST /admin/api/combos/delete with body {id}.
func (h *Combos) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: h.msg(r, "method_not_allowed")})
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_json")})
		return
	}
	if !h.Store.Delete(req.ID) {
		httpx.WriteAPI(w, http.StatusNotFound, httpx.Response{Error: h.msg(r, "combo_not_found")})
		return
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Message: h.msg(r, "combo_deleted")})
}

// Toggle handles POST /admin/api/combos/toggle with body {id, enabled}.
func (h *Combos) Toggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: h.msg(r, "method_not_allowed")})
		return
	}
	var req struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_json")})
		return
	}
	combo, err := h.Store.SetEnabled(req.ID, req.Enabled)
	if err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: err.Error()})
		return
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: map[string]any{"combo": combo}})
}

// Clone handles POST /admin/api/combos/clone with body {id}.
func (h *Combos) Clone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: h.msg(r, "method_not_allowed")})
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_json")})
		return
	}
	combo, err := h.Store.Clone(req.ID)
	if err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.localize(r, err)})
		return
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{
		Success: true, Message: h.msg(r, "combo_cloned"), Data: map[string]any{"combo": combo},
	})
}
