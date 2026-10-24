package config

import (
	"encoding/json"
	"os"

	"cline-go-proxy/internal/domain"
	"cline-go-proxy/internal/platform"
)

// ProxyPath is the data file holding global proxy settings and combos.
const ProxyPath = ".cline-config.json"

// Proxy is the persisted global proxy configuration.
type Proxy struct {
	Strategy string            `json:"strategy"`
	Headers  map[string]string `json:"headers"`
	// ModelChain is the administrator-configured fallback order used when a
	// model is cooling down. Empty means the built-in free chain.
	ModelChain []string `json:"modelChain,omitempty"`
	// OnlyFree limits model listings to free models.
	OnlyFree bool `json:"onlyFree"`
	// Combos are named ordered fallback combinations stored in this file.
	Combos []domain.ModelCombo `json:"combos,omitempty"`
}

// DefaultProxy returns the built-in proxy configuration.
func DefaultProxy() *Proxy {
	return &Proxy{
		Strategy: "round_robin",
		Headers: map[string]string{
			"User-Agent":         "Cline/3.0.47",
			"HTTP-Referer":       "https://cline.bot",
			"X-Title":            "Cline",
			"X-IS-MULTIROOT":     "false",
			"X-CLIENT-TYPE":      "cline-cli",
			"X-CLIENT-VERSION":   "3.0.47",
			"X-PLATFORM":         "terminal",
			"X-PLATFORM-VERSION": "3.0.47",
			"X-CORE-VERSION":     "0.0.66",
		},
	}
}

// Normalize restores an unknown strategy to round_robin.
func (c *Proxy) Normalize() {
	switch c.Strategy {
	case "round_robin", "fill", "random", "quality":
	default:
		c.Strategy = "round_robin"
	}
}

// LoadProxy loads path over the built-in defaults so absent fields keep their
// default values. A decode error is returned with the partially decoded config
// so the caller can back up the corrupt file, matching the previous behavior.
func LoadProxy(path string) (*Proxy, error) {
	cfg := DefaultProxy()
	var decodeErr error
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			decodeErr = err
		}
	}
	cfg.Normalize()
	return cfg, decodeErr
}

// SaveProxy persists cfg atomically.
func SaveProxy(path string, cfg *Proxy) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(path, data, 0600)
}
