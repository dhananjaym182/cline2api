package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chdir 临时切换工作目录（使用它的测试不可并行）。
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	})
}

// usePoolPath 把账号池文件指向指定路径并清空内存缓存，测试结束后恢复。
func usePoolPath(t *testing.T, path string) {
	t.Helper()
	oldPool, oldPath := pool, poolPath
	pool, poolPath = nil, path
	t.Cleanup(func() { pool, poolPath = oldPool, oldPath })
}

// TestResolveDataPathEnvOverride 设置了 CLINE2API_DATA_DIR 时，
// 无论工作目录/exe 目录是否存在同名文件，数据一律落到该目录，且目录自动创建。
func TestResolveDataPathEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(dataDirEnv, dir)

	// 工作目录里故意放一个同名文件，验证 env 目录仍具有最高优先级
	chdir(t, t.TempDir())
	if err := os.WriteFile(".cline-accounts.json", []byte("{}"), 0600); err != nil {
		t.Fatalf("seed cwd file: %v", err)
	}

	got := resolveDataPath(".cline-accounts.json")
	if want := filepath.Join(dir, ".cline-accounts.json"); got != want {
		t.Fatalf("resolveDataPath = %q, want %q", got, want)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("data dir not created: %v", err)
	}
}

// TestResolveDataPathLegacyHeuristic 未设置环境变量时保持原有启发式：
// 工作目录存在该文件 → 用工作目录；否则回退 exe 目录。
func TestResolveDataPathLegacyHeuristic(t *testing.T) {
	t.Setenv(dataDirEnv, "") // 显式置空，屏蔽运行环境中可能存在的同名变量

	const name = ".cline-test-fixture.json" // 不会被迁移逻辑使用的独立文件名
	work := t.TempDir()
	chdir(t, work)

	if err := os.WriteFile(filepath.Join(work, name), []byte("{}"), 0600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if got := resolveDataPath(name); got != filepath.Join(work, name) {
		t.Fatalf("resolveDataPath with cwd file = %q, want %q", got, filepath.Join(work, name))
	}

	if err := os.Remove(filepath.Join(work, name)); err != nil {
		t.Fatalf("remove seed: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("executable: %v", err)
	}
	if got := resolveDataPath(name); got != filepath.Join(filepath.Dir(exe), name) {
		t.Fatalf("resolveDataPath fallback = %q, want exe dir %q", got, filepath.Join(filepath.Dir(exe), name))
	}
}

// TestMigrateLegacyDataFiles 切换数据目录时应把旧位置已有的数据文件拷贝过来：
// 仅当目标不存在时拷贝（不覆盖已有新数据），且重复调用幂等。
func TestMigrateLegacyDataFiles(t *testing.T) {
	t.Setenv(dataDirEnv, "")
	work := t.TempDir()
	chdir(t, work)
	dir := filepath.Join(work, "data")

	// 隔离旧目录探测：只把当前临时工作目录视为旧数据位置，
	// 避免 exe 目录/主目录里的同名文件（如其他测试写出的默认配置）干扰断言。
	origOverride := legacyDataDirsOverride
	legacyDataDirsOverride = func() []string { return []string{work} }
	t.Cleanup(func() { legacyDataDirsOverride = origOverride })

	legacy := map[string]string{
		".cline-accounts.json": `{"keys":["sk-old"],"accounts":[]}`,
		".cline-config.json":   `{"strategy":"fill"}`,
	}
	for name, content := range legacy {
		if err := os.WriteFile(filepath.Join(work, name), []byte(content), 0600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	migrateLegacyDataFiles(dir)

	for name, content := range legacy {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("migrated file %s missing: %v", name, err)
		}
		if strings.TrimSpace(string(data)) != content {
			t.Fatalf("migrated %s = %q, want %q", name, data, content)
		}
	}

	// 已存在的目标不被覆盖
	existing := filepath.Join(dir, ".cline-accounts.json")
	if err := os.WriteFile(existing, []byte(`{"keys":["sk-new"],"accounts":[]}`), 0600); err != nil {
		t.Fatalf("seed destination: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, ".cline-accounts.json"), []byte(`{"keys":["sk-older"],"accounts":[]}`), 0600); err != nil {
		t.Fatalf("reseed legacy: %v", err)
	}
	migrateLegacyDataFiles(dir)
	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if strings.TrimSpace(string(data)) != `{"keys":["sk-new"],"accounts":[]}` {
		t.Fatalf("existing destination was overwritten: %q", data)
	}
}

// TestWriteFileAtomic 原子写：内容正确、可覆盖旧内容、不残留 .tmp。
func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".cline-accounts.json")

	if err := writeFileAtomic(path, []byte("v1"), 0600); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := writeFileAtomic(path, []byte("v2"), 0600); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "v2" {
		t.Fatalf("read back = %q, err %v; want \"v2\"", data, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file left behind: %v", err)
	}
}

// TestLoadPoolCorruptFileBackup 账号池文件损坏时：
// 返回空池、原文件改名备份且内容完整保留、后续保存写全新文件且不碰备份。
// 这是防「损坏 → 静默空池 → 空池写回覆盖」数据永久丢失链路的核心测试。
func TestLoadPoolCorruptFileBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".cline-accounts.json")
	corrupt := `{"accounts": [ {"accountId": "a1"` // 模拟写入中途被杀留下的截断 JSON
	if err := os.WriteFile(path, []byte(corrupt), 0600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	usePoolPath(t, path)

	p := loadPool()
	if p == nil || len(p.Accounts) != 0 || len(p.Keys) != 0 {
		t.Fatalf("expected fresh empty pool, got %+v", p)
	}

	backups, err := filepath.Glob(path + ".corrupt-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected exactly 1 corrupt backup, got %d (glob err %v)", len(backups), err)
	}
	data, err := os.ReadFile(backups[0])
	if err != nil || string(data) != corrupt {
		t.Fatalf("backup content = %q, err %v; want original %q", data, err, corrupt)
	}

	savePool()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fresh pool file missing after save: %v", err)
	}
	data, err = os.ReadFile(backups[0])
	if err != nil || string(data) != corrupt {
		t.Fatalf("backup modified after save: %q, err %v", data, err)
	}
}

// TestPoolPersistenceRoundTrip 添加账号 → 落盘 → 丢弃内存缓存重新加载（模拟重启），数据完好。
func TestPoolPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".cline-accounts.json")
	usePoolPath(t, path)

	addAccount(&Account{AccountID: "acc-1", Email: "a@b.c", RefreshToken: "rt-1", Status: "active"})

	pool = nil // 模拟进程重启
	p := loadPool()
	if len(p.Accounts) != 1 || p.Accounts[0].AccountID != "acc-1" || p.Accounts[0].RefreshToken != "rt-1" {
		t.Fatalf("round-trip account mismatch: %+v", p.Accounts)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pool file: %v", err)
	}
	var check AccountPool
	if err := json.Unmarshal(data, &check); err != nil {
		t.Fatalf("pool file is not valid JSON: %v", err)
	}
	if len(check.Accounts) != 1 {
		t.Fatalf("pool file on disk has %d accounts, want 1", len(check.Accounts))
	}
}

// TestFindCredentialsFileEnv 凭据文件路径与数据目录环境变量保持一致。
func TestFindCredentialsFileEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(dataDirEnv, dir)
	if got := findCredentialsFile(); got != filepath.Join(dir, ".cline-credentials.json") {
		t.Fatalf("findCredentialsFile = %q, want %q", got, filepath.Join(dir, ".cline-credentials.json"))
	}
}
