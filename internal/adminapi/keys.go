package adminapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"cline-go-proxy/internal/httpx"
)

// KeyManager serves the admin API-key endpoints. Storage lives in the account
// pool; callbacks keep this package independent of pool locking and persistence.
type KeyManager struct {
	ListKeys  func() []string
	AddKey    func(string)
	DeleteKey func(string)
	Now       func() time.Time
	Translate func(*http.Request, string) string
}

func (m *KeyManager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *KeyManager) msg(r *http.Request, key string) string {
	if m.Translate != nil {
		return m.Translate(r, key)
	}
	return key
}

// Get handles GET /admin/api/keys.
func (m *KeyManager) Get(w http.ResponseWriter, r *http.Request) {
	var keys []string
	if m.ListKeys != nil {
		keys = m.ListKeys()
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: map[string]any{"keys": keys}})
}

// Generate handles POST /admin/api/keys/generate.
func (m *KeyManager) Generate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: m.msg(r, "method_not_allowed")})
		return
	}
	now := m.now()
	key := fmt.Sprintf("cline_%x_%x", now.UnixMilli(), now.UnixNano()%1000000)
	if m.AddKey == nil {
		httpx.WriteAPI(w, http.StatusInternalServerError, httpx.Response{Error: "key store unavailable"})
		return
	}
	m.AddKey(key)
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: map[string]any{"key": key}})
}

// Delete handles POST /admin/api/keys/delete with body {"key": "..."}.
func (m *KeyManager) Delete(w http.ResponseWriter, r *http.Request) {
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
		Key string `json:"key"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: m.msg(r, "invalid_json")})
		return
	}
	if m.DeleteKey != nil {
		m.DeleteKey(req.Key)
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Message: m.msg(r, "key_deleted")})
}
