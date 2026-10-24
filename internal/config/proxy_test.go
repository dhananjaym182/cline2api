package config

import (
	"os"
	"path/filepath"
	"testing"

	"cline-go-proxy/internal/domain"
)

func TestDefaultProxy(t *testing.T) {
	cfg := DefaultProxy()
	if cfg.Strategy != "round_robin" {
		t.Errorf("strategy = %q", cfg.Strategy)
	}
	if cfg.Headers["User-Agent"] != "Cline/3.0.47" {
		t.Errorf("User-Agent = %q", cfg.Headers["User-Agent"])
	}
	if cfg.OnlyFree || len(cfg.Combos) != 0 {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadProxyMissingFileKeepsDefaults(t *testing.T) {
	cfg, err := LoadProxy(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Strategy != "round_robin" || cfg.Headers["X-Title"] != "Cline" {
		t.Errorf("defaults not applied: %+v", cfg)
	}
}

func TestLoadProxyMergesPartialFileOverDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cline-config.json")
	if err := os.WriteFile(path, []byte(`{"strategy":"fill","modelChain":["a/b"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadProxy(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Strategy != "fill" {
		t.Errorf("strategy = %q, want fill", cfg.Strategy)
	}
	if len(cfg.ModelChain) != 1 || cfg.ModelChain[0] != "a/b" {
		t.Errorf("modelChain = %v", cfg.ModelChain)
	}
	if cfg.Headers["User-Agent"] != "Cline/3.0.47" {
		t.Error("headers should keep built-in defaults when the file omits them")
	}
}

func TestLoadProxyInvalidStrategyFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cline-config.json")
	if err := os.WriteFile(path, []byte(`{"strategy":"nonsense"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadProxy(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Strategy != "round_robin" {
		t.Errorf("strategy = %q, want round_robin", cfg.Strategy)
	}
}

func TestLoadProxyCorruptFileReportsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cline-config.json")
	if err := os.WriteFile(path, []byte("{oops"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProxy(path); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestSaveProxyRoundTripIncludingCombos(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cline-config.json")
	cfg := DefaultProxy()
	cfg.Strategy = "quality"
	cfg.OnlyFree = true
	cfg.Combos = []domain.ModelCombo{{
		ID:      "combo-a",
		Name:    "Combo A",
		Enabled: true,
		Entries: []domain.ComboEntry{{Model: "m1"}, {Model: "m2", Source: "provider", Provider: "p"}},
	}}
	if err := SaveProxy(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadProxy(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Strategy != "quality" || !loaded.OnlyFree {
		t.Errorf("settings lost: %+v", loaded)
	}
	if len(loaded.Combos) != 1 || loaded.Combos[0].ID != "combo-a" || len(loaded.Combos[0].Entries) != 2 {
		t.Errorf("combos lost: %+v", loaded.Combos)
	}
	if loaded.Combos[0].Entries[1].Provider != "p" {
		t.Errorf("entry provider lost: %+v", loaded.Combos[0].Entries[1])
	}
}
