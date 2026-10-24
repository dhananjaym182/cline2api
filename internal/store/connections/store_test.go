package connections

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
	if err := os.WriteFile(path, []byte("{bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	in := []*domain.Connection{{
		ID:                   "conn-1",
		Provider:             "kilocode",
		AuthType:             "device_code",
		Email:                "user@example.com",
		AccessToken:          "access",
		RefreshToken:         "refresh",
		ExpiresAt:            time.Now().Add(time.Hour).UnixMilli(),
		ProviderSpecificData: map[string]string{"orgId": "org-1"},
		Status:               "active",
		UsageCount:           7,
		ModelStats:           map[string]*domain.ModelStat{"m": {ModelID: "m", UsageCount: 2}},
	}}
	if err := Save(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("out = %+v", out)
	}
	got := out[0]
	if got.AccessToken != "access" || got.ProviderSpecificData["orgId"] != "org-1" || got.UsageCount != 7 {
		t.Errorf("round trip lost data: %+v", got)
	}
	if got.ModelStats["m"] == nil || got.ModelStats["m"].UsageCount != 2 {
		t.Errorf("model stats lost: %+v", got.ModelStats)
	}
}
