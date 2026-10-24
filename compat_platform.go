package main

import (
	"os"
	"time"

	"cline-go-proxy/internal/platform"
)

// Temporary root compatibility layer for the platform extraction. Call sites
// migrate to internal/platform first; these wrappers are removed once no
// package-main caller or local test depends on them.

const dataDirEnv = platform.DataDirEnv

var knownDataFiles = platform.KnownDataFiles()

// legacyDataDirsOverride lets local tests substitute legacy directory probing.
var legacyDataDirsOverride func() []string

func resolveDataPath(filename string) string {
	return platform.ResolveDataPath(filename, legacyDataDirsOverride)
}

func currentDataDir() string {
	return platform.CurrentDataDir()
}

func envDataDir() string {
	return platform.EnvDataDir()
}

func testIsolatedDataDir() string {
	return platform.TestIsolatedDataDir()
}

func migrateLegacyDataOnce(dir string) {
	platform.MigrateLegacyDataFilesOnce(dir, legacyDataDirsOverride)
}

func migrateLegacyDataFiles(dir string) {
	platform.MigrateLegacyDataFiles(dir, legacyDataDirsOverride)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	return platform.WriteFileAtomic(path, data, perm)
}

func backupCorruptFile(path string, cause error) {
	platform.BackupCorruptFile(path, cause)
}

func fileExists(p string) bool {
	return platform.FileExists(p)
}

func truncate(s string, maxLen int) string {
	return platform.Truncate(s, maxLen)
}

func envInt(key string, def int) int {
	return platform.EnvInt(key, def)
}

func envDurationFromMs(key string, def time.Duration) time.Duration {
	return platform.EnvDurationFromMS(key, def)
}
