package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// newToggleTestPool 建立一个独立数据目录的测试池（不落盘到真实数据目录）。
func newToggleTestPool(t *testing.T, models []Model, defaultModel string) {
	t.Helper()
	oldPool, oldPath := pool, poolPath
	t.Cleanup(func() { pool, poolPath = oldPool, oldPath })
	poolPath = filepath.Join(t.TempDir(), ".cline-accounts.json")
	pool = &AccountPool{Accounts: []*Account{}, Keys: []string{}, Models: models, DefaultModel: defaultModel}
}

// handleAdminModelToggle：批量启停模型；停用当前默认模型时清空默认；
// 未知模型 404，空 ids 400，非 POST 405。
func TestHandleAdminModelToggle(t *testing.T) {
	newToggleTestPool(t, []Model{
		{ID: "prov/default-model", Provider: "prov", Cost: "free", Status: "active", Source: "remote"},
		{ID: "prov/other-model", Provider: "prov", Cost: "pass", Status: "active", Source: "remote"},
		{ID: "my-custom", Provider: "custom", Cost: "pass", Status: "active", Custom: true},
	}, "prov/default-model")

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/admin/api/models/toggle", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handleAdminModelToggle(rec, req)
		return rec
	}

	// 停用默认模型 → 标记 + 清空 DefaultModel
	if rec := post(`{"ids":["prov/default-model"],"disabled":true}`); rec.Code != http.StatusOK {
		t.Fatalf("toggle default-model: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	p := loadPool()
	if !p.Models[0].Disabled {
		t.Error("default-model should be marked Disabled")
	}
	if p.DefaultModel != "" {
		t.Errorf("DefaultModel = %q, want empty after disabling the default model", p.DefaultModel)
	}

	// 批量停用（含自定义模型）
	if rec := post(`{"ids":["prov/other-model","my-custom"],"disabled":true}`); rec.Code != http.StatusOK {
		t.Fatalf("batch toggle: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	p = loadPool()
	if !p.Models[1].Disabled || !p.Models[2].Disabled {
		t.Error("batch toggle should disable both models")
	}

	// 重新启用
	if rec := post(`{"ids":["prov/default-model"],"disabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("re-enable: status = %d", rec.Code)
	}
	if p := loadPool(); p.Models[0].Disabled {
		t.Error("default-model should be re-enabled")
	}

	// 未知模型 → 404
	if rec := post(`{"ids":["nope/missing"],"disabled":true}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown model: status = %d, want 404", rec.Code)
	}
	// 空 ids → 400
	if rec := post(`{"ids":[],"disabled":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty ids: status = %d, want 400", rec.Code)
	}
	// 非 POST → 405
	req := httptest.NewRequest(http.MethodGet, "/admin/api/models/toggle", nil)
	rec := httptest.NewRecorder()
	handleAdminModelToggle(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: status = %d, want 405", rec.Code)
	}
}

// isModelDisabled：按 ID 判定停用状态，支持 "opencode/" 前缀别名。
func TestIsModelDisabled(t *testing.T) {
	newToggleTestPool(t, []Model{
		{ID: "zen/disabled-free", Provider: "opencode", Cost: "free", Status: "active", Source: "zen", Disabled: true},
		{ID: "prov/live-model", Provider: "prov", Cost: "free", Status: "active", Source: "remote"},
	}, "")

	if !isModelDisabled("zen/disabled-free") {
		t.Error("disabled model should be reported as disabled")
	}
	if !isModelDisabled("opencode/zen/disabled-free") {
		t.Error("opencode/ prefixed alias of a disabled model should be reported as disabled")
	}
	if isModelDisabled("prov/live-model") {
		t.Error("enabled model must not be reported as disabled")
	}
	if isModelDisabled("") {
		t.Error("empty model id must not be reported as disabled")
	}
}

// liveModelSet：停用模型不进「在线」集合 → 默认模型校验与回退链过滤自动跳过。
func TestLiveModelSetExcludesDisabled(t *testing.T) {
	newToggleTestPool(t, []Model{
		{ID: "prov/disabled", Provider: "prov", Cost: "free", Status: "active", Source: "remote", Disabled: true},
		{ID: "prov/live", Provider: "prov", Cost: "free", Status: "active", Source: "remote"},
	}, "")
	p := loadPool()
	poolMu.Lock()
	live := liveModelSet(p)
	poolMu.Unlock()
	if live["prov/disabled"] {
		t.Error("disabled model must not be in the live set")
	}
	if !live["prov/live"] {
		t.Error("enabled model should be in the live set")
	}
}

// getDefaultModel：停用的默认模型跳过（含离线模式），停用模型不参与
// 「第一个远程免费模型」回退。
func TestGetDefaultModelSkipsDisabled(t *testing.T) {
	newToggleTestPool(t, []Model{
		{ID: "prov/disabled-free", Provider: "prov", Cost: "free", Status: "active", Source: "remote", Disabled: true},
		{ID: "prov/live-free", Provider: "prov", Cost: "free", Status: "active", Source: "remote"},
	}, "prov/disabled-free")

	if got := getDefaultModel(); got != "prov/live-free" {
		t.Errorf("getDefaultModel() = %q, want prov/live-free (disabled default skipped)", got)
	}

	// 无默认模型时：第一个未停用的远程免费模型
	newToggleTestPool(t, []Model{
		{ID: "prov/disabled-free", Provider: "prov", Cost: "free", Status: "active", Source: "remote", Disabled: true},
		{ID: "prov/live-free", Provider: "prov", Cost: "free", Status: "active", Source: "remote"},
	}, "")
	if got := getDefaultModel(); got != "prov/live-free" {
		t.Errorf("getDefaultModel() = %q, want prov/live-free (disabled model skipped in fallback)", got)
	}
}

// syncClineModels：重建 remote 条目时保留用户停用标记（Disabled），
// 周期同步不会把停用状态冲掉。
func TestSyncClineModelsPreservesDisabled(t *testing.T) {
	oldPool, oldPath, oldURL := pool, poolPath, clineRecommendedModelsURL
	oldSyncRan := modelSyncRan
	oldLast := lastModelSync
	oldBusy := modelSyncBusy
	t.Cleanup(func() {
		pool, poolPath, clineRecommendedModelsURL = oldPool, oldPath, oldURL
		modelSyncMu.Lock()
		modelSyncRan, lastModelSync, modelSyncBusy = oldSyncRan, oldLast, oldBusy
		modelSyncMu.Unlock()
	})
	poolPath = filepath.Join(t.TempDir(), ".cline-accounts.json")
	pool = &AccountPool{Accounts: []*Account{}, Keys: []string{}, Models: []Model{
		{ID: "prov/disabled-model", Provider: "prov", Cost: "free", Status: "active", Source: "remote", Disabled: true},
		{ID: "prov/enabled-model", Provider: "prov", Cost: "free", Status: "active", Source: "remote"},
	}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 两个模型都仍在官方列表里（同步会重建 remote 条目）
		w.Write([]byte(`{"free":[{"id":"prov/disabled-model","tags":["FREE"]},{"id":"prov/enabled-model","tags":["FREE"]}]}`))
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
	if m, ok := byID["prov/disabled-model"]; !ok {
		t.Fatal("disabled model missing after sync")
	} else if !m.Disabled {
		t.Error("Disabled flag must survive a sync that rebuilds the entry")
	}
	if m, ok := byID["prov/enabled-model"]; !ok {
		t.Fatal("enabled model missing after sync")
	} else if m.Disabled {
		t.Error("enabled model must not become disabled by sync")
	}
}
