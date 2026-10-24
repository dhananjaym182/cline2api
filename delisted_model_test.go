package main

import (
	"strings"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// isModelGoneError：仅 400/404 且响应体含「模型不存在」类文案才算，
// 429 限流 / 5xx 瞬时错误不算 —— 不能凭单次请求误删。
func TestIsModelGoneError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"model-not-found-400", http.StatusBadRequest, `{"error":{"message":"Model not found: z-ai/glm-5.3-flash"}}`, true},
		{"unknown-model-404", http.StatusNotFound, "no such model", true},
		{"invalid-model-400", http.StatusBadRequest, "invalid model id", true},
		{"rate-limited-429", http.StatusTooManyRequests, "model not found", false},
		{"server-error-500", http.StatusInternalServerError, "model not found", false},
		{"unrelated-400", http.StatusBadRequest, `{"error":"boom"}`, false},
	}
	for _, c := range cases {
		if got := isModelGoneError(c.status, c.body); got != c.want {
			t.Errorf("%s: isModelGoneError(%d, %q) = %v, want %v", c.name, c.status, c.body, got, c.want)
		}
	}
}

// markModelGone：只删同步打上 Delisted 标记的模型；仍在官方列表里的和
// 自定义模型不动；删除的是默认模型时清空回退。
func TestMarkModelGoneRemovesOnlyDelisted(t *testing.T) {
	oldPool, oldPath := pool, poolPath
	t.Cleanup(func() { pool, poolPath = oldPool, oldPath })
	poolPath = filepath.Join(t.TempDir(), ".cline-accounts.json")
	pool = &AccountPool{
		Accounts:     []*Account{},
		Keys:         []string{},
		DefaultModel: "delisted-model",
		Models: []Model{
			{ID: "delisted-model", Source: "remote", Delisted: true},
			{ID: "live-model", Source: "remote"},
			{ID: "my-custom", Source: "remote", Delisted: true, Custom: true},
		},
	}

	markModelGone("delisted-model")
	markModelGone("live-model")
	markModelGone("my-custom")
	markModelGone("unknown-model")
	markModelGone("")

	models := map[string]bool{}
	for _, m := range loadPool().Models {
		models[m.ID] = true
	}
	if models["delisted-model"] {
		t.Error("delisted model should be removed after upstream reports it gone")
	}
	if !models["live-model"] {
		t.Error("model still in the official list must not be removed")
	}
	if !models["my-custom"] {
		t.Error("custom model must be left for manual handling")
	}
	if got := loadPool().DefaultModel; got != "" {
		t.Errorf("DefaultModel = %q, want empty after its model was removed", got)
	}
}

// syncClineModels：官方列表里消失的模型不删除，改打 Delisted 标记保留；
// 重新出现时标记清除。res.Removed 只记录新下架的。
func TestSyncClineModelsMarksDelisted(t *testing.T) {
	oldPool, oldPath, oldURL := pool, poolPath, clineRecommendedModelsURL
	oldSyncRan := modelSyncRan
	oldLast := lastModelSync
	t.Cleanup(func() {
		pool, poolPath, clineRecommendedModelsURL = oldPool, oldPath, oldURL
		modelSyncMu.Lock()
		modelSyncRan, lastModelSync = oldSyncRan, oldLast
		modelSyncMu.Unlock()
	})
	poolPath = filepath.Join(t.TempDir(), ".cline-accounts.json")
	pool = &AccountPool{Accounts: []*Account{}, Keys: []string{}, Models: []Model{
		{ID: "old-free/gone-model", Provider: "old-free", Cost: "free", Status: "active", Source: "remote"},
	}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// gone-model 不在最新列表里；fresh-model 是新上线的
		w.Write([]byte(`{"free":[{"id":"new-free/fresh-model","tags":["FREE"]}]}`))
	}))
	defer srv.Close()
	clineRecommendedModelsURL = srv.URL

	res := syncClineModels()
	if res.Error != "" {
		t.Fatalf("sync error: %s", res.Error)
	}

	byID := map[string]Model{}
	for _, m := range loadPool().Models {
		byID[m.ID] = m
	}
	gone, ok := byID["old-free/gone-model"]
	if !ok {
		t.Fatal("model missing from the official list must be kept in the pool")
	}
	if !gone.Delisted {
		t.Error("kept model should be marked Delisted")
	}
	if gone.Cost != "free" || gone.Source != "remote" {
		t.Errorf("kept model meta changed: cost=%s source=%s", gone.Cost, gone.Source)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "old-free/gone-model" {
		t.Errorf("res.Removed = %v, want [old-free/gone-model]", res.Removed)
	}
	if _, ok := byID["new-free/fresh-model"]; !ok {
		t.Error("new model from the list should be added")
	} else if got := byID["new-free/fresh-model"].Context; got != 1048576 {
		t.Errorf("new model default context = %d, want 1048576 (1M)", got)
	}

	// 第二次同步：模型重新回到官方列表 → 标记清除
	srv2Body := `{"free":[{"id":"old-free/gone-model","tags":["FREE"]},{"id":"new-free/fresh-model","tags":["FREE"]}]}`
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(srv2Body))
	}))
	defer srv2.Close()
	clineRecommendedModelsURL = srv2.URL

	res2 := syncClineModels()
	if res2.Error != "" {
		t.Fatalf("sync2 error: %s", res2.Error)
	}
	for _, m := range loadPool().Models {
		if m.ID == "old-free/gone-model" && m.Delisted {
			t.Error("model back in the official list should clear the Delisted flag")
		}
	}
	if len(res2.Removed) != 0 {
		t.Errorf("res2.Removed = %v, want empty (model revived)", res2.Removed)
	}
}

// 用户显式配置的回退链不做列表过滤：列表外但实测仍可用的模型允许配置；
// 动态派生的免费链仍按 liveModelSet 过滤下架条目。
func TestModelFallbackChainKeepsUserConfiguredOutOfList(t *testing.T) {
	oldPool, oldPath := pool, poolPath
	oldCfg := getProxyConfig()
	t.Cleanup(func() {
		pool, poolPath = oldPool, oldPath
		setProxyConfig(oldCfg)
	})
	poolPath = filepath.Join(t.TempDir(), ".cline-accounts.json")
	pool = &AccountPool{Accounts: []*Account{}, Keys: []string{}, Models: []Model{
		{ID: "cline-free/gemini-3.8-flash", Source: "remote", Cost: "free", Status: "active"},
	}}
	setProxyConfig(&proxyConfigData{ModelChain: []string{"out-of-list/still-works", "cline-free/gemini-3.8-flash", "out-of-list/still-works"}})

	chain := modelFallbackChain("requested-model")
	got := strings.Join(chain, ",")
	if !strings.Contains(got, "out-of-list/still-works") {
		t.Errorf("user-configured out-of-list model must be kept in the chain, got: %s", got)
	}
	if strings.Count(got, "out-of-list/still-works") != 1 {
		t.Errorf("chain should be deduped, got: %s", got)
	}
	if chain[0] != "requested-model" {
		t.Errorf("requested model must stay first, got: %s", got)
	}
}

// addAccount 按邮箱去重：同邮箱原地更新凭据（保留 AccountID/统计），不产生
// 重复条目；已存在的同邮箱重复条目合并移除。
func TestAddAccountUpsertsByEmail(t *testing.T) {
	oldPool, oldPath := pool, poolPath
	t.Cleanup(func() { pool, poolPath = oldPool, oldPath })
	poolPath = filepath.Join(t.TempDir(), ".cline-accounts.json")
	oldAcc := &Account{AccountID: "acc_old", Email: "user@example.com", RefreshToken: "old-rt", Status: "expired", UsageCount: 100, TotalTokens: 5000, CreatedAt: time.Now().Add(-72 * time.Hour)}
	pool = &AccountPool{Accounts: []*Account{oldAcc}, Keys: []string{}}

	// 同邮箱重新授权：原地更新，不新增
	addAccount(&Account{AccountID: "acc_new", Email: "user@example.com", RefreshToken: "new-rt", Status: "active", UsageCount: 5})

	p := loadPool()
	if len(p.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1 (in-place re-auth, no duplicate)", len(p.Accounts))
	}
	got := p.Accounts[0]
	if got.AccountID != "acc_old" {
		t.Errorf("AccountID = %q, want original preserved", got.AccountID)
	}
	if got.Status != "active" || got.RefreshToken != "new-rt" {
		t.Errorf("credentials not updated: status=%s rt=%s", got.Status, got.RefreshToken)
	}
	if got.UsageCount != 100 {
		t.Errorf("UsageCount = %d, want original 100 preserved", got.UsageCount)
	}

	// 已存在的重复条目：addAccount 合并移除
	dup := &Account{AccountID: "acc_dup", Email: "USER@example.com", RefreshToken: "dup-rt", Status: "active", UsageCount: 7}
	pool.Accounts = append(pool.Accounts, dup)
	addAccount(&Account{AccountID: "acc_x", Email: "user@example.com", RefreshToken: "latest-rt", Status: "active"})

	p = loadPool()
	if len(p.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1 (duplicates merged)", len(p.Accounts))
	}
	got = p.Accounts[0]
	if got.UsageCount != 107 {
		t.Errorf("UsageCount = %d, want 107 (100 + 7 merged)", got.UsageCount)
	}
	if got.RefreshToken != "latest-rt" {
		t.Errorf("RefreshToken = %q, want latest-rt", got.RefreshToken)
	}
}
