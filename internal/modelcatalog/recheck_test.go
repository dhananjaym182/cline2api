package modelcatalog

import "testing"

func TestTrackMissing(t *testing.T) {
	miss := map[string]int{}
	upstream := map[string]bool{"a": true, "c": true}
	declared := []string{"a", "b"}

	toMark, counting := TrackMissing(declared, upstream, miss, 2)
	if len(toMark) != 0 || counting != 1 {
		t.Fatalf("round 1: toMark=%v counting=%d", toMark, counting)
	}
	if miss["b"] != 1 {
		t.Fatalf("b miss = %d", miss["b"])
	}
	if _, ok := miss["a"]; ok {
		t.Fatal("present model must not have a miss counter")
	}

	toMark, counting = TrackMissing(declared, upstream, miss, 2)
	if len(toMark) != 1 || toMark[0] != "b" || counting != 0 {
		t.Fatalf("round 2: toMark=%v counting=%d", toMark, counting)
	}

	// Reappearance resets the counter and clears the mark.
	toMark, _ = TrackMissing(declared, map[string]bool{"a": true, "b": true}, miss, 2)
	if len(toMark) != 0 || miss["b"] != 0 {
		t.Fatalf("reappeared model: toMark=%v miss[b]=%d", toMark, miss["b"])
	}

	// Undeclared ids stop being tracked.
	miss["old"] = 5
	TrackMissing([]string{"a"}, map[string]bool{"a": true}, miss, 2)
	if _, ok := miss["old"]; ok {
		t.Fatal("undeclared id must be pruned")
	}
}

func TestFilterNew(t *testing.T) {
	entries := []Entry{{ID: "declared"}, {ID: "in-pool"}, {ID: "fresh"}, {ID: "fresh-two"}}
	fresh := FilterNew(entries, map[string]bool{"declared": true}, map[string]bool{"in-pool": true})
	if len(fresh) != 2 || fresh[0].ID != "fresh" || fresh[1].ID != "fresh-two" {
		t.Fatalf("fresh = %+v", fresh)
	}
	if got := FilterNew(entries, map[string]bool{"declared": true, "in-pool": true, "fresh": true, "fresh-two": true}, nil); len(got) != 0 {
		t.Fatalf("expected no fresh entries, got %+v", got)
	}
}
