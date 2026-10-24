package platform

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// DataDirEnv fixes all data files to one directory when set (highest priority).
// Docker deployments rely on it to keep data on a mounted volume.
const DataDirEnv = "CLINE2API_DATA_DIR"

// knownDataFiles lists every data file considered when migrating an old data
// directory. Keep this list in sync with the files the application persists.
var knownDataFiles = []string{
	".cline-accounts.json",
	".cline-config.json",
	".cline-providers.json",
	".cline-request-logs.json",
	".cline-zen.json",
	".cline-proxy.json",
	".cline-credentials.json",
}

// KnownDataFiles returns a copy so callers cannot mutate the migration list.
func KnownDataFiles() []string {
	out := make([]string, len(knownDataFiles))
	copy(out, knownDataFiles)
	return out
}

// migrateOnce makes automatic legacy migration run at most once per process.
var migrateOnce sync.Once

// MigrateLegacyDataFilesOnce runs the migration for the first data directory
// selected in this process. It is idempotent.
func MigrateLegacyDataFilesOnce(dir string, legacyDirs func() []string) {
	migrateOnce.Do(func() { MigrateLegacyDataFiles(dir, legacyDirs) })
}

var (
	testDataDirOnce sync.Once
	testDataDir     string
)

// TestIsolatedDataDir lazily creates a temporary directory used by go test so
// tests never read or write the developer's real data files.
func TestIsolatedDataDir() string {
	testDataDirOnce.Do(func() {
		dir, err := os.MkdirTemp(os.TempDir(), "cline2api-test-data-")
		if err != nil {
			log.Printf("Failed to create isolated test data dir: %v", err)
			return
		}
		testDataDir = dir
	})
	return testDataDir
}

// EnvDataDir returns the configured data directory, creating it if needed.
func EnvDataDir() string {
	dir := strings.TrimSpace(os.Getenv(DataDirEnv))
	if dir == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Printf("Failed to create data dir %s: %v", dir, err)
	}
	return dir
}

// ResolveDataPath resolves a data file path using this priority:
//  1. CLINE2API_DATA_DIR (migrating legacy files on first use);
//  2. a per-test temporary directory when running under go test;
//  3. executable directory, working directory, then ~/.cline2api;
//  4. executable directory as the creation fallback.
func ResolveDataPath(filename string, legacyDirs func() []string) string {
	if dir := EnvDataDir(); dir != "" {
		MigrateLegacyDataFilesOnce(dir, legacyDirs)
		return filepath.Join(dir, filename)
	}
	if testing.Testing() {
		if dir := TestIsolatedDataDir(); dir != "" {
			return filepath.Join(dir, filename)
		}
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), filename)
		if FileExists(p) {
			return p
		}
	}
	if pwd, err := os.Getwd(); err == nil {
		p := filepath.Join(pwd, filename)
		if FileExists(p) {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".cline2api", filename)
		if FileExists(p) {
			return p
		}
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), filename)
	}
	pwd, _ := os.Getwd()
	return filepath.Join(pwd, filename)
}

// CurrentDataDir returns the directory where new data files will be created.
func CurrentDataDir() string {
	if dir := EnvDataDir(); dir != "" {
		return dir
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	pwd, _ := os.Getwd()
	return pwd
}

// LegacyDataCandidates returns de-duplicated absolute paths that may contain
// data files from an older installation.
func LegacyDataCandidates(legacyDirs func() []string) []string {
	if legacyDirs != nil {
		return legacyDirs()
	}
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if pwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, pwd)
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".cline2api"))
	}
	seen := make(map[string]bool, len(dirs))
	out := dirs[:0]
	for _, d := range dirs {
		abs, err := filepath.Abs(d)
		if err != nil {
			abs = d
		}
		if !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	return out
}

// MigrateLegacyDataFiles copies known data files from legacy locations into dir
// when the destination does not already exist. It is idempotent.
func MigrateLegacyDataFiles(dir string, legacyDirs func() []string) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Printf("Failed to create data dir %s for migration: %v", dir, err)
		return
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	for _, name := range knownDataFiles {
		dst := filepath.Join(dir, name)
		if FileExists(dst) {
			continue
		}
		for _, d := range LegacyDataCandidates(legacyDirs) {
			if d == absDir {
				continue
			}
			src := filepath.Join(d, name)
			data, err := os.ReadFile(src)
			if err != nil {
				continue
			}
			if err := WriteFileAtomic(dst, data, 0600); err != nil {
				log.Printf("Failed to migrate %s from %s to %s: %v", name, d, dst, err)
				break
			}
			log.Printf("Migrated %s from %s to %s", name, d, dst)
			break
		}
	}
}

// FileExists reports whether path exists.
func FileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
