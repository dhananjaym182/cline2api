package modelcatalog

import "testing"

func TestBuildImportItemsActions(t *testing.T) {
	entries := []Entry{
		{ID: "new-free", FreeFlag: true},
		{ID: "remote-model"},
		{ID: "manual-model"},
		{ID: "same-tier"},
		{ID: "changed-tier"},
	}
	existing := map[string]Existing{
		"remote-model": {Source: "remote", Cost: "free"},
		"manual-model": {TierSignal: "manual", Cost: "pass"},
		"same-tier":    {Source: "provider", Cost: "pass"},
		"changed-tier": {Source: "provider", Cost: "free"},
	}
	items := BuildImportItems(entries, false, existing, nil)
	if len(items) != 5 {
		t.Fatalf("items = %+v", items)
	}
	byID := map[string]ImportItem{}
	for _, it := range items {
		byID[it.ModelID] = it
	}
	if byID["new-free"].Action != "add" || byID["new-free"].Tier != "free" {
		t.Errorf("new-free = %+v", byID["new-free"])
	}
	if byID["remote-model"].Action != "protected" || byID["remote-model"].Reason == "" {
		t.Errorf("remote-model = %+v", byID["remote-model"])
	}
	if byID["manual-model"].Action != "protected" {
		t.Errorf("manual-model = %+v", byID["manual-model"])
	}
	if byID["same-tier"].Action != "nochange" {
		t.Errorf("same-tier = %+v", byID["same-tier"])
	}
	if byID["changed-tier"].Action != "tier_change" || byID["changed-tier"].OldTier != "free" || byID["changed-tier"].Tier != "pass" {
		t.Errorf("changed-tier = %+v", byID["changed-tier"])
	}
}

func TestBuildImportItemsEmpty(t *testing.T) {
	if got := BuildImportItems(nil, false, nil, nil); len(got) != 0 {
		t.Errorf("items = %+v", got)
	}
}
