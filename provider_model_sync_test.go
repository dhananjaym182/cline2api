package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
)

// isolateProviderRecheck 把 recheck 的池/状态文件/批次表重置到临时隔离环境。
func isolateProviderRecheck(t *testing.T) {
	t.Helper()
	oldPool, oldPoolPath := pool, poolPath
	oldSyncPath, oldStat := providerSyncFilePath, providerRecheckStat
	oldMiss, oldLoaded := provRecheckMiss, provRecheckLoaded
	oldImports := pendingImports
	poolPath = filepath.Join(t.TempDir(), ".cline-accounts.json")
	providerSyncFilePath = filepath.Join(t.TempDir(), ".cline-providers-sync.json")
	providerRecheckStat = map[string]*providerRecheckState{}
	provRecheckMiss = map[string]map[string]int{}
	provRecheckLoaded = true
	pendingImports = map[string]*importBatch{}
	t.Cleanup(func() {
		pool, poolPath = oldPool, oldPoolPath
		providerSyncFilePath, providerRecheckStat = oldSyncPath, oldStat
		provRecheckMiss, provRecheckLoaded = oldMiss, oldLoaded
		pendingImports = oldImports
	})
}

type mutableUpstream struct {
	mu   sync.Mutex
	ids  []string
	paid bool // false=pricing 全 0；true=pricing 非 0
}

func (u *mutableUpstream) set(ids []string) {
	u.mu.Lock()
	u.ids = ids
	u.mu.Unlock()
}

func (u *mutableUpstream) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		ids := append([]string(nil), u.ids...)
		paid := u.paid
		u.mu.Unlock()
		type item struct {
			ID      string  `json:"id"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		}
		out := struct {
			Data []item `json:"data"`
		}{}
		for _, id := range ids {
			it := item{ID: id}
			pr := "0"
			if paid {
				pr = "0.005"
			}
			it.Pricing.Prompt, it.Pricing.Completion = pr, pr
			out.Data = append(out.Data, it)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}))
}

func poolModelByID(id string) (Model, bool) {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	for _, m := range p.Models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// 连续缺失 2 次才下架；重新出现撤标记；新上游模型只入 Auto 批次不静默导入。
func TestProviderRecheckDelistCycleAndAutoBatch(t *testing.T) {
	isolateProviderRecheck(t)
	up := &mutableUpstream{}
	srv := up.server()
	defer srv.Close()

	pool = &AccountPool{Accounts: []*Account{}, Models: []Model{
		{ID: "a", Provider: "TestProv", Cost: "free", Status: "active", Source: "provider"},
		{ID: "b", Provider: "TestProv", Cost: "free", Status: "active", Source: "provider"},
	}}
	p := CustomProvider{ID: "prov1", Name: "TestProv", BaseURL: srv.URL, ModelIDs: []string{"a", "b"}, Enabled: true}

	up.set([]string{"a", "b"})
	st := recheckProviderModels(p)
	if st.LastError != "" {
		t.Fatalf("cycle0 error: %s", st.LastError)
	}
	if m, _ := poolModelByID("b"); m.Delisted {
		t.Error("full list must not mark anything")
	}

	// 第 1 次缺失：只计数，不下架（防单次抖动误标）
	up.set([]string{"a"})
	st = recheckProviderModels(p)
	if m, _ := poolModelByID("b"); m.Delisted {
		t.Error("single missed check must not delist")
	}
	if st.MissCounting != 1 {
		t.Errorf("MissCounting = %d, want 1", st.MissCounting)
	}

	// 第 2 次缺失：打 Delisted（条目保留不删）
	recheckProviderModels(p)
	m, ok := poolModelByID("b")
	if !ok {
		t.Fatal("delisted model must be kept in pool")
	}
	if !m.Delisted {
		t.Error("second consecutive miss should mark Delisted")
	}
	if st := getProviderRecheckStateForTest("prov1"); st.DelistedMarked != 1 {
		t.Errorf("DelistedMarked = %d, want 1", st.DelistedMarked)
	}

	// 重新出现：标记自动清除
	up.set([]string{"a", "b"})
	recheckProviderModels(p)
	if m, _ := poolModelByID("b"); m.Delisted {
		t.Error("model back upstream should clear Delisted")
	}

	// 新上游模型 → Auto 批次入队（action=add，pricing 0 → free），不落池
	up.set([]string{"a", "b", "new-free-c"})
	st = recheckProviderModels(p)
	if st.NewQueued != 1 || st.BatchID == "" {
		t.Fatalf("want 1 queued + batchId, got queued=%d batch=%q", st.NewQueued, st.BatchID)
	}
	if _, ok := poolModelByID("new-free-c"); ok {
		t.Error("new model must NOT be imported silently")
	}
	importsMu.Lock()
	b := pendingImports[st.BatchID]
	importsMu.Unlock()
	if b == nil || !b.Auto {
		t.Fatal("auto batch missing")
	}
	if len(b.Items) != 1 || b.Items[0].ModelID != "new-free-c" || b.Items[0].Action != "add" {
		t.Fatalf("batch items = %+v", b.Items)
	}
	if b.Items[0].Tier != "free" || b.Items[0].Signal != tierSignalPricingZero {
		t.Errorf("tier = %s/%s, want free/pricing-zero", b.Items[0].Tier, b.Items[0].Signal)
	}

	// 下一轮同样有新模型：Auto 批次被刷新替换，而不是堆积
	up.set([]string{"a", "b", "new-free-d"})
	st2 := recheckProviderModels(p)
	importsMu.Lock()
	batches := 0
	var ids []string
	for id := range pendingImports {
		batches++
		ids = append(ids, id)
	}
	importsMu.Unlock()
	if batches != 1 || st2.BatchID == st.BatchID || st2.BatchID != ids[0] {
		t.Errorf("auto batch should be replaced, got %d batches: %v", batches, ids)
	}
}

// 付费新模型绝不猜免费：pricing 非 0 时归 pass/pricing-paid。
func TestProviderRecheckNewModelPaidDefault(t *testing.T) {
	isolateProviderRecheck(t)
	up := &mutableUpstream{paid: true}
	srv := up.server()
	defer srv.Close()
	pool = &AccountPool{Accounts: []*Account{}, Models: []Model{}}
	p := CustomProvider{ID: "provP", Name: "PaidProv", BaseURL: srv.URL, Enabled: true}
	up.set([]string{"claude-opus-x"})
	st := recheckProviderModels(p)
	importsMu.Lock()
	b := pendingImports[st.BatchID]
	importsMu.Unlock()
	if st.NewQueued != 1 || b == nil {
		t.Fatalf("want queued batch, got %+v", st)
	}
	if b.Items[0].Tier != "pass" || b.Items[0].Signal != tierSignalPricingPaid {
		t.Errorf("tier = %s/%s, want pass/pricing-paid", b.Items[0].Tier, b.Items[0].Signal)
	}
}

// 连接不可用：记录错误、不动缺失计数、不改池。
func TestProviderRecheckConnectionUnavailable(t *testing.T) {
	isolateProviderRecheck(t)
	oldConns, oldConnsLoaded := connections, connectionsLoaded
	connections, connectionsLoaded = nil, true
	t.Cleanup(func() { connections, connectionsLoaded = oldConns, oldConnsLoaded })

	pool = &AccountPool{Accounts: []*Account{}, Models: []Model{
		{ID: "a", Provider: "ConnProv", Status: "active", Source: "provider"},
	}}
	p := CustomProvider{ID: "provC", Name: "ConnProv", BaseURL: "http://127.0.0.1:1", ModelIDs: []string{"a"}, ConnectionID: "missing-conn", Enabled: true}
	st := recheckProviderModels(p)
	if st.LastError == "" {
		t.Error("want connection unavailable error recorded")
	}
	if len(provRecheckMiss["provC"]) != 0 {
		t.Error("failed check must not bump miss counters")
	}
	if m, _ := poolModelByID("a"); m.Delisted {
		t.Error("pool must be untouched on failed check")
	}
	// 失败轮不刷新 LastCheckedMs
	if getProviderRecheckStateForTest("provC").LastCheckedMs != 0 {
		t.Error("failed round should not set lastCheckedMs")
	}
}

// syncProviderModels 刷新 provider 条目时保留 Delisted 标记（下架≠删除的存续前提）。
func TestSyncProviderModelsPreservesDelisted(t *testing.T) {
	oldPool, oldPoolPath := pool, poolPath
	poolPath = filepath.Join(t.TempDir(), ".cline-accounts.json")
	t.Cleanup(func() { pool, poolPath = oldPool, oldPoolPath })
	installProviders(t, &CustomProvider{ID: "provS", Name: "SyncProv", ModelIDs: []string{"a", "b"}, Enabled: true, Free: true})
	pool = &AccountPool{Accounts: []*Account{}, Models: []Model{
		{ID: "a", Provider: "SyncProv", Cost: "free", Status: "active", Source: "provider", Delisted: true},
	}}
	syncProviderModels()
	m, ok := poolModelByID("a")
	if !ok || !m.Delisted {
		t.Errorf("Delisted flag must survive syncProviderModels, got %+v ok=%v", m, ok)
	}
	if _, ok := poolModelByID("b"); !ok {
		t.Error("declared model should be (re)added")
	}
}

func getProviderRecheckStateForTest(id string) providerRecheckState {
	provRecheckMu.Lock()
	defer provRecheckMu.Unlock()
	if st, ok := providerRecheckStat[id]; ok {
		return *st
	}
	return providerRecheckState{}
}
