package main

// providerPresetsImported（ExtremeRouter registry 一键导入，§7.3）的结构性测试：
// 每个生成条目必须可解析、命名完整、URL 合法；FreeTier 与付费-only 条目不共享
// 上游站点（free/paid 不重叠）；合并语义：手写 preset 覆盖同名导入。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProviderPresetsImportedStructure(t *testing.T) {
	if len(providerPresetsImported) == 0 {
		t.Fatal("providerPresetsImported empty: registry import was lost")
	}
	seenName := map[string]string{} // display Name 在前端展示层应唯一
	for key, p := range providerPresetsImported {
		if key == "" || p.Name == "" || p.BaseURL == "" {
			t.Errorf("preset %q: key/name/baseURL required", key)
			continue
		}
		if !strings.Contains(p.Notes, "registry") {
			t.Errorf("preset %q: Notes %q should trace provenance (--extremerouter registry)", key, p.Notes)
		}
		if u, err := url.Parse(p.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			t.Errorf("preset %q: invalid baseURL %q (%v)", key, p.BaseURL, err)
		}
		for _, id := range p.ModelIDs {
			if id == "" {
				t.Errorf("preset %q: empty ModelID", key)
			}
		}
		if p.ModelsFetcher != nil && p.ModelsFetcher.URL == "" {
			t.Errorf("preset %q: ModelsFetcher.URL empty", key)
		}
		if other, ok := seenName[p.Name]; ok {
			t.Errorf("preset %q: display Name %q duplicates %q", key, p.Name, other)
		}
		seenName[p.Name] = key
	}
}

func TestProviderPresetsFreePaidNoOriginOverlap(t *testing.T) {
	freeSite := map[string]string{}
	host := func(base string) string {
		u, err := url.Parse(base)
		if err != nil {
			return base
		}
		return strings.TrimPrefix(u.Hostname(), "www.")
	}
	for key, p := range providerPresetsImported {
		if p.FreeTier {
			freeSite[host(p.BaseURL)] = key
		}
	}
	for key, p := range providerPresetsImported {
		if p.FreeTier {
			continue
		}
		if k, ok := freeSite[host(p.BaseURL)]; ok {
			t.Errorf("paid-only preset %q shares origin with free preset %q — tier ambiguo, remove one", key, k)
		}
	}
}

func TestMergePresetsRuntimeWins(t *testing.T) {
	dst := map[string]providerPreset{"a": {Name: "runtime-a"}, "b": {Name: "runtime-b"}}
	src := map[string]providerPreset{"b": {Name: "imported-b"}, "c": {Name: "imported-c"}}
	got := mergePresets(dst, src)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got["b"].Name != "runtime-b" {
		t.Errorf("runtime preset must win on collision, got %q", got["b"].Name)
	}
	if got["a"].Name != "runtime-a" || got["c"].Name != "imported-c" {
		t.Errorf("merge lost entries: %+v", got)
	}
}

func TestHandleProviderPresetsIncludesImported(t *testing.T) {
	resp := httptest.NewRecorder()
	handleProviderPresets(resp, httptest.NewRequest(http.MethodGet, "/admin/api/providers/presets", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	var body struct {
		Data struct {
			Presets []struct {
				Key      string `json:"key"`
				FreeTier bool   `json:"freeTier"`
			} `json:"presets"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	found := false
	for _, p := range body.Data.Presets {
		if p.Key == "g4f-groq" {
			found = true
			if !p.FreeTier {
				t.Errorf("g4f-groq should be FreeTier")
			}
		}
	}
	if !found {
		t.Error("imported preset g4f-groq missing from /admin/api/providers/presets")
	}
}
