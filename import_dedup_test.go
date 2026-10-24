package main

import (
	"testing"
	"time"
)

// withEmptyAccounts 清空账号池并在测试结束后恢复，避免测试间相互污染。
func withEmptyAccounts(t *testing.T) {
	t.Helper()
	p := loadPool()
	old := p.Accounts
	p.Accounts = []*Account{}
	savePool()
	t.Cleanup(func() {
		q := loadPool()
		q.Accounts = old
		savePool()
	})
}

func TestFindAccountByRefreshToken(t *testing.T) {
	withEmptyAccounts(t)

	tok := "refresh-token-abc"
	if got := findAccountByRefreshToken(tok); got != nil {
		t.Fatalf("unknown token should not match, got %+v", got)
	}
	if accountExists(tok) {
		t.Fatal("accountExists should be false for unknown token")
	}

	addAccount(&Account{
		AccountID:    "acc_test_1",
		Email:        "a@example.com",
		RefreshToken: tok,
		Status:       "active",
		CreatedAt:    time.Now(),
	})

	if got := findAccountByRefreshToken(tok); got == nil || got.AccountID != "acc_test_1" {
		t.Fatalf("findAccountByRefreshToken(%q) = %+v, want acc_test_1", tok, got)
	}
	// 前后空白应被忽略
	if got := findAccountByRefreshToken("  " + tok + "\t"); got == nil || got.AccountID != "acc_test_1" {
		t.Fatalf("lookup should trim whitespace, got %+v", got)
	}
	// 空 token 永远不应命中
	if accountExists("") || accountExists("   ") {
		t.Fatal("empty token must never match an account")
	}
}

func TestIsDuplicateImportToken(t *testing.T) {
	withEmptyAccounts(t)

	addAccount(&Account{
		AccountID:    "acc_existing",
		Email:        "b@example.com",
		RefreshToken: "existing-token",
		Status:       "active",
		CreatedAt:    time.Now(),
	})

	seen := make(map[string]bool)

	// 账号池中已存在 → 重复
	if !isDuplicateImportToken("existing-token", seen) {
		t.Fatal("token already in pool should be a duplicate")
	}
	// 首次出现 → 不重复，但记入 seen
	if isDuplicateImportToken("new-token", seen) {
		t.Fatal("first occurrence in batch should not be a duplicate")
	}
	// 同批次第二次出现 → 重复
	if !isDuplicateImportToken("new-token", seen) {
		t.Fatal("second occurrence within the same batch should be a duplicate")
	}
	// 未见过的新 token → 不重复
	if isDuplicateImportToken("another-token", seen) {
		t.Fatal("unseen new token should not be a duplicate")
	}
}
