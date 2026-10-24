package dispatch

import (
	"reflect"
	"testing"
)

func TestBuildChain(t *testing.T) {
	got := BuildChain("asked", []string{"", "asked", "a", "b", "a"})
	want := []string{"asked", "a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chain = %v, want %v", got, want)
	}
	if got := BuildChain("only", nil); !reflect.DeepEqual(got, []string{"only"}) {
		t.Fatalf("empty configured chain = %v", got)
	}
}

func TestFilterLive(t *testing.T) {
	live := map[string]bool{"a": true, "c": true}

	kept, dropped := FilterLive([]string{"a", "b", "c"}, true, live)
	if !reflect.DeepEqual(kept, []string{"a", "c"}) || !reflect.DeepEqual(dropped, []string{"b"}) {
		t.Fatalf("kept=%v dropped=%v", kept, dropped)
	}

	// Unknown catalog: keep everything, report nothing.
	kept, dropped = FilterLive([]string{"a", "b"}, false, live)
	if !reflect.DeepEqual(kept, []string{"a", "b"}) || dropped != nil {
		t.Fatalf("inactive catalog kept=%v dropped=%v", kept, dropped)
	}

	// Filtering everything would leave no candidate: keep the original chain.
	kept, dropped = FilterLive([]string{"x", "y"}, true, live)
	if !reflect.DeepEqual(kept, []string{"x", "y"}) || dropped != nil {
		t.Fatalf("all-stale kept=%v dropped=%v", kept, dropped)
	}

	// Empty entries survive filtering, matching the previous model-chain
	// behavior where the caller passes them through.
	kept, _ = FilterLive([]string{"", "a"}, true, live)
	if !reflect.DeepEqual(kept, []string{"", "a"}) {
		t.Fatalf("empty entry kept=%v", kept)
	}
}

func TestHasFallbackAfter(t *testing.T) {
	chain := []string{"a", "b", "c"}
	if HasFallbackAfter(chain, "a") != true || HasFallbackAfter(chain, "c") != false {
		t.Fatal("fallback detection mismatch")
	}
	if HasFallbackAfter(chain, "missing") {
		t.Fatal("unknown current must not report a fallback")
	}
	if HasFallbackAfter([]string{"a"}, "a") {
		t.Fatal("single-entry chain has no fallback")
	}
}

func TestIsFreeAlias(t *testing.T) {
	chain := []string{"m1", "m2"}
	if !IsFreeAlias("free", nil) || !IsFreeAlias("m2", chain) {
		t.Fatal("expected free alias membership")
	}
	if IsFreeAlias("m3", chain) || IsFreeAlias("", chain) {
		t.Fatal("unexpected free alias membership")
	}
}

func TestOrderByAvailability(t *testing.T) {
	got := OrderByAvailability([]ModelAvailability{
		{Model: "cold", Available: false, MinUsage: -1},
		{Model: "hot", Available: true, MinUsage: 90},
		{Model: "warm", Available: true, MinUsage: 5},
		{Model: "tie", Available: true, MinUsage: 90},
	})
	want := []string{"warm", "hot", "tie", "cold"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}

	// The input slice must not be mutated.
	in := []ModelAvailability{{Model: "b", Available: false}, {Model: "a", Available: true}}
	OrderByAvailability(in)
	if in[0].Model != "b" {
		t.Fatalf("input mutated: %v", in)
	}
}
