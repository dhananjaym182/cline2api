package main

import (
	"reflect"
	"testing"

	"cline-go-proxy/internal/providerreg"
)

// The embedded JSON catalog replaced the in-code preset maps. Until the old
// maps are removed, this test proves semantic parity in both directions.
func TestEmbeddedPresetsMatchRootPresetMaps(t *testing.T) {
	reg := providerreg.Builtin()
	merged := mergePresets(providerPresets, providerPresetsImported)

	if reg.Len() != len(merged) {
		t.Fatalf("embedded presets = %d, root maps = %d", reg.Len(), len(merged))
	}
	for id, want := range merged {
		got, ok := reg.Get(id)
		if !ok {
			t.Errorf("embedded registry missing preset %q", id)
			continue
		}
		if got.ID != id {
			t.Errorf("preset %q: embedded id = %q", id, got.ID)
		}
		if got.Name != want.Name {
			t.Errorf("preset %q: name = %q, want %q", id, got.Name, want.Name)
		}
		if got.BaseURL != want.BaseURL {
			t.Errorf("preset %q: baseURL = %q, want %q", id, got.BaseURL, want.BaseURL)
		}
		if got.Notes != want.Notes {
			t.Errorf("preset %q: notes differ", id)
		}
		if got.FreeTier != want.FreeTier {
			t.Errorf("preset %q: freeTier = %v, want %v", id, got.FreeTier, want.FreeTier)
		}
		if got.Kind != want.Kind {
			t.Errorf("preset %q: kind = %q, want %q", id, got.Kind, want.Kind)
		}
		if got.LoginFlow != want.LoginFlow {
			t.Errorf("preset %q: loginFlow = %v, want %v", id, got.LoginFlow, want.LoginFlow)
		}
		if !reflect.DeepEqual(got.Headers, want.Headers) {
			t.Errorf("preset %q: headers differ: %v vs %v", id, got.Headers, want.Headers)
		}
		if !reflect.DeepEqual(got.ModelIDs, want.ModelIDs) {
			t.Errorf("preset %q: modelIds differ: %v vs %v", id, got.ModelIDs, want.ModelIDs)
		}
		if (got.ModelsFetcher == nil) != (want.ModelsFetcher == nil) {
			t.Errorf("preset %q: modelsFetcher presence differs", id)
		} else if got.ModelsFetcher != nil && *got.ModelsFetcher != *want.ModelsFetcher {
			t.Errorf("preset %q: modelsFetcher = %+v, want %+v", id, *got.ModelsFetcher, *want.ModelsFetcher)
		}
	}
	for _, id := range reg.IDs() {
		if _, ok := merged[id]; !ok {
			t.Errorf("root maps missing embedded preset %q", id)
		}
	}
}
