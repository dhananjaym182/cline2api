package accounts

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cline-go-proxy/internal/domain"
)

func TestEmptyInitializesSlices(t *testing.T) {
	p := Empty()
	if p.Accounts == nil || p.Keys == nil || p.Models == nil {
		t.Fatalf("nil slices in Empty(): %+v", p)
	}
}

func TestLoadMissingFile(t *testing.T) {
	p, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Accounts == nil || p.Keys == nil || p.Models == nil {
		t.Errorf("nil slices on missing file: %+v", p)
	}
}

func TestLoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte("{oops"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if p == nil || p.Accounts == nil {
		t.Errorf("corrupt load should still return an initialized pool: %+v", p)
	}
}

func TestLoadNormalizesNilSlices(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if p.Accounts == nil || p.Keys == nil || p.Models == nil {
		t.Errorf("nil slices not normalized: %+v", p)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	in := &domain.AccountPool{
		Accounts: []*domain.Account{{
			AccountID:    "acct-1",
			Email:        "user@example.com",
			RefreshToken: "refresh",
			Status:       "active",
			CreatedAt:    time.Unix(1700000000, 0).UTC(),
		}},
		Keys:         []string{"cline_key"},
		Models:       []domain.Model{{ID: "m1", Provider: "p", Cost: "free", Status: "active"}},
		DefaultModel: "m1",
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out.Accounts) != 1 || out.Accounts[0].RefreshToken != "refresh" || out.Keys[0] != "cline_key" {
		t.Errorf("round trip lost data: %+v", out)
	}
	if len(out.Models) != 1 || out.DefaultModel != "m1" {
		t.Errorf("models/default lost: %+v", out)
	}
	if out.Accounts[0].AccessToken != "" {
		t.Error("access token must not be persisted")
	}
}
