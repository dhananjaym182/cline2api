package config

import (
	"encoding/json"
	"os"
	"strings"

	"cline-go-proxy/internal/platform"
)

// ZenPath is the data file holding the OpenCode Zen configuration.
const ZenPath = ".cline-zen.json"

// LegacyZenBaseURL is the retired default endpoint. Persisted configurations
// pointing at it are migrated to the current inference API on load.
const LegacyZenBaseURL = "https://opencode.ai/zen/v1"

// ZenCompact is the automatic context-compaction configuration nested in Zen.
type ZenCompact struct {
	Auto         bool   `json:"auto"`
	Buffer       int    `json:"buffer"`
	KeepTokens   int    `json:"keepTokens"`
	SummaryModel string `json:"summaryModel"`
	MaxSummary   int    `json:"maxSummary"`
}

// Zen is the persisted OpenCode Zen configuration.
type Zen struct {
	Enabled         bool     `json:"enabled"`
	Key             string   `json:"key"`
	BaseURL         string   `json:"baseURL"`
	Proxies         []string `json:"proxies"`
	ProxyStrategy   string   `json:"proxyStrategy"` // round_robin / random / fill
	MaxConcurrency  int      `json:"maxConcurrency"`
	Retries         int      `json:"retries"`
	Failover        bool     `json:"failover"`
	FailoverCount   int      `json:"failoverCount"`
	FailoverMinutes int      `json:"failoverMinutes"`
	// ZenHeaders overrides the built-in fingerprint headers. The special values
	// "$session", "$request", "$project", and "$client" inject per-request identity.
	ZenHeaders map[string]string `json:"zenHeaders,omitempty"`
	Compaction ZenCompact        `json:"compaction"`
}

// DefaultZen returns the built-in configuration for the given inference base URL.
func DefaultZen(baseURL string) *Zen {
	return &Zen{
		Enabled:         true,
		Key:             "public",
		BaseURL:         baseURL,
		ProxyStrategy:   "round_robin",
		MaxConcurrency:  8,
		Retries:         3,
		Failover:        true,
		FailoverCount:   3,
		FailoverMinutes: 5,
		Compaction: ZenCompact{
			Auto:       true,
			Buffer:     20000,
			KeepTokens: 8000,
			MaxSummary: 4096,
		},
	}
}

// NormalizeLoaded applies the defaults used when reading a persisted file:
// a missing key, the legacy endpoint, and an unknown proxy strategy are fixed.
func (z *Zen) NormalizeLoaded(baseURL string) {
	if z.Key == "" {
		z.Key = "public"
	}
	if z.BaseURL == "" || z.BaseURL == LegacyZenBaseURL {
		z.BaseURL = baseURL
	}
	if z.ProxyStrategy != "random" && z.ProxyStrategy != "fill" {
		z.ProxyStrategy = "round_robin"
	}
}

// NormalizeForSave validates administrator input before it is persisted.
func (z *Zen) NormalizeForSave(baseURL string) {
	z.BaseURL = strings.TrimSpace(z.BaseURL)
	if z.BaseURL == "" {
		z.BaseURL = baseURL
	}
	if z.Key == "" {
		z.Key = "public"
	}
	if z.ProxyStrategy != "round_robin" && z.ProxyStrategy != "random" && z.ProxyStrategy != "fill" {
		z.ProxyStrategy = "round_robin"
	}
	if z.MaxConcurrency <= 0 {
		z.MaxConcurrency = 8
	}
	if z.Retries < 0 {
		z.Retries = 3
	}
}

// LoadZen loads path over the built-in defaults. A decode error is returned
// with the partially decoded config so the caller can back up the corrupt file.
func LoadZen(path, baseURL string) (*Zen, error) {
	cfg := DefaultZen(baseURL)
	var decodeErr error
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			decodeErr = err
		}
	}
	cfg.NormalizeLoaded(baseURL)
	return cfg, decodeErr
}

// SaveZen persists cfg atomically. The caller is responsible for validation.
func SaveZen(path string, cfg *Zen) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(path, data, 0600)
}
