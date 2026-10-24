package config

import (
	"os"
	"path/filepath"
	"testing"
)

const testZenBase = "https://zen.example/v1"

func TestDefaultZen(t *testing.T) {
	z := DefaultZen(testZenBase)
	if !z.Enabled || z.Key != "public" || z.BaseURL != testZenBase {
		t.Errorf("unexpected defaults: %+v", z)
	}
	if z.MaxConcurrency != 8 || z.Retries != 3 || !z.Failover {
		t.Errorf("unexpected tuning defaults: %+v", z)
	}
	if !z.Compaction.Auto || z.Compaction.Buffer != 20000 || z.Compaction.KeepTokens != 8000 || z.Compaction.MaxSummary != 4096 {
		t.Errorf("unexpected compaction defaults: %+v", z.Compaction)
	}
}

func TestZenNormalizeLoaded(t *testing.T) {
	z := Zen{BaseURL: LegacyZenBaseURL, ProxyStrategy: "bogus", MaxConcurrency: 0, Retries: 0}
	z.NormalizeLoaded(testZenBase)
	if z.Key != "public" {
		t.Errorf("key = %q", z.Key)
	}
	if z.BaseURL != testZenBase {
		t.Errorf("legacy base URL not migrated: %q", z.BaseURL)
	}
	if z.ProxyStrategy != "round_robin" {
		t.Errorf("strategy = %q", z.ProxyStrategy)
	}
	if z.MaxConcurrency != 0 || z.Retries != 0 {
		t.Error("load normalization must not apply save-time clamps")
	}
}

func TestZenNormalizeForSave(t *testing.T) {
	z := Zen{BaseURL: "  ", ProxyStrategy: "bogus", MaxConcurrency: -1, Retries: -2}
	z.NormalizeForSave(testZenBase)
	if z.BaseURL != testZenBase || z.Key != "public" {
		t.Errorf("baseURL/key = %q/%q", z.BaseURL, z.Key)
	}
	if z.ProxyStrategy != "round_robin" {
		t.Errorf("strategy = %q", z.ProxyStrategy)
	}
	if z.MaxConcurrency != 8 || z.Retries != 3 {
		t.Errorf("clamps = %d/%d", z.MaxConcurrency, z.Retries)
	}
	z2 := Zen{BaseURL: " https://custom/v1 ", ProxyStrategy: "fill", MaxConcurrency: 4, Retries: 0}
	z2.NormalizeForSave(testZenBase)
	if z2.BaseURL != "https://custom/v1" || z2.ProxyStrategy != "fill" || z2.MaxConcurrency != 4 || z2.Retries != 0 {
		t.Errorf("valid values changed: %+v", z2)
	}
}

func TestLoadZenMissingFile(t *testing.T) {
	z, err := LoadZen(filepath.Join(t.TempDir(), "missing.json"), testZenBase)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if z.BaseURL != testZenBase || z.Key != "public" || !z.Enabled {
		t.Errorf("defaults not applied: %+v", z)
	}
}

func TestLoadZenCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cline-zen.json")
	if err := os.WriteFile(path, []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	z, err := LoadZen(path, testZenBase)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if z.BaseURL != testZenBase {
		t.Errorf("fallback base URL = %q", z.BaseURL)
	}
}

func TestSaveLoadZenRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cline-zen.json")
	z := DefaultZen(testZenBase)
	z.Enabled = false
	z.Proxies = []string{"socks5://127.0.0.1:1080"}
	z.ZenHeaders = map[string]string{"X-Test": "1"}
	z.Compaction.KeepTokens = 1234
	if err := SaveZen(path, z); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadZen(path, testZenBase)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Enabled || len(loaded.Proxies) != 1 || loaded.ZenHeaders["X-Test"] != "1" {
		t.Errorf("fields lost: %+v", loaded)
	}
	if loaded.Compaction.KeepTokens != 1234 {
		t.Errorf("compaction lost: %+v", loaded.Compaction)
	}
}
