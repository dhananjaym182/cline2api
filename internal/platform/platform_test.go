package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKnownDataFilesReturnsCopy(t *testing.T) {
	files := KnownDataFiles()
	if len(files) == 0 {
		t.Fatal("known data file list is empty")
	}
	if files[0] != ".cline-accounts.json" {
		t.Fatalf("first data file = %q, want .cline-accounts.json", files[0])
	}
	files[0] = "mutated"
	if KnownDataFiles()[0] != ".cline-accounts.json" {
		t.Fatal("KnownDataFiles returned the internal slice")
	}
}

func TestResolveDataPathUsesEnvDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(DataDirEnv, dir)

	got := ResolveDataPath(".cline-config.json", nil)
	want := filepath.Join(dir, ".cline-config.json")
	if got != want {
		t.Fatalf("ResolveDataPath = %q, want %q", got, want)
	}
}

func TestResolveDataPathIsolatedUnderTest(t *testing.T) {
	t.Setenv(DataDirEnv, "")

	got := ResolveDataPath(".cline-config.json", nil)
	isolated := TestIsolatedDataDir()
	if isolated == "" {
		t.Fatal("test isolation directory is empty")
	}
	if filepath.Dir(got) != isolated {
		t.Fatalf("ResolveDataPath = %q, want parent %q", got, isolated)
	}
}

func TestMigrateLegacyDataFilesIsIdempotent(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	name := ".cline-config.json"
	if err := os.WriteFile(filepath.Join(src, name), []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}

	legacy := func() []string { return []string{src} }
	MigrateLegacyDataFiles(dst, legacy)
	got, err := os.ReadFile(filepath.Join(dst, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Fatalf("migrated content = %q, want first", got)
	}

	if err := os.WriteFile(filepath.Join(src, name), []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	MigrateLegacyDataFiles(dst, legacy)
	got, err = os.ReadFile(filepath.Join(dst, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Fatalf("idempotent migration overwrote destination: %q", got)
	}
}

func TestWriteFileAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	if err := WriteFileAtomic(path, []byte("v1"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("v2"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2" {
		t.Fatalf("content = %q, want v2", got)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file was not renamed: %v", err)
	}
}

func TestBackupCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	BackupCorruptFile(path, os.ErrInvalid)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("original file still exists: %v", err)
	}
	matches, err := filepath.Glob(path + ".corrupt-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("backup count = %d, want 1", len(matches))
	}
}

func TestEnvInt(t *testing.T) {
	t.Setenv("CLINE2API_TEST_INT", "7")
	if got := EnvInt("CLINE2API_TEST_INT", 3); got != 7 {
		t.Fatalf("EnvInt = %d, want 7", got)
	}
	t.Setenv("CLINE2API_TEST_INT", "0")
	if got := EnvInt("CLINE2API_TEST_INT", 3); got != 3 {
		t.Fatalf("EnvInt fallback = %d, want 3", got)
	}
	t.Setenv("CLINE2API_TEST_INT", "bad")
	if got := EnvInt("CLINE2API_TEST_INT", 3); got != 3 {
		t.Fatalf("EnvInt invalid fallback = %d, want 3", got)
	}
}

func TestEnvDurationFromMS(t *testing.T) {
	t.Setenv("CLINE2API_TEST_MS", "1500")
	if got := EnvDurationFromMS("CLINE2API_TEST_MS", time.Second); got != 1500*time.Millisecond {
		t.Fatalf("EnvDurationFromMS = %v, want 1.5s", got)
	}
	t.Setenv("CLINE2API_TEST_MS", "-1")
	if got := EnvDurationFromMS("CLINE2API_TEST_MS", time.Second); got != time.Second {
		t.Fatalf("EnvDurationFromMS fallback = %v, want 1s", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("short", 10); got != "short" {
		t.Fatalf("Truncate = %q, want short", got)
	}
	if got := Truncate("abcdef", 3); got != "abc..." {
		t.Fatalf("Truncate = %q, want abc...", got)
	}
	if !strings.HasPrefix(Truncate("abcdef", 3), "abc") {
		t.Fatal("Truncate did not preserve prefix")
	}
}
