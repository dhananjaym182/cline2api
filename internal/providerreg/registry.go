// Package providerreg defines provider presets and their immutable built-in
// registry. Presets are compile-time data: editing a JSON file requires
// rebuilding the binary. Runtime custom providers are stored separately in
// .cline-providers.json and apply without a rebuild.
package providerreg

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"strings"
	"sync"
)

//go:embed presets/*.json
var presetFS embed.FS

// ModelsFetcher describes an upstream model-catalog endpoint.
type ModelsFetcher struct {
	URL  string `json:"url"`
	Type string `json:"type"` // "openai" (the only supported catalog format)
}

// Preset is the data-only definition of a known provider. Provider-level hints
// such as FreeTier and Kind describe the provider offering; they are not
// per-model access entitlements and must not be used as such.
type Preset struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseURL"`
	// Protocol is the transport protocol. Empty means OpenAI-compatible chat.
	Protocol string            `json:"protocol,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Notes    string            `json:"notes,omitempty"`
	// FreeTier is a provider-level UI hint, not a per-model entitlement.
	FreeTier bool `json:"freeTier"`
	// ModelIDs seeds the provider's catalog; models are still classified by
	// the import path before they become routable.
	ModelIDs      []string       `json:"modelIds,omitempty"`
	ModelsFetcher *ModelsFetcher `json:"modelsFetcher,omitempty"`
	// Kind is the UI grouping hint: free, freeTier, hasFree, local, or other.
	Kind string `json:"kind,omitempty"`
	// LoginFlow marks providers authenticated by a device/browser login
	// connection instead of an API key.
	LoginFlow bool `json:"loginFlow,omitempty"`
}

// Registry is an immutable preset lookup.
type Registry struct {
	byID map[string]Preset
}

// New validates presets and returns an immutable registry.
func New(presets map[string]Preset) (*Registry, error) {
	if err := Validate(presets); err != nil {
		return nil, err
	}
	cp := make(map[string]Preset, len(presets))
	for id, p := range presets {
		cp[id] = p
	}
	return &Registry{byID: cp}, nil
}

// Get returns a preset by ID.
func (r *Registry) Get(id string) (Preset, bool) {
	p, ok := r.byID[id]
	return p, ok
}

// All returns a copy of every preset.
func (r *Registry) All() map[string]Preset {
	out := make(map[string]Preset, len(r.byID))
	for id, p := range r.byID {
		out[id] = p
	}
	return out
}

// IDs returns the preset IDs in sorted order.
func (r *Registry) IDs() []string {
	ids := make([]string, 0, len(r.byID))
	for id := range r.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Len returns the number of presets.
func (r *Registry) Len() int { return len(r.byID) }

var builtinKinds = map[string]bool{"": true, "free": true, "freeTier": true, "hasFree": true, "local": true, "other": true}

// knownFetcherTypes lists catalog fetcher implementations that exist in the
// codebase. Providers with a specialized fetcher still speak OpenAI-compatible
// chat; only their catalog format differs.
var knownFetcherTypes = map[string]bool{
	"":              true,
	"openai":        true,
	"agentrouter":   true,
	"bynara":        true,
	"hcnsec":        true,
	"inxora":        true,
	"mimo-free":     true,
	"opencode-free": true,
}

// Validate checks every preset. The map key must equal the preset ID.
func Validate(presets map[string]Preset) error {
	seenIDs := make(map[string]string, len(presets))
	for key, p := range presets {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("preset with empty key")
		}
		if p.ID != "" && p.ID != key {
			return fmt.Errorf("preset %q: id %q does not match key", key, p.ID)
		}
		if prev, dup := seenIDs[p.ID]; dup {
			return fmt.Errorf("preset id %q duplicated (%s and %s)", p.ID, prev, key)
		}
		seenIDs[p.ID] = key
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("preset %q: name is required", key)
		}
		u, err := url.Parse(p.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("preset %q: baseURL %q must be an absolute http(s) URL", key, p.BaseURL)
		}
		if !builtinKinds[p.Kind] {
			return fmt.Errorf("preset %q: unknown kind %q", key, p.Kind)
		}
		if p.Kind == "free" && !p.FreeTier {
			return fmt.Errorf("preset %q: kind free requires freeTier", key)
		}
		if p.Protocol != "" && p.Protocol != "openai-chat" {
			return fmt.Errorf("preset %q: unsupported protocol %q", key, p.Protocol)
		}
		if p.ModelsFetcher != nil {
			fu, err := url.Parse(p.ModelsFetcher.URL)
			if err != nil || (fu.Scheme != "http" && fu.Scheme != "https") || fu.Host == "" {
				return fmt.Errorf("preset %q: modelsFetcher URL %q must be absolute", key, p.ModelsFetcher.URL)
			}
			if !knownFetcherTypes[p.ModelsFetcher.Type] {
				return fmt.Errorf("preset %q: unsupported modelsFetcher type %q", key, p.ModelsFetcher.Type)
			}
		}
		seenModels := make(map[string]bool, len(p.ModelIDs))
		for _, id := range p.ModelIDs {
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("preset %q: empty model id", key)
			}
			if seenModels[id] {
				return fmt.Errorf("preset %q: duplicate model id %q", key, id)
			}
			seenModels[id] = true
		}
	}
	return nil
}

// LoadEmbedded reads and validates the embedded preset files. Each filename
// must equal "<id>.json".
func LoadEmbedded() (*Registry, error) {
	entries, err := fs.ReadDir(presetFS, "presets")
	if err != nil {
		return nil, fmt.Errorf("read embedded presets: %w", err)
	}
	presets := make(map[string]Preset, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		data, err := presetFS.ReadFile("presets/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read preset %s: %w", entry.Name(), err)
		}
		var p Preset
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("decode preset %s: %w", entry.Name(), err)
		}
		if p.ID != name {
			return nil, fmt.Errorf("preset %s: id %q must match filename", entry.Name(), p.ID)
		}
		if _, dup := presets[p.ID]; dup {
			return nil, fmt.Errorf("preset %s: duplicate id", entry.Name())
		}
		presets[p.ID] = p
	}
	if len(presets) == 0 {
		return nil, fmt.Errorf("no embedded presets found")
	}
	return New(presets)
}

var (
	builtinOnce sync.Once
	builtin     *Registry
	builtinErr  error
)

// Builtin returns the immutable registry compiled into the binary. Invalid
// embedded data is a build defect and panics at first use.
func Builtin() *Registry {
	builtinOnce.Do(func() {
		builtin, builtinErr = LoadEmbedded()
	})
	if builtinErr != nil {
		panic("providerreg: invalid embedded presets: " + builtinErr.Error())
	}
	return builtin
}
