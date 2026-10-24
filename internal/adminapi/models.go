package adminapi

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"cline-go-proxy/internal/domain"
	"cline-go-proxy/internal/httpx"
)

// DeleteModelResult reports why a model deletion succeeded or failed.
type DeleteModelResult int

const (
	// ModelDeleted means the model was removed and persisted.
	ModelDeleted DeleteModelResult = iota
	// ModelNotFound means no model matched the requested ID.
	ModelNotFound
	// ModelBuiltin means the model is upstream-owned and not delisted, so the
	// administrator must not remove it.
	ModelBuiltin
)

// ModelStore is the account-pool's model catalog as seen by the admin API.
// Implementations own locking and persistence.
type ModelStore interface {
	List() []domain.Model
	Add(domain.Model)
	Delete(id string) DeleteModelResult
	UpdateContext(id string, context, output int) bool
	SetDisabled(ids []string, disabled bool) int
}

// Models serves model catalog administration. Catalog state is injected so this
// package does not depend on pool locking or persistence.
type Models struct {
	Store     ModelStore
	LastSync  func() any
	Normalize func([]domain.Model) []domain.Model
	Translate func(*http.Request, string) string
	Logf      func(string, ...any)
}

func (m *Models) msg(r *http.Request, key string) string {
	if m.Translate != nil {
		return m.Translate(r, key)
	}
	return key
}

func (m *Models) logf(format string, args ...any) {
	if m.Logf != nil {
		m.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func (m *Models) list() []domain.Model {
	if m.Store == nil {
		return nil
	}
	return m.Store.List()
}

// List handles GET /admin/api/models.
func (m *Models) List(w http.ResponseWriter, r *http.Request) {
	models := m.list()
	if m.Normalize != nil {
		models = m.Normalize(models)
	}
	var sync any
	if m.LastSync != nil {
		sync = m.LastSync()
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: map[string]any{
		"models":   models,
		"lastSync": sync,
	}})
}

// Add handles POST /admin/api/models/add with body {id, provider?, cost?}.
func (m *Models) Add(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: m.msg(r, "method_not_allowed")})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: err.Error()})
		return
	}
	defer r.Body.Close()

	var req struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Cost     string `json:"cost"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: m.msg(r, "invalid_json")})
		return
	}
	if req.ID == "" {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: m.msg(r, "model_id_required")})
		return
	}
	for _, existing := range m.list() {
		if existing.ID == req.ID {
			httpx.WriteAPI(w, http.StatusConflict, httpx.Response{Error: m.msg(r, "model_exists")})
			return
		}
	}
	cost := req.Cost
	if cost == "" {
		cost = "pass"
	}
	provider := req.Provider
	if provider == "" {
		if idx := strings.Index(req.ID, "/"); idx > 0 {
			provider = req.ID[:idx]
		} else {
			provider = "custom"
		}
	}
	if m.Store == nil {
		httpx.WriteAPI(w, http.StatusInternalServerError, httpx.Response{Error: "model store unavailable"})
		return
	}
	m.Store.Add(domain.Model{
		ID:       req.ID,
		Provider: provider,
		Cost:     cost,
		Status:   "active",
		Custom:   true,
	})
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Message: m.msg(r, "model_added")})
}

// Delete handles POST /admin/api/models/delete with body {id}.
func (m *Models) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: m.msg(r, "method_not_allowed")})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: err.Error()})
		return
	}
	defer r.Body.Close()

	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: m.msg(r, "invalid_json")})
		return
	}
	if req.ID == "" {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: m.msg(r, "model_id_required")})
		return
	}
	if m.Store == nil {
		httpx.WriteAPI(w, http.StatusInternalServerError, httpx.Response{Error: "model store unavailable"})
		return
	}
	switch m.Store.Delete(req.ID) {
	case ModelDeleted:
		httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Message: m.msg(r, "model_deleted")})
	case ModelBuiltin:
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: m.msg(r, "cannot_delete_builtin")})
	default:
		httpx.WriteAPI(w, http.StatusNotFound, httpx.Response{Error: m.msg(r, "model_not_found")})
	}
}

// UpdateContext handles POST /admin/api/models/context with body
// {id, context, output}. Zero values reset the limits to unknown.
func (m *Models) UpdateContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: m.msg(r, "method_not_allowed")})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: err.Error()})
		return
	}
	defer r.Body.Close()

	var req struct {
		ID      string `json:"id"`
		Context int    `json:"context"`
		Output  int    `json:"output"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: m.msg(r, "invalid_json")})
		return
	}
	if req.ID == "" {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: m.msg(r, "model_id_required")})
		return
	}
	if req.Context < 0 || req.Output < 0 {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: m.msg(r, "invalid_json")})
		return
	}
	if m.Store == nil || !m.Store.UpdateContext(req.ID, req.Context, req.Output) {
		httpx.WriteAPI(w, http.StatusNotFound, httpx.Response{Error: m.msg(r, "model_not_found")})
		return
	}
	m.logf("  model context updated: %s ctx=%d out=%d", req.ID, req.Context, req.Output)
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Message: m.msg(r, "model_context_saved")})
}

// Toggle handles POST /admin/api/models/toggle with body {ids, disabled}.
func (m *Models) Toggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: m.msg(r, "method_not_allowed")})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: err.Error()})
		return
	}
	defer r.Body.Close()

	var req struct {
		IDs      []string `json:"ids"`
		Disabled bool     `json:"disabled"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: m.msg(r, "invalid_json")})
		return
	}
	ids := make([]string, 0, len(req.IDs))
	for _, id := range req.IDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: m.msg(r, "model_id_required")})
		return
	}
	if m.Store == nil {
		httpx.WriteAPI(w, http.StatusInternalServerError, httpx.Response{Error: "model store unavailable"})
		return
	}
	changed := m.Store.SetDisabled(ids, req.Disabled)
	if changed == 0 {
		httpx.WriteAPI(w, http.StatusNotFound, httpx.Response{Error: m.msg(r, "model_not_found")})
		return
	}
	m.logf("  model toggle: %d models -> disabled=%v", changed, req.Disabled)
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Message: m.msg(r, "model_status_saved")})
}
