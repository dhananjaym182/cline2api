package providerreg

import (
	"strings"
	"testing"
)

func TestLoadEmbedded(t *testing.T) {
	reg, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	if reg.Len() < 100 {
		t.Errorf("preset count = %d, want the full built-in catalog", reg.Len())
	}
	for _, id := range []string{"openrouter", "kilocode", "ollama-local", "groq"} {
		if _, ok := reg.Get(id); !ok {
			t.Errorf("missing preset %q", id)
		}
	}
}

func TestBuiltinIsStable(t *testing.T) {
	a := Builtin()
	b := Builtin()
	if a != b {
		t.Error("Builtin must return the same immutable registry")
	}
	if a.Len() == 0 {
		t.Error("Builtin is empty")
	}
}

func TestRegistryAllReturnsCopy(t *testing.T) {
	reg := Builtin()
	all := reg.All()
	if len(all) != reg.Len() {
		t.Fatalf("All len = %d, want %d", len(all), reg.Len())
	}
	first := reg.IDs()[0]
	p := all[first]
	p.Name = "mutated"
	all[first] = p
	again, _ := reg.Get(first)
	if again.Name == "mutated" {
		t.Error("All must not alias the registry map")
	}
}

func TestRegistryIDsSorted(t *testing.T) {
	ids := Builtin().IDs()
	for i := 1; i < len(ids); i++ {
		if ids[i-1] > ids[i] {
			t.Fatalf("IDs not sorted at %d: %q > %q", i, ids[i-1], ids[i])
		}
	}
}

func validPreset() Preset {
	return Preset{Name: "Test", BaseURL: "https://example.com/v1", Kind: "freeTier"}
}

func TestValidateRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		key  string
		p    Preset
		want string
	}{
		{"empty key", "", validPreset(), "empty key"},
		{"id mismatch", "a", func() Preset { p := validPreset(); p.ID = "b"; return p }(), "does not match key"},
		{"missing name", "a", func() Preset { p := validPreset(); p.Name = " "; return p }(), "name is required"},
		{"bad base url", "a", func() Preset { p := validPreset(); p.BaseURL = "ftp://x"; return p }(), "absolute http(s)"},
		{"relative base url", "a", func() Preset { p := validPreset(); p.BaseURL = "/v1"; return p }(), "absolute http(s)"},
		{"bad kind", "a", func() Preset { p := validPreset(); p.Kind = "weird"; return p }(), "unknown kind"},
		{"free without tier", "a", func() Preset { p := validPreset(); p.Kind = "free"; p.FreeTier = false; return p }(), "requires freeTier"},
		{"bad protocol", "a", func() Preset { p := validPreset(); p.Protocol = "anthropic"; return p }(), "unsupported protocol"},
		{"bad fetcher type", "a", func() Preset {
			p := validPreset()
			p.ModelsFetcher = &ModelsFetcher{URL: "https://example.com/models", Type: "gemini"}
			return p
		}(), "unsupported modelsFetcher type"},
		{"bad fetcher url", "a", func() Preset {
			p := validPreset()
			p.ModelsFetcher = &ModelsFetcher{URL: "nope"}
			return p
		}(), "must be absolute"},
		{"empty model id", "a", func() Preset { p := validPreset(); p.ModelIDs = []string{"m", " "}; return p }(), "empty model id"},
		{"duplicate model id", "a", func() Preset { p := validPreset(); p.ModelIDs = []string{"m", "m"}; return p }(), "duplicate model id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(map[string]Preset{tc.key: tc.p})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestValidateAcceptsProtocolAndFetcher(t *testing.T) {
	p := validPreset()
	p.ID = "a"
	p.Protocol = "openai-chat"
	p.ModelsFetcher = &ModelsFetcher{URL: "https://example.com/models", Type: "openai"}
	if err := Validate(map[string]Preset{"a": p}); err != nil {
		t.Fatalf("valid preset rejected: %v", err)
	}
}
