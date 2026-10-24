package adminapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	configpkg "cline-go-proxy/internal/config"
	"cline-go-proxy/internal/domain"
	"cline-go-proxy/internal/httpx"
)

// Config serves proxy runtime configuration administration. All state lives in
// the composition layer; callbacks own locking, persistence, and listener life.
type Config struct {
	Proxy              func() *configpkg.Proxy
	SetProxy           func(*configpkg.Proxy)
	Models             func() []domain.Model
	DefaultModel       func() string
	SetDefaultModel    func(string)
	SetListenHost      func(string)
	LocalIPs           func() []string
	ListenHost         func() string
	ListenPort         func() int
	EffectiveAdminHost func(string) string
	Version            string
	ZenHeaders         func() map[string]string
	PoolPath           string
	HasPassword        func() bool
	// RestartListener rebinds the HTTP listener to a new host on the same port.
	RestartListener func(host string, port int) error
	Translate       func(*http.Request, string) string
	Logf            func(string, ...any)
}

func (h *Config) msg(r *http.Request, key string) string {
	if h.Translate != nil {
		return h.Translate(r, key)
	}
	return key
}

func (h *Config) logf(format string, args ...any) {
	if h.Logf != nil {
		h.Logf(format, args...)
	}
}

func (h *Config) address(host string) string {
	port := 0
	if h.ListenPort != nil {
		port = h.ListenPort()
	}
	if h.EffectiveAdminHost != nil {
		host = h.EffectiveAdminHost(host)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// Get handles GET /admin/api/config.
func (h *Config) Get(w http.ResponseWriter, r *http.Request) {
	cfg := h.Proxy()
	host := ""
	if h.ListenHost != nil {
		host = h.ListenHost()
	}
	data := map[string]any{
		"address":      h.address(host),
		"host":         host,
		"strategy":     cfg.Strategy,
		"modelChain":   cfg.ModelChain,
		"version":      h.Version,
		"poolPath":     h.PoolPath,
		"defaultModel": h.DefaultModel(),
		"headers":      cfg.Headers,
		"onlyFree":     cfg.OnlyFree,
	}
	if h.ZenHeaders != nil {
		data["zenHeaders"] = h.ZenHeaders()
	}
	if h.LocalIPs != nil {
		data["localIPs"] = h.LocalIPs()
	}
	if h.HasPassword != nil {
		data["hasPassword"] = h.HasPassword()
	}
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: data})
}

// Update handles POST /admin/api/config with a partial configuration patch.
func (h *Config) Update(w http.ResponseWriter, r *http.Request) {
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

	var chainUnverified []string

	var req struct {
		Strategy     string            `json:"strategy"`
		Headers      map[string]string `json:"headers"`
		DefaultModel string            `json:"defaultModel"`
		Host         string            `json:"host"`
		ModelChain   *[]string         `json:"modelChain"`
		OnlyFree     *bool             `json:"onlyFree"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_json")})
		return
	}

	cfg := h.Proxy()
	changed := false
	restarting := false

	if req.Strategy != "" {
		switch req.Strategy {
		case "round_robin", "fill", "random", "quality":
			cfg.Strategy = req.Strategy
			changed = true
		default:
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_strategy")})
			return
		}
	}

	if req.Headers != nil {
		for k, v := range req.Headers {
			cfg.Headers[k] = v
		}
		changed = true
	}

	if req.ModelChain != nil {
		known := map[string]bool{"free": true}
		if h.Models != nil {
			for _, m := range h.Models() {
				known[m.ID] = true
			}
		}
		var chain []string
		var unverified []string
		seen := map[string]bool{}
		for _, raw := range *req.ModelChain {
			id := strings.TrimSpace(raw)
			if id == "" || seen[id] {
				continue
			}
			if !known[id] {
				unverified = append(unverified, id)
			}
			seen[id] = true
			chain = append(chain, id)
		}
		cfg.ModelChain = chain
		if len(unverified) > 0 {
			h.logf("  model chain: kept out-of-list model(s) %v (not in synced list, may still work)", unverified)
		}
		changed = true
		chainUnverified = unverified
	}

	if req.OnlyFree != nil {
		cfg.OnlyFree = *req.OnlyFree
		changed = true
	}

	if req.DefaultModel != "" {
		found := false
		if h.Models != nil {
			for _, m := range h.Models() {
				if m.ID == req.DefaultModel {
					found = true
					break
				}
			}
		}
		if !found {
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_default_model")})
			return
		}
		if h.SetDefaultModel != nil {
			h.SetDefaultModel(req.DefaultModel)
		}
	}

	if req.Host != "" {
		valid := req.Host == "127.0.0.1" || req.Host == "0.0.0.0" || req.Host == "localhost" || req.Host == "::1"
		if !valid && h.LocalIPs != nil {
			for _, ip := range h.LocalIPs() {
				if ip == req.Host {
					valid = true
					break
				}
			}
		}
		if !valid {
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_host")})
			return
		}
		if h.SetListenHost != nil {
			h.SetListenHost(req.Host)
		}
		restarting = true
	}

	if changed && h.SetProxy != nil {
		h.SetProxy(cfg)
	}

	// Restart is asynchronous, so the reported host is the one currently bound.
	host := ""
	if h.ListenHost != nil {
		host = h.ListenHost()
	}
	if restarting && h.RestartListener != nil {
		port := 0
		if h.ListenPort != nil {
			port = h.ListenPort()
		}
		go func() {
			if err := h.RestartListener(req.Host, port); err != nil && err != http.ErrServerClosed {
				h.logf("Listener restart failed: %v", err)
			}
		}()
	}

	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: map[string]any{
		"strategy":        cfg.Strategy,
		"headers":         cfg.Headers,
		"onlyFree":        cfg.OnlyFree,
		"defaultModel":    h.DefaultModel(),
		"host":            host,
		"address":         h.address(host),
		"restarting":      restarting,
		"chainUnverified": chainUnverified,
	}})
}
