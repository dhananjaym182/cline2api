package syncstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	states, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || states != nil {
		t.Fatalf("states/err = %v/%v", states, err)
	}
}

func TestLoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte("{bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	in := map[string]State{
		"p1": {LastCheckedMs: 1700000000000, LastError: "timeout", DelistedMarked: 2, MissCounting: 1, NewQueued: 3, BatchID: "b1"},
		"p2": {LastCheckedMs: 1700000001000},
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out) != 2 || out["p1"] == nil {
		t.Fatalf("out = %+v", out)
	}
	if out["p1"].LastError != "timeout" || out["p1"].DelistedMarked != 2 || out["p1"].BatchID != "b1" {
		t.Errorf("fields lost: %+v", out["p1"])
	}
	if out["p2"].LastCheckedMs != 1700000001000 {
		t.Errorf("p2 = %+v", out["p2"])
	}
}
