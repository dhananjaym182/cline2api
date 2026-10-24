package providers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cline-go-proxy/internal/domain"
)

func TestLoadMissingFile(t *testing.T) {
	list, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || list != nil {
		t.Fatalf("list/err = %v/%v", list, err)
	}
}

func TestLoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte("[oops"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	in := []*domain.CustomProvider{{
		ID:       "p1",
		Name:     "OpenRouter",
		BaseURL:  "https://openrouter.ai/api/v1",
		APIKey:   "sk-test",
		ModelIDs: []string{"a/b"},
		Enabled:  true,
		Free:     true,
		Headers:  map[string]string{"HTTP-Referer": "https://example.com"},
	}}
	if err := Save(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out) != 1 || out[0].Name != "OpenRouter" || out[0].Headers["HTTP-Referer"] != "https://example.com" {
		t.Errorf("round trip lost data: %+v", out)
	}
}

func TestLoadCooldownsDropsExpired(t *testing.T) {
	path := filepath.Join(t.TempDir(), CooldownsFileName)
	now := time.Now()
	payload := `{"p:future":` + itoa(now.Add(time.Hour).UnixMicro()) + `,"p:past":` + itoa(now.Add(-time.Hour).UnixMicro()) + `}`
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := LoadCooldowns(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := out["p:past"]; ok {
		t.Error("expired entry retained")
	}
	if _, ok := out["p:future"]; !ok {
		t.Error("future entry dropped")
	}
}

func TestLoadCooldownsCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), CooldownsFileName)
	if err := os.WriteFile(path, []byte("nope"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := LoadCooldowns(path)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if out == nil || len(out) != 0 {
		t.Errorf("out = %v", out)
	}
}

func TestSaveCooldownsRemovesFileWhenEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), CooldownsFileName)
	if err := os.WriteFile(path, []byte(`{"p:old":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveCooldowns(path, map[string]time.Time{"p:old": time.Now().Add(-time.Minute)}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expired-only cooldown file should be removed, stat err = %v", err)
	}
}

func TestSaveCooldownsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), CooldownsFileName)
	until := time.Now().Add(30 * time.Minute)
	if err := SaveCooldowns(path, map[string]time.Time{"p:model": until, "p:stale": time.Time{}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := LoadCooldowns(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("out = %v", out)
	}
	if got := out["p:model"]; got.IsZero() || !got.After(time.Now()) {
		t.Errorf("cooldown = %v", got)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
