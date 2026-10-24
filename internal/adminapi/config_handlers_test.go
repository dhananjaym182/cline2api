package adminapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cline-go-proxy/internal/config"
)

func TestOpencodeConfigMasksProxies(t *testing.T) {
	cfg := &config.Zen{Enabled: true, Key: "public", Proxies: []string{"http://u:p@host:1"}, ProxyStrategy: "round_robin"}
	h := &Opencode{
		Get:            func() *config.Zen { return cfg },
		MaskProxy:      func(string) string { return "masked" },
		ProxyCooldowns: func() map[string]string { return map[string]string{"0": "now"} },
		FailedNow:      func() bool { return true },
		SyncedModels:   func() int { return 7 },
		LastSync:       func() any { return map[string]any{"ok": true} },
	}
	rec := httptest.NewRecorder()
	h.Config(rec, httptest.NewRequest(http.MethodGet, "/admin/api/opencode/config", nil))
	body := rec.Body.String()
	for _, want := range []string{`"masked"`, `"failoverActive":true`, `"syncedModels":7`, `"lastSync":{"ok":true}`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "u:p@") {
		t.Fatal("proxy credentials leaked")
	}
}

func TestOpencodeUpdateAppliesPatchAndValidates(t *testing.T) {
	cfg := &config.Zen{ProxyStrategy: "round_robin", MaxConcurrency: 8}
	var saved *config.Zen
	h := &Opencode{
		Get:           func() *config.Zen { return cfg },
		Set:           func(c *config.Zen) { saved = c },
		ValidateProxy: func(list []string) error { return nil },
		Logf:          func(string, ...any) {},
	}

	rec := httptest.NewRecorder()
	h.Update(rec, postJSON("/admin/api/opencode/config/update", `{"enabled":false,"proxyStrategy":"fill","proxies":[" http://x:1 ",""]}`))
	if rec.Code != http.StatusOK || saved == nil {
		t.Fatalf("status = %d saved = %v body=%s", rec.Code, saved, rec.Body.String())
	}
	if cfg.Enabled || cfg.ProxyStrategy != "fill" || len(cfg.Proxies) != 1 || cfg.Proxies[0] != "http://x:1" {
		t.Fatalf("cfg = %+v", cfg)
	}

	for _, tc := range []struct {
		body string
		key  string
	}{
		{`{"baseURL":"ftp://x"}`, "invalid_base_url"},
		{`{"proxyStrategy":"bogus"}`, "invalid_proxy_strategy"},
		{`{"maxConcurrency":0}`, "invalid_concurrency"},
		{`{"retries":11}`, "invalid_retries"},
		{`{"failoverCount":0}`, "invalid_failover"},
		{`{"compaction":{"buffer":-1}}`, "invalid_compaction"},
	} {
		h.Translate = func(*http.Request, string) string { return "translated" }
		rec := httptest.NewRecorder()
		h.Update(rec, postJSON("/admin/api/opencode/config/update", tc.body))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "translated") {
			t.Fatalf("%s status = %d body=%s", tc.key, rec.Code, rec.Body.String())
		}
	}
}

func TestClineEgressConfigAndUpdate(t *testing.T) {
	cfg := &config.ClineEgress{Proxies: []string{"http://a:1", "http://b:2"}, ProxyStrategy: "round_robin"}
	var saved *config.ClineEgress
	persistErr := error(nil)
	h := &ClineEgress{
		Get:           func() *config.ClineEgress { return cfg },
		Set:           func(c *config.ClineEgress) { saved = c },
		PersistErr:    func() error { return persistErr },
		ValidateProxy: func([]string) error { return nil },
		MaskProxy:     func(s string) string { return "m:" + s },
		Logf:          func(string, ...any) {},
	}

	rec := httptest.NewRecorder()
	h.Config(rec, httptest.NewRequest(http.MethodGet, "/admin/api/cline-proxy/config", nil))
	if !strings.Contains(rec.Body.String(), "m:http://a:1") {
		t.Fatalf("body = %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.Update(rec, postJSON("/admin/api/cline-proxy/config/update", `{"proxyStrategy":"random"}`))
	if rec.Code != http.StatusOK || saved == nil || saved.ProxyStrategy != "random" || len(saved.Proxies) != 2 {
		t.Fatalf("status=%d saved=%+v", rec.Code, saved)
	}

	h.ValidateProxy = func([]string) error { return errors.New("bad proxy") }
	rec = httptest.NewRecorder()
	h.Update(rec, postJSON("/admin/api/cline-proxy/config/update", `{"proxies":["bad"]}`))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "bad proxy") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	h.ValidateProxy = func([]string) error { return nil }
	persistErr = errors.New("disk full")
	rec = httptest.NewRecorder()
	h.Update(rec, postJSON("/admin/api/cline-proxy/config/update", `{}`))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "disk full") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
