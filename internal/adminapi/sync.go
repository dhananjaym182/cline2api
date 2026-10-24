package adminapi

import (
	"encoding/json"
	"net/http"

	"cline-go-proxy/internal/httpx"
)

// Sync serves model-catalog synchronization and provider health rechecks.
// Callbacks own upstream work and persistence; handlers own the wire contract.
type Sync struct {
	// ModelSync triggers a Cline model sync and returns the result payload and
	// an error message (empty on success).
	ModelSync func() (data any, errMsg string)
	// ZenModelSync triggers an OpenCode Zen sync and records the result.
	ZenModelSync func() (data any, errMsg string)
	// RecheckAll rechecks every provider; nil data means a run is already active.
	RecheckAll func() any
	// RecheckOne rechecks one provider and returns its state.
	RecheckOne func(id string) (any, error)
	// SyncStatus returns the last known recheck state keyed by provider.
	SyncStatus func() map[string]any

	Translate func(*http.Request, string) string
}

func (h *Sync) msg(r *http.Request, key string) string {
	if h.Translate != nil {
		return h.Translate(r, key)
	}
	return key
}

// Models handles POST /admin/api/models/sync.
func (h *Sync) Models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: h.msg(r, "method_not_allowed")})
		return
	}
	data, errMsg := h.ModelSync()
	if errMsg != "" {
		httpx.WriteAPI(w, http.StatusBadGateway, httpx.Response{Success: false, Error: errMsg, Message: h.msg(r, "model_sync_failed")})
		return
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: data, Message: h.msg(r, "model_sync_done")})
}

// ZenModels handles POST /admin/api/opencode/models/sync.
func (h *Sync) ZenModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: h.msg(r, "method_not_allowed")})
		return
	}
	data, errMsg := h.ZenModelSync()
	if errMsg != "" {
		httpx.WriteAPI(w, http.StatusBadGateway, httpx.Response{Success: false, Error: errMsg, Message: h.msg(r, "model_sync_failed")})
		return
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: data, Message: h.msg(r, "model_sync_done")})
}

// ProviderRecheck handles POST /admin/api/providers/recheck with {id} or {all}.
func (h *Sync) ProviderRecheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: "method_not_allowed"})
		return
	}
	var req struct {
		ID  string `json:"id"`
		All bool   `json:"all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: "invalid_json"})
		return
	}
	if req.All {
		res := h.RecheckAll()
		if res == nil {
			httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: map[string]any{"busy": true}})
			return
		}
		httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: map[string]any{"results": res}})
		return
	}
	if req.ID == "" {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: "id or all required"})
		return
	}
	st, err := h.RecheckOne(req.ID)
	if err != nil {
		httpx.WriteAPI(w, http.StatusNotFound, httpx.Response{Error: err.Error()})
		return
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: st})
}

// ProviderStatus handles GET /admin/api/providers/sync-status.
func (h *Sync) ProviderStatus(w http.ResponseWriter, r *http.Request) {
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: map[string]any{
		"providers": h.SyncStatus(),
	}})
}
