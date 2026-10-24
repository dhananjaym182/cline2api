package requestlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cline-go-proxy/internal/domain"
)

func TestLoadMissingFile(t *testing.T) {
	entries, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || entries != nil {
		t.Fatalf("entries/err = %v/%v", entries, err)
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

func TestSaveLoadRoundTripExcludesToolNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	started := time.Unix(1700000000, 0).UTC()
	in := []domain.RequestLog{{
		ID:         "req-1",
		StartedAt:  started,
		AccountID:  "acct-1",
		Protocol:   "openai-chat",
		Model:      "m1",
		ComboID:    "c1",
		Attempts:   []domain.AttemptLog{{Model: "m1", Outcome: "success"}},
		ToolNames:  map[string]string{"t": "long_tool"},
		OutputTPS:  9.5,
		FinishedAt: started.Add(time.Second),
	}}
	if err := Save(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" || contains(string(raw), "long_tool") || contains(string(raw), "ToolNames") {
		t.Errorf("tool names leaked into persisted log: %s", raw)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out) != 1 || out[0].ComboID != "c1" || len(out[0].Attempts) != 1 || out[0].OutputTPS != 9.5 {
		t.Errorf("round trip lost data: %+v", out)
	}
	if out[0].ToolNames != nil {
		t.Errorf("ToolNames should not be restored from disk: %+v", out[0].ToolNames)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
