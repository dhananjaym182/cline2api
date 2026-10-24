package adminapi

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"cline-go-proxy/internal/config"
	"cline-go-proxy/internal/httpx"
)

// ClineEgress serves the Cline egress proxy configuration endpoints. The live
// config and persistence error reporting are injected by the composition layer.
type ClineEgress struct {
	Get           func() *config.ClineEgress
	Set           func(*config.ClineEgress)
	PersistErr    func() error
	ValidateProxy func([]string) error
	MaskProxy     func(string) string
	Translate     func(*http.Request, string) string
	Logf          func(string, ...any)
}

func (h *ClineEgress) msg(r *http.Request, key string) string {
	if h.Translate != nil {
		return h.Translate(r, key)
	}
	return key
}

func (h *ClineEgress) logf(format string, args ...any) {
	if h.Logf != nil {
		h.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Config handles GET /admin/api/cline-proxy/config with masked proxies.
func (h *ClineEgress) Config(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: h.msg(r, "method_not_allowed")})
		return
	}
	cfg := h.Get()
	proxies := make([]string, 0, len(cfg.Proxies))
	for _, p := range cfg.Proxies {
		proxies = append(proxies, h.MaskProxy(p))
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: map[string]any{
		"proxies":       proxies,
		"proxyStrategy": cfg.ProxyStrategy,
	}})
}

// Update handles POST /admin/api/cline-proxy/config/update.
func (h *ClineEgress) Update(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Proxies       []string `json:"proxies"`
		ProxyStrategy *string  `json:"proxyStrategy"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_json")})
		return
	}
	if req.Proxies != nil {
		if err := h.ValidateProxy(req.Proxies); err != nil {
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: err.Error()})
			return
		}
	}
	cfg := h.Get()
	updated := &config.ClineEgress{
		Proxies:       cfg.Proxies,
		ProxyStrategy: cfg.ProxyStrategy,
	}
	if req.Proxies != nil {
		updated.Proxies = req.Proxies
	}
	if req.ProxyStrategy != nil {
		switch *req.ProxyStrategy {
		case "round_robin", "random", "fill":
			updated.ProxyStrategy = *req.ProxyStrategy
		default:
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_proxy_strategy")})
			return
		}
	}
	h.Set(updated)
	if h.PersistErr != nil {
		if err := h.PersistErr(); err != nil {
			httpx.WriteAPI(w, http.StatusInternalServerError, httpx.Response{Error: err.Error()})
			return
		}
	}
	h.logf("admin: cline proxy config updated (%d proxies, %s)", len(updated.Proxies), updated.ProxyStrategy)
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Message: h.msg(r, "cline_proxy_saved")})
}
