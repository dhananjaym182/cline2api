// Package dispatch owns routing decisions: chain construction, availability
// ordering, cooldown classification, and Free-only entitlement. It performs no
// HTTP writes and owns no storage; callers supply catalog and account snapshots.
package dispatch

import "sort"

// BuildChain returns the ordered, de-duplicated fallback chain for a request:
// the explicitly requested model first, followed by every configured candidate.
// Empty entries are ignored; the requested model is always kept even when it is
// absent from the configured list.
func BuildChain(requested string, configured []string) []string {
	seen := map[string]bool{requested: true}
	chain := []string{requested}
	for _, m := range configured {
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		chain = append(chain, m)
	}
	return chain
}

// FilterLive removes candidates the live catalog no longer knows about.
//
// It is a no-op when the catalog is not active (remoteActive false) or when
// filtering would empty the chain: trying the original chain once is preferable
// to failing with no candidates at all. dropped lists removed entries and is
// nil when nothing was removed; kept is the original chain in that case.
func FilterLive(chain []string, remoteActive bool, live map[string]bool) (kept, dropped []string) {
	if len(chain) == 0 || !remoteActive {
		return chain, nil
	}
	kept = make([]string, 0, len(chain))
	for _, m := range chain {
		if m == "" || live[m] {
			kept = append(kept, m)
			continue
		}
		dropped = append(dropped, m)
	}
	if len(dropped) == 0 || len(kept) == 0 {
		return chain, nil
	}
	return kept, dropped
}

// HasFallbackAfter reports whether another candidate follows current in chain.
// Unknown current values have no known fallback.
func HasFallbackAfter(chain []string, current string) bool {
	for i, m := range chain {
		if m == current {
			return i < len(chain)-1
		}
	}
	return false
}

// IsFreeAlias reports whether model is the "free" alias or a member of the
// derived free chain. Such models must not be appended again as an explicit
// fallback.
func IsFreeAlias(model string, freeChain []string) bool {
	if model == "free" {
		return true
	}
	for _, m := range freeChain {
		if m == model {
			return true
		}
	}
	return false
}

// ModelAvailability is one chain candidate's observed account availability.
type ModelAvailability struct {
	Model string
	// Available is true when at least one active account can serve the model.
	Available bool
	// MinUsage is the lowest usage count among active accounts, or -1 when no
	// active account can serve the model.
	MinUsage int64
}

// OrderByAvailability returns model IDs ordered by:
//  1. an available account first;
//  2. within the available group, lowest usage first, spreading traffic away
//     from heavily used models;
//  3. otherwise the input order is preserved.
func OrderByAvailability(cands []ModelAvailability) []string {
	sorted := append([]ModelAvailability(nil), cands...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Available != sorted[j].Available {
			return sorted[i].Available
		}
		if sorted[i].Available && sorted[i].MinUsage != sorted[j].MinUsage {
			return sorted[i].MinUsage < sorted[j].MinUsage
		}
		return false
	})
	out := make([]string, len(sorted))
	for i, c := range sorted {
		out[i] = c.Model
	}
	return out
}
