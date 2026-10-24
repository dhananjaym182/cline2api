package config

import (
	"encoding/json"
	"os"
	"strings"

	"cline-go-proxy/internal/platform"
)

// ClineEgressPath is the data file holding the Cline egress proxy pool.
const ClineEgressPath = ".cline-proxy.json"

// ClineEgress is the persisted Cline egress proxy pool configuration.
type ClineEgress struct {
	Proxies       []string `json:"proxies"`
	ProxyStrategy string   `json:"proxyStrategy"` // round_robin / random / fill
}

// DefaultClineEgress returns the built-in configuration.
func DefaultClineEgress() ClineEgress {
	return ClineEgress{Proxies: []string{}, ProxyStrategy: "round_robin"}
}

// Normalize trims proxy entries, drops blanks, and restores an invalid
// strategy to round_robin.
func (c *ClineEgress) Normalize() {
	cleaned := make([]string, 0, len(c.Proxies))
	for _, p := range c.Proxies {
		if line := strings.TrimSpace(p); line != "" {
			cleaned = append(cleaned, line)
		}
	}
	c.Proxies = cleaned
	if c.ProxyStrategy != "random" && c.ProxyStrategy != "fill" {
		c.ProxyStrategy = "round_robin"
	}
}

// Snapshot returns a copy of the proxy list and the strategy. Callers use one
// snapshot per request so a concurrent update cannot change the decision midway.
func (c *ClineEgress) Snapshot() (proxies []string, strategy string) {
	return append([]string(nil), c.Proxies...), c.ProxyStrategy
}

// LoadClineEgress loads path, falling back to defaults when the file is
// missing or unreadable. A decode error is returned alongside the defaults so
// the caller can back up the corrupt file; the returned config is normalized.
func LoadClineEgress(path string) (ClineEgress, error) {
	cfg := DefaultClineEgress()
	var decodeErr error
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			decodeErr = err
		}
	}
	cfg.Normalize()
	return cfg, decodeErr
}

// SaveClineEgress persists cfg atomically after normalization.
func SaveClineEgress(path string, cfg *ClineEgress) error {
	cfg.Normalize()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(path, data, 0600)
}
