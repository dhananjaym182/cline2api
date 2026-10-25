package adminapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	configpkg "cline-go-proxy/internal/config"
)

func TestConfigGetExposesTuningPolicies(t *testing.T) {
	h, f := newConfigHandler()
	f.cfg.Retry = configpkg.RetryPolicy{MaxRounds: 3}
	f.cfg.Stream = configpkg.StreamPolicy{BootstrapRetries: 2}
	f.cfg.SessionAffinity = configpkg.SessionAffinityPolicy{Enabled: true, TTLSeconds: 120}
	f.cfg.ModelAliases = []configpkg.ModelAlias{{Alias: "fast", Target: "m1"}}
	f.cfg.ExcludedModels = []string{"gpt-4-*"}
	f.cfg.RequestErrorRules = []configpkg.RequestErrorRule{{StatusCodes: []int{429}, Action: configpkg.ErrorActionStop}}
	f.cfg.MaxRequestBodyBytes = 8 << 20
	f.cfg.PassthroughHeaders = true
	f.cfg.NonstreamKeepaliveSeconds = 30

	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/admin/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"retry":{"maxRounds":3}`,
		`"stream":{"bootstrapRetries":2}`,
		`"sessionAffinity":{"enabled":true,"ttlSeconds":120}`,
		`"modelAliases":[{"alias":"fast","target":"m1"}]`,
		`"excludedModels":["gpt-4-*"]`,
		`"requestErrorRules":[{"statusCodes":[429],"action":"stop"}]`,
		`"maxRequestBodyBytes":8388608`,
		`"passthroughHeaders":true`,
		`"nonstreamKeepaliveSeconds":30`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET body missing %s: %s", want, body)
		}
	}
}

func TestConfigGetExposesBuildInfo(t *testing.T) {
	h, _ := newConfigHandler()
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/admin/api/config", nil))
	if strings.Contains(rec.Body.String(), `"build"`) {
		t.Fatal("build should be omitted when callback absent")
	}

	h.Build = func() map[string]any { return map[string]any{"commit": "abc1234", "goVersion": "go1.25.0"} }
	rec = httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/admin/api/config", nil))
	data := decodeEnvelope(t, rec).Data.(map[string]any)
	build, ok := data["build"].(map[string]any)
	if !ok || build["commit"] != "abc1234" {
		t.Fatalf("build = %v", data["build"])
	}
}

func TestConfigUpdateAppliesTuningPatches(t *testing.T) {
	h, f := newConfigHandler()
	body := `{
		"strategy":"weighted_round_robin",
		"retry":{"maxRounds":4,"maxCredentialsPerRound":2,"maxIntervalSeconds":30,"transientCooldownSeconds":-1,"maxFailoverModels":5},
		"stream":{"keepaliveSeconds":20,"bootstrapRetries":3,"suppressKeepaliveBeforeFirstToken":true,"firstContentTimeoutSeconds":45},
		"sessionAffinity":{"enabled":true,"ttlSeconds":300},
		"modelAliases":[{"alias":" fast ","target":" m1 ","provider":"OpenRouter"}],
		"excludedModels":[" internal-*",""],
		"requestErrorRules":[{"statusCodes":[429,999],"match":"rate limit","action":"STOP_AND_COOLDOWN"}],
		"maxRequestBodyBytes":4096,
		"passthroughHeaders":true,
		"nonstreamKeepaliveSeconds":15
	}`
	rec := httptest.NewRecorder()
	h.Update(rec, postJSON("/admin/api/config", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	cfg := f.cfg
	if cfg.Strategy != "weighted_round_robin" {
		t.Fatalf("strategy = %q", cfg.Strategy)
	}
	if cfg.Retry.MaxRounds != 4 || cfg.Retry.MaxCredentialsPerRound != 2 || cfg.Retry.MaxIntervalSeconds != 30 ||
		cfg.Retry.TransientCooldownSeconds != -1 || cfg.Retry.MaxFailoverModels != 5 {
		t.Fatalf("retry = %+v", cfg.Retry)
	}
	if cfg.Stream.KeepaliveSeconds != 20 || cfg.Stream.BootstrapRetries != 3 ||
		!cfg.Stream.SuppressKeepaliveBeforeFirstToken || cfg.Stream.FirstContentTimeoutSeconds != 45 {
		t.Fatalf("stream = %+v", cfg.Stream)
	}
	if cfg.Stream.BootstrapBufferBytes != 1<<20 || cfg.Stream.BootstrapBufferFrames != 64 {
		t.Fatalf("stream buffer defaults not applied: %+v", cfg.Stream)
	}
	if !cfg.SessionAffinity.Enabled || cfg.SessionAffinity.TTLSeconds != 300 {
		t.Fatalf("sessionAffinity = %+v", cfg.SessionAffinity)
	}
	if len(cfg.ModelAliases) != 1 || cfg.ModelAliases[0].Alias != "fast" || cfg.ModelAliases[0].Target != "m1" {
		t.Fatalf("modelAliases = %+v", cfg.ModelAliases)
	}
	if len(cfg.ExcludedModels) != 1 || cfg.ExcludedModels[0] != "internal-*" {
		t.Fatalf("excludedModels = %+v", cfg.ExcludedModels)
	}
	if len(cfg.RequestErrorRules) != 1 {
		t.Fatalf("requestErrorRules = %+v", cfg.RequestErrorRules)
	}
	rule := cfg.RequestErrorRules[0]
	if rule.Action != configpkg.ErrorActionStopAndCooldown || len(rule.StatusCodes) != 1 || rule.StatusCodes[0] != 429 {
		t.Fatalf("rule = %+v", rule)
	}
	if cfg.MaxRequestBodyBytes != 4096 || !cfg.PassthroughHeaders || cfg.NonstreamKeepaliveSeconds != 15 {
		t.Fatalf("wire knobs = %+v", cfg)
	}

	// Response echoes the applied values for the UI.
	data := decodeEnvelope(t, rec).Data.(map[string]any)
	for _, key := range []string{"retry", "stream", "sessionAffinity", "modelAliases", "excludedModels", "requestErrorRules", "maxRequestBodyBytes", "passthroughHeaders", "nonstreamKeepaliveSeconds"} {
		if _, ok := data[key]; !ok {
			t.Fatalf("response data missing %q", key)
		}
	}
	if data["strategy"] != "weighted_round_robin" {
		t.Fatalf("strategy echo = %v", data["strategy"])
	}
}

func TestConfigUpdateClampsPolicyValues(t *testing.T) {
	h, f := newConfigHandler()
	body := `{
		"retry":{"maxRounds":1000,"maxIntervalSeconds":-5},
		"stream":{"bootstrapRetries":99,"firstContentTimeoutSeconds":9999,"keepaliveSeconds":-2},
		"sessionAffinity":{"enabled":true,"ttlSeconds":5},
		"maxRequestBodyBytes":-9,
		"nonstreamKeepaliveSeconds":500
	}`
	rec := httptest.NewRecorder()
	h.Update(rec, postJSON("/admin/api/config", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	cfg := f.cfg
	if cfg.Retry.MaxRounds != 100 || cfg.Retry.MaxIntervalSeconds != 0 {
		t.Fatalf("retry clamp = %+v", cfg.Retry)
	}
	if cfg.Stream.BootstrapRetries != 5 || cfg.Stream.FirstContentTimeoutSeconds != 600 || cfg.Stream.KeepaliveSeconds != -1 {
		t.Fatalf("stream clamp = %+v", cfg.Stream)
	}
	if cfg.SessionAffinity.TTLSeconds != 60 {
		t.Fatalf("affinity clamp = %+v", cfg.SessionAffinity)
	}
	if cfg.MaxRequestBodyBytes != 0 || cfg.NonstreamKeepaliveSeconds != 300 {
		t.Fatalf("wire clamp: maxBody=%d keepalive=%d", cfg.MaxRequestBodyBytes, cfg.NonstreamKeepaliveSeconds)
	}
}

func TestConfigUpdateRejectsInvalidErrorRule(t *testing.T) {
	h, f := newConfigHandler()
	f.cfg.ModelAliases = []configpkg.ModelAlias{{Alias: "fast", Target: "m1"}}
	f.cfg.RequestErrorRules = []configpkg.RequestErrorRule{{StatusCodes: []int{429}, Action: configpkg.ErrorActionStop}}

	body := `{"strategy":"fill","requestErrorRules":[{"statusCodes":[429],"match":"[unclosed","regex":true,"action":"stop"}]}`
	rec := httptest.NewRecorder()
	h.Update(rec, postJSON("/admin/api/config", body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	// Validate-before-mutate: even the strategy patch must not have applied.
	if f.cfg.Strategy != "round_robin" {
		t.Fatalf("strategy mutated on rejected patch: %q", f.cfg.Strategy)
	}
	if len(f.cfg.RequestErrorRules) != 1 || f.cfg.RequestErrorRules[0].Action != configpkg.ErrorActionStop {
		t.Fatalf("live rules mutated on rejected patch: %+v", f.cfg.RequestErrorRules)
	}
	if len(f.cfg.ModelAliases) != 1 {
		t.Fatalf("live aliases mutated on rejected patch: %+v", f.cfg.ModelAliases)
	}
}

func TestConfigUpdateRejectsDuplicateAndEmptyAlias(t *testing.T) {
	h, f := newConfigHandler()

	rec := httptest.NewRecorder()
	h.Update(rec, postJSON("/admin/api/config", `{"modelAliases":[{"alias":"fast","target":"m1"},{"alias":"fast","target":"m2"}]}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate alias: status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	if resp := decodeEnvelope(t, rec); !strings.Contains(resp.Error, "duplicate") {
		t.Fatalf("error = %q", resp.Error)
	}
	if f.cfg.ModelAliases != nil {
		t.Fatalf("live aliases mutated: %+v", f.cfg.ModelAliases)
	}

	rec = httptest.NewRecorder()
	h.Update(rec, postJSON("/admin/api/config", `{"modelAliases":[{"alias":"x","target":""}]}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty target: status = %d, want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.Update(rec, postJSON("/admin/api/config", `{"requestErrorRules":[{"action":"explode"}]}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown action: status = %d, want 400", rec.Code)
	}
}

// A patch that replaces one policy list while leaving another untouched must
// still validate the merged set, and normalization must never reorder the
// live slice that was not provided.
func TestConfigUpdateMergesPatchesWithoutLiveMutation(t *testing.T) {
	h, f := newConfigHandler()
	liveAliases := []configpkg.ModelAlias{
		{Alias: "a1", Target: "m1"},
		{Alias: "a2", Target: "m2"},
	}
	f.cfg.ModelAliases = liveAliases
	f.cfg.ExcludedModels = []string{"keep-*"}

	rec := httptest.NewRecorder()
	h.Update(rec, postJSON("/admin/api/config", `{"excludedModels":["other-*"]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if len(f.cfg.ModelAliases) != 2 || f.cfg.ModelAliases[0].Alias != "a1" || f.cfg.ModelAliases[1].Alias != "a2" {
		t.Fatalf("unrelated aliases reordered: %+v", f.cfg.ModelAliases)
	}
	if liveAliases[0].Alias != "a1" {
		t.Fatalf("shared backing array overwritten: %+v", liveAliases)
	}
	if len(f.cfg.ExcludedModels) != 1 || f.cfg.ExcludedModels[0] != "other-*" {
		t.Fatalf("excluded = %+v", f.cfg.ExcludedModels)
	}
}
