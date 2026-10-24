package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultClineEgress(t *testing.T) {
	cfg := DefaultClineEgress()
	if cfg.ProxyStrategy != "round_robin" {
		t.Errorf("strategy = %q, want round_robin", cfg.ProxyStrategy)
	}
	if cfg.Proxies == nil || len(cfg.Proxies) != 0 {
		t.Errorf("proxies = %v, want empty non-nil slice", cfg.Proxies)
	}
}

func TestClineEgressNormalize(t *testing.T) {
	cfg := ClineEgress{Proxies: []string{"  http://a:1 ", "", "socks5://b:2"}, ProxyStrategy: "bogus"}
	cfg.Normalize()
	if len(cfg.Proxies) != 2 || cfg.Proxies[0] != "http://a:1" || cfg.Proxies[1] != "socks5://b:2" {
		t.Errorf("proxies = %#v", cfg.Proxies)
	}
	if cfg.ProxyStrategy != "round_robin" {
		t.Errorf("strategy = %q, want round_robin", cfg.ProxyStrategy)
	}
	for _, s := range []string{"random", "fill", "round_robin"} {
		c := ClineEgress{ProxyStrategy: s}
		c.Normalize()
		if c.ProxyStrategy != s {
			t.Errorf("strategy %q changed to %q", s, c.ProxyStrategy)
		}
	}
}

func TestClineEgressSnapshotIsCopy(t *testing.T) {
	cfg := ClineEgress{Proxies: []string{"a", "b"}, ProxyStrategy: "fill"}
	proxies, strategy := cfg.Snapshot()
	if strategy != "fill" || len(proxies) != 2 {
		t.Fatalf("snapshot = %v/%q", proxies, strategy)
	}
	proxies[0] = "mutated"
	if cfg.Proxies[0] != "a" {
		t.Error("snapshot must not alias the proxy list")
	}
}

func TestLoadClineEgressMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	cfg, err := LoadClineEgress(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ProxyStrategy != "round_robin" || len(cfg.Proxies) != 0 {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestLoadClineEgressCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cline-proxy.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadClineEgress(path)
	if err == nil {
		t.Fatal("expected a decode error for corrupt input")
	}
	if cfg.ProxyStrategy != "round_robin" {
		t.Errorf("fallback strategy = %q", cfg.ProxyStrategy)
	}
}

func TestSaveLoadClineEgressRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cline-proxy.json")
	cfg := ClineEgress{Proxies: []string{" http://a:1 ", " "}, ProxyStrategy: "random"}
	if err := SaveClineEgress(path, &cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("perm = %v, want 0600", info.Mode().Perm())
	}
	loaded, err := LoadClineEgress(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Proxies) != 1 || loaded.Proxies[0] != "http://a:1" || loaded.ProxyStrategy != "random" {
		t.Errorf("loaded = %+v", loaded)
	}
}
