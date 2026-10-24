// Package providers persists custom provider definitions and their cooldown
// table. It performs no upstream calls.
package providers

import (
	"encoding/json"
	"os"
	"time"

	"cline-go-proxy/internal/domain"
	"cline-go-proxy/internal/platform"
)

const (
	// FileName is the data file holding custom provider definitions.
	FileName = ".cline-providers.json"
	// CooldownsFileName is the data file holding provider:model cooldowns.
	CooldownsFileName = ".cline-providers-cooldowns.json"
)

// Load reads path. A missing or unreadable file yields nil and no error; a
// decode error yields nil and the error so the caller can back up the file.
func Load(path string) ([]*domain.CustomProvider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var list []*domain.CustomProvider
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// Save writes the provider list atomically.
func Save(path string, list []*domain.CustomProvider) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(path, data, 0600)
}

// LoadCooldowns reads the cooldown table and drops expired entries. A missing
// or unreadable file yields an empty map and no error; a decode error yields an
// empty map and the error so the caller can back up the file.
func LoadCooldowns(path string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out, nil
	}
	var raw map[string]int64
	if err := json.Unmarshal(data, &raw); err != nil {
		return out, err
	}
	now := time.Now()
	for k, v := range raw {
		if until := time.UnixMicro(v); until.After(now) {
			out[k] = until
		}
	}
	return out, nil
}

// SaveCooldowns persists only future entries. When none remain it removes the
// data file instead of writing an empty object; removal is idempotent.
func SaveCooldowns(path string, cooldowns map[string]time.Time) error {
	now := time.Now()
	raw := make(map[string]int64, len(cooldowns))
	for k, until := range cooldowns {
		if until.IsZero() || !until.After(now) {
			continue
		}
		raw[k] = until.UnixMicro()
	}
	if len(raw) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(path, data, 0600)
}
