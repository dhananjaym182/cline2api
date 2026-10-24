package modelcatalog

import "time"

// ImportItem is one model in a preview import batch.
type ImportItem struct {
	ModelID string `json:"modelId"`
	Tier    string `json:"tier"`
	Signal  string `json:"signal"`
	Action  string `json:"action"` // add | tier_change | nochange | protected
	OldTier string `json:"oldTier,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// ImportBatch is an in-memory preview awaiting administrator approval.
type ImportBatch struct {
	ID           string       `json:"id"`
	ProviderID   string       `json:"providerId"`
	ProviderName string       `json:"providerName"`
	CreatedAt    time.Time    `json:"createdAt"`
	Auto         bool         `json:"auto,omitempty"`
	Items        []ImportItem `json:"items"`
}

// Existing describes the pool entry an imported model would collide with.
type Existing struct {
	Source     string
	TierSignal string
	Cost       string
}

// BuildImportItems classifies entries and diffs them against existing pool
// entries. The caller owns batch persistence and pool locking.
func BuildImportItems(entries []Entry, providerFree bool, existing map[string]Existing, allowlist map[string]bool) []ImportItem {
	items := make([]ImportItem, 0, len(entries))
	for _, e := range entries {
		tier, signal := Classify(e, providerFree, allowlist)
		item := ImportItem{ModelID: e.ID, Tier: tier, Signal: signal, Action: "add"}
		if m, ok := existing[e.ID]; ok {
			switch {
			case m.Source != "" && m.Source != "provider":
				item.Action = "protected"
				item.Reason = "模型归属 " + m.Source + " 同步链，不跨源改层级"
			case m.TierSignal == "manual":
				item.Action = "protected"
				item.Reason = "用户手动设置过层级"
			case m.Cost == tier:
				item.Action = "nochange"
			default:
				item.Action = "tier_change"
				item.OldTier = m.Cost
			}
		}
		items = append(items, item)
	}
	return items
}
