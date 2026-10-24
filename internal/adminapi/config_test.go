package adminapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	configpkg "cline-go-proxy/internal/config"
	"cline-go-proxy/internal/domain"
)

type configFixture struct {
	cfg          *configpkg.Proxy
	defaultModel string
	host         string
	restarted    chan string
}

func newConfigHandler() (*Config, *configFixture) {
	f := &configFixture{
		cfg:          &configpkg.Proxy{Strategy: "round_robin", Headers: map[string]string{"A": "1"}},
		defaultModel: "m1",
		host:         "127.0.0.1",
		restarted:    make(chan string, 1),
	}
	h := &Config{
		Proxy: func() *configpkg.Proxy { return f.cfg },
		SetProxy: func(c *configpkg.Proxy) {
			f.cfg = c
		},
		Models: func() []domain.Model {
			return []domain.Model{{ID: "m1"}, {ID: "m2"}}
		},
		DefaultModel:    func() string { return f.defaultModel },
		SetDefaultModel: func(m string) { f.defaultModel = m },
		SetListenHost:   func(host string) { f.host = host },
		LocalIPs:        func() []string { return []string{"192.168.1.5"} },
		ListenHost:      func() string { return f.host },
		ListenPort:      func() int { return 8050 },
		EffectiveAdminHost: func(host string) string {
			if host == "" || host == "0.0.0.0" || host == "::" {
				return "localhost"
			}
			return host
		},
		Version: "1.2.3",
		ZenHeaders: func() map[string]string {
			return map[string]string{"Z": "9"}
		},
		PoolPath:    "/tmp/pool.json",
		HasPassword: func() bool { return true },
		RestartListener: func(host string, port int) error {
			f.restarted <- host
			return nil
		},
		Translate: func(_ *http.Request, key string) string { return key },
	}
	return h, f
}

func TestConfigGet(t *testing.T) {
	h, _ := newConfigHandler()
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/admin/api/config", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data := decodeEnvelope(t, rec).Data.(map[string]any)
	if data["address"] != "127.0.0.1:8050" {
		t.Fatalf("address = %v", data["address"])
	}
	if data["version"] != "1.2.3" || data["defaultModel"] != "m1" {
		t.Fatalf("unexpected data: %v", data)
	}
	if data["hasPassword"] != true {
		t.Fatalf("hasPassword = %v", data["hasPassword"])
	}
	ips := data["localIPs"].([]any)
	if len(ips) != 1 || ips[0] != "192.168.1.5" {
		t.Fatalf("localIPs = %v", data["localIPs"])
	}
}

func TestConfigUpdateRejectsInvalidStrategy(t *testing.T) {
	h, _ := newConfigHandler()
	rec := httptest.NewRecorder()
	h.Update(rec, httptest.NewRequest(http.MethodPost, "/admin/api/config", strings.NewReader(`{"strategy":"bogus"}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "invalid_strategy" {
		t.Fatalf("error = %q", resp.Error)
	}
}

func TestConfigUpdateAppliesPatch(t *testing.T) {
	h, f := newConfigHandler()
	body := `{"strategy":"fill","headers":{"B":"2"},"onlyFree":true}`
	rec := httptest.NewRecorder()
	h.Update(rec, httptest.NewRequest(http.MethodPost, "/admin/api/config", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if f.cfg.Strategy != "fill" || !f.cfg.OnlyFree {
		t.Fatalf("cfg = %+v", f.cfg)
	}
	if f.cfg.Headers["A"] != "1" || f.cfg.Headers["B"] != "2" {
		t.Fatalf("headers = %v", f.cfg.Headers)
	}
}

func TestConfigUpdateModelChainDedupesAndReportsUnverified(t *testing.T) {
	h, f := newConfigHandler()
	body := `{"modelChain":[" m1 ","m2","m1","","unknown/model"]}`
	rec := httptest.NewRecorder()
	h.Update(rec, httptest.NewRequest(http.MethodPost, "/admin/api/config", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := []string{"m1", "m2", "unknown/model"}
	if len(f.cfg.ModelChain) != len(want) {
		t.Fatalf("chain = %v", f.cfg.ModelChain)
	}
	for i := range want {
		if f.cfg.ModelChain[i] != want[i] {
			t.Fatalf("chain = %v", f.cfg.ModelChain)
		}
	}
	data := decodeEnvelope(t, rec).Data.(map[string]any)
	unverified := data["chainUnverified"].([]any)
	if len(unverified) != 1 || unverified[0] != "unknown/model" {
		t.Fatalf("chainUnverified = %v", unverified)
	}
}

func TestConfigUpdateValidatesDefaultModel(t *testing.T) {
	h, f := newConfigHandler()

	rec := httptest.NewRecorder()
	h.Update(rec, httptest.NewRequest(http.MethodPost, "/admin/api/config", strings.NewReader(`{"defaultModel":"missing"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "invalid_default_model" {
		t.Fatalf("error = %q", resp.Error)
	}

	rec = httptest.NewRecorder()
	h.Update(rec, httptest.NewRequest(http.MethodPost, "/admin/api/config", strings.NewReader(`{"defaultModel":"m2"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if f.defaultModel != "m2" {
		t.Fatalf("default = %q", f.defaultModel)
	}
}

func TestConfigUpdateHostValidationAndRestart(t *testing.T) {
	h, f := newConfigHandler()

	rec := httptest.NewRecorder()
	h.Update(rec, httptest.NewRequest(http.MethodPost, "/admin/api/config", strings.NewReader(`{"host":"10.9.9.9"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "invalid_host" {
		t.Fatalf("error = %q", resp.Error)
	}

	rec = httptest.NewRecorder()
	h.Update(rec, httptest.NewRequest(http.MethodPost, "/admin/api/config", strings.NewReader(`{"host":"192.168.1.5"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if f.host != "192.168.1.5" {
		t.Fatalf("host = %q", f.host)
	}
	select {
	case got := <-f.restarted:
		if got != "192.168.1.5" {
			t.Fatalf("restarted with %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("listener restart was not triggered")
	}
}

func TestConfigUpdateRejectsInvalidJSON(t *testing.T) {
	h, _ := newConfigHandler()
	rec := httptest.NewRecorder()
	h.Update(rec, httptest.NewRequest(http.MethodPost, "/admin/api/config", strings.NewReader("{")))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeEnvelope(t, rec); resp.Error != "invalid_json" {
		t.Fatalf("error = %q", resp.Error)
	}
}
