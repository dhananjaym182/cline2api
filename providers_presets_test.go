package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// §7.3.4：导入的 preset 必须结构合法、分组正确、且绝不覆盖手写的 providerPresets。
func TestImportedPresetsShape(t *testing.T) {
	for k, p := range providerPresetsImported {
		if k == "" || p.Name == "" {
			t.Errorf("preset %q: bad name/key", k)
		}
		if !strings.HasPrefix(p.BaseURL, "http://") && !strings.HasPrefix(p.BaseURL, "https://") {
			t.Errorf("preset %q: baseURL %q not http(s)", k, p.BaseURL)
		}
		switch p.Kind {
		case "free", "freeTier", "hasFree", "local", "other":
		default:
			t.Errorf("preset %q: unknown kind %q", k, p.Kind)
		}
		if p.Kind == "free" && !p.FreeTier {
			t.Errorf("preset %q: kind free must set FreeTier", k)
		}
		if p.ModelsFetcher != nil && !strings.HasPrefix(p.ModelsFetcher.URL, "https://") {
			t.Errorf("preset %q: modelsFetcher URL %q not https", k, p.ModelsFetcher.URL)
		}
	}
	for k := range providerPresets {
		if _, dup := providerPresetsImported[k]; dup {
			t.Errorf("imported preset %q collides with hand-written preset", k)
		}
	}
}

// 手写 preset 优先于导入项；输出带 kind（空白时由 FreeTier 推导）。
func TestProviderPresetsEndpointMergePrecedence(t *testing.T) {
	providerPresetsImported["openrouter"] = providerPreset{Name: "IMPORTED-SHOULD-LOSE", BaseURL: "https://evil.example/v1"}
	defer delete(providerPresetsImported, "openrouter")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/providers/presets", nil)
	handleProviderPresets(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var resp struct {
		Data struct {
			Presets []struct {
				Key  string `json:"key"`
				Name string `json:"name"`
				Kind string `json:"kind"`
			} `json:"presets"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byKey := map[string]string{}
	kinds := map[string]bool{}
	for _, p := range resp.Data.Presets {
		byKey[p.Key] = p.Name
		kinds[p.Kind] = true
	}
	if byKey["openrouter"] == "IMPORTED-SHOULD-LOSE" || byKey["openrouter"] == "" {
		t.Errorf("hand-written preset was overridden or missing: %q", byKey["openrouter"])
	}
	for _, want := range []string{"free", "freeTier", "hasFree", "local"} {
		if !kinds[want] {
			t.Errorf("expected kind %q present in merged presets", want)
		}
	}
}
