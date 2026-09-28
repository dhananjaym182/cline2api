package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
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
