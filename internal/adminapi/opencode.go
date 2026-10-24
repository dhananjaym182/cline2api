package adminapi

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"cline-go-proxy/internal/config"
	"cline-go-proxy/internal/httpx"
)

// Opencode serves the OpenCode Zen configuration endpoints. The live config is
// injected: the composition layer owns its mutex, persistence, and transport
// rebuild side effects.
type Opencode struct {
	Get            func() *config.Zen
	Set            func(*config.Zen)
	ValidateProxy  func([]string) error
	MaskProxy      func(string) string
	ProxyCooldowns func() map[string]string
	FailedNow      func() bool
	SyncedModels   func() int
	LastSync       func() any
	Translate      func(*http.Request, string) string
	Logf           func(string, ...any)
}

func (h *Opencode) msg(r *http.Request, key string) string {
	if h.Translate != nil {
		return h.Translate(r, key)
	}
	return key
}

func (h *Opencode) logf(format string, args ...any) {
	if h.Logf != nil {
		h.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Config handles GET /admin/api/opencode/config.
func (h *Opencode) Config(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteAPI(w, http.StatusMethodNotAllowed, httpx.Response{Error: h.msg(r, "method_not_allowed")})
		return
	}
	cfg := h.Get()
	proxies := make([]string, 0, len(cfg.Proxies))
	for _, p := range cfg.Proxies {
		proxies = append(proxies, h.MaskProxy(p))
	}
	var cooldowns map[string]string
	if h.ProxyCooldowns != nil {
		cooldowns = h.ProxyCooldowns()
	}
	var synced int
	if h.SyncedModels != nil {
		synced = h.SyncedModels()
	}
	var lastSync any
	if h.LastSync != nil {
		lastSync = h.LastSync()
	}
	failed := h.FailedNow != nil && h.FailedNow()
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Data: map[string]any{
		"enabled":         cfg.Enabled,
		"key":             cfg.Key,
		"baseURL":         cfg.BaseURL,
		"proxies":         proxies,
		"proxyStrategy":   cfg.ProxyStrategy,
		"proxyCooldowns":  cooldowns,
		"maxConcurrency":  cfg.MaxConcurrency,
		"retries":         cfg.Retries,
		"failover":        cfg.Failover,
		"failoverCount":   cfg.FailoverCount,
		"failoverMinutes": cfg.FailoverMinutes,
		"zenHeaders":      cfg.ZenHeaders,
		"compaction":      cfg.Compaction,
		"runtime": map[string]any{
			"failoverActive": failed,
		},
		"syncedModels": synced,
		"lastSync":     lastSync,
	}})
}

// Update handles POST /admin/api/opencode/config/update with pointer-style
// patch fields. Validation happens before persisting; earlier accepted fields
// stay applied in memory exactly like a field-by-field patch.
func (h *Opencode) Update(w http.ResponseWriter, r *http.Request) {
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
		Enabled         *bool              `json:"enabled"`
		Key             *string            `json:"key"`
		BaseURL         *string            `json:"baseURL"`
		Proxies         []string           `json:"proxies"`
		ProxyStrategy   *string            `json:"proxyStrategy"`
		MaxConcurrency  *int               `json:"maxConcurrency"`
		Retries         *int               `json:"retries"`
		Failover        *bool              `json:"failover"`
		FailoverCount   *int               `json:"failoverCount"`
		FailoverMinutes *int               `json:"failoverMinutes"`
		ZenHeaders      *map[string]string `json:"zenHeaders"`
		Compaction      *config.ZenCompact `json:"compaction"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_json")})
		return
	}

	cfg := h.Get()
	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}
	if req.Key != nil {
		cfg.Key = strings.TrimSpace(*req.Key)
	}
	if req.BaseURL != nil {
		u := strings.TrimSpace(*req.BaseURL)
		if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_base_url")})
			return
		}
		cfg.BaseURL = u
	}
	if req.Proxies != nil {
		if err := h.ValidateProxy(req.Proxies); err != nil {
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: err.Error()})
			return
		}
		var cleaned []string
		for _, p := range req.Proxies {
			if line := strings.TrimSpace(p); line != "" {
				cleaned = append(cleaned, line)
			}
		}
		cfg.Proxies = cleaned
	}
	if req.ProxyStrategy != nil {
		switch *req.ProxyStrategy {
		case "round_robin", "random", "fill":
			cfg.ProxyStrategy = *req.ProxyStrategy
		default:
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_proxy_strategy")})
			return
		}
	}
	if req.MaxConcurrency != nil {
		if *req.MaxConcurrency < 1 || *req.MaxConcurrency > 64 {
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_concurrency")})
			return
		}
		cfg.MaxConcurrency = *req.MaxConcurrency
	}
	if req.Retries != nil {
		if *req.Retries < 0 || *req.Retries > 10 {
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_retries")})
			return
		}
		cfg.Retries = *req.Retries
	}
	if req.Failover != nil {
		cfg.Failover = *req.Failover
	}
	if req.FailoverCount != nil {
		if *req.FailoverCount < 1 || *req.FailoverCount > 20 {
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_failover")})
			return
		}
		cfg.FailoverCount = *req.FailoverCount
	}
	if req.FailoverMinutes != nil {
		if *req.FailoverMinutes < 1 || *req.FailoverMinutes > 120 {
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_failover")})
			return
		}
		cfg.FailoverMinutes = *req.FailoverMinutes
	}
	if req.ZenHeaders != nil {
		cleaned := map[string]string{}
		for k, v := range *req.ZenHeaders {
			k = strings.TrimSpace(k)
			if k == "" || strings.TrimSpace(v) == "" {
				continue
			}
			cleaned[k] = strings.TrimSpace(v)
		}
		cfg.ZenHeaders = cleaned
	}
	if req.Compaction != nil {
		c := req.Compaction
		if c.Buffer < 0 || c.KeepTokens < 0 || c.MaxSummary < 0 {
			httpx.WriteAPI(w, http.StatusBadRequest, httpx.Response{Error: h.msg(r, "invalid_compaction")})
			return
		}
		cfg.Compaction.Auto = c.Auto
		cfg.Compaction.Buffer = c.Buffer
		cfg.Compaction.KeepTokens = c.KeepTokens
		cfg.Compaction.SummaryModel = strings.TrimSpace(c.SummaryModel)
		cfg.Compaction.MaxSummary = c.MaxSummary
	}

	h.Set(cfg)
	h.logf("admin: opencode config updated (enabled=%v)", cfg.Enabled)
	httpx.WriteAPI(w, http.StatusOK, httpx.Response{Success: true, Message: h.msg(r, "opencode_config_saved")})
}
