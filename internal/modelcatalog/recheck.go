package modelcatalog

// TrackMissing updates consecutive-miss counters for declared models and
// returns the models that crossed missLimit plus the number still counting.
// A model present upstream resets its counter; a declared model absent
// upstream increments it; counters for ids no longer declared are dropped.
// The miss map is mutated in place and pruned of undeclared ids.
func TrackMissing(declared []string, upstream map[string]bool, miss map[string]int, missLimit int) (toMark []string, counting int) {
	declaredSet := make(map[string]bool, len(declared))
	for _, id := range declared {
		if id == "" {
			continue
		}
		declaredSet[id] = true
		if upstream[id] {
			delete(miss, id)
			continue
		}
		miss[id]++
	}
	for id := range miss {
		if !declaredSet[id] {
			delete(miss, id)
		}
	}
	for id, n := range miss {
		if n >= missLimit {
			toMark = append(toMark, id)
		} else {
			counting++
		}
	}
	return toMark, counting
}

// FilterNew returns upstream entries that are neither declared by the provider
// nor already present in the pool. Order is preserved.
func FilterNew(entries []Entry, declared map[string]bool, poolHas map[string]bool) []Entry {
	fresh := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if declared[e.ID] || poolHas[e.ID] {
			continue
		}
		fresh = append(fresh, e)
	}
	return fresh
}
