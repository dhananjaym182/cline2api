package main

// ============================================================================
// §W3-C 已启用 Provider 模型清单后台自动检测（对齐 Cline/zen 同步语义）：
// 周期性拉取每个启用 provider 的上游 /models——
//   - provider 声明且池内有条目、上游连续 2 次缺失 → 打 Delisted 标记（不删；
//     实测下架后往往仍可用，回退链照常尝试，直到上游明确报「模型不存在」由
//     markModelGone 清理）；重新出现时标记自动清除。
//   - 上游新增模型 → 绝不静默导入：分类后放入 §9.3 预览批次队列（Auto 批次，
//     每 provider 至多一个，重复检测时刷新），管理员勾选批准后才写入池。
// 展示状态（最近检测/错误/计数）落盘 .cline-providers-sync.json；连续缺失
// 计数只存内存——重启后重新数满 2 次才下架，天然防单次抖动误标。
// ============================================================================

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"cline-go-proxy/internal/modelcatalog"
	"cline-go-proxy/internal/store/syncstate"
)

const (
	providerRecheckInterval  = 6 * time.Hour
	providerRecheckMissLimit = 2 // 连续缺失次数阈值
	providerRecheckBootDelay = 2 * time.Minute
	providerRecheckGap       = 2 * time.Second // 相邻 provider 检测间隔，避免同时打同一上游
)

type providerRecheckState = syncstate.State

var (
	provRecheckMu        sync.Mutex
	providerRecheckStat  = map[string]*providerRecheckState{} // providerID → 展示状态
	provRecheckMiss      = map[string]map[string]int{}        // providerID → modelID → 连续缺失次数（内存）
	provRecheckLoaded    bool
	provRecheckBusy      bool
	providerSyncFilePath = ""
)

func init() { providerSyncFilePath = resolveDataPath(syncstate.FileName) }

func loadProviderRecheckState() {
	provRecheckMu.Lock()
	defer provRecheckMu.Unlock()
	if provRecheckLoaded {
		return
	}
	provRecheckLoaded = true
	persisted, err := syncstate.Load(providerSyncFilePath)
	if err != nil {
		backupCorruptFile(providerSyncFilePath, err)
		return
	}
	for id, st := range persisted {
		if st != nil {
			providerRecheckStat[id] = st
		}
	}
}

func saveProviderRecheckStateLocked() {
	out := make(map[string]providerRecheckState, len(providerRecheckStat))
	for id, st := range providerRecheckStat {
		out[id] = *st
	}
	if err := syncstate.Save(providerSyncFilePath, out); err != nil {
		log.Printf("provider sync state save failed: %v", err)
	}
}

// recheckCreds 检测用的只读凭据，与 callProvider 的连接注入逻辑同源：
// APIKey/自定义头 + 绑定连接时的连接 token（含刷新/兑换）与动态组织头。
// 绑定了连接但连接不可用（过期/需重登）返回 ok=false，跳过本轮检测。
func recheckCreds(p *CustomProvider) (apiKey string, headers map[string]string, ok bool) {
	headers = map[string]string{}
	for k, v := range p.Headers {
		if v != "" {
			headers[k] = v
		}
	}
	apiKey = p.APIKey
	if ids := providerConnIDs(p); len(ids) == 0 {
		return apiKey, headers, true
	}
	// Any pooled connection works for catalog reads; the picker rotates.
	conn := pickConnectionForProvider(p)
	if conn == nil {
		return "", nil, false
	}
	if spec, hasSpec := providerDeviceFlows[conn.Provider]; hasSpec {
		apiKey = connectionBearer(conn, spec)
		if spec.OrgHeader != "" {
			if oid := conn.ProviderSpecificData["orgId"]; oid != "" {
				headers[spec.OrgHeader] = oid
			}
		}
	} else {
		apiKey = conn.AccessToken
	}
	return apiKey, headers, true
}

// recheckProviderModels 对单个 provider 检测一轮：下架标记 + 新模型入队。
// 返回本轮展示状态副本。上游失败只记 error，不动缺失计数与标记。
func recheckProviderModels(p CustomProvider) providerRecheckState {
	loadProviderRecheckState()
	st := providerRecheckState{}
	apiKey, headers, ok := recheckCreds(&p)
	if !ok {
		st.LastError = "connection unavailable (expired or reauth required)"
		setProviderRecheckState(p.ID, st)
		return st
	}
	entries, err := discoverModelEntries(p.BaseURL, apiKey, headers)
	if err != nil {
		st.LastError = err.Error()
		setProviderRecheckState(p.ID, st)
		return st
	}

	upstream := make(map[string]bool, len(entries))
	for _, e := range entries {
		upstream[e.ID] = true
	}
	declared := make(map[string]bool, len(p.ModelIDs))
	for _, id := range p.ModelIDs {
		if id != "" {
			declared[id] = true
		}
	}

	// 1) 声明模型的缺失计数（连续 2 次才下架；出现即清零并撤标记）
	provRecheckMu.Lock()
	miss := provRecheckMiss[p.ID]
	if miss == nil {
		miss = map[string]int{}
		provRecheckMiss[p.ID] = miss
	}
	toMark, counting := modelcatalog.TrackMissing(p.ModelIDs, upstream, miss, providerRecheckMissLimit)
	provRecheckMu.Unlock()

	// 2) 写池：打/撤 Delisted（只动 Source=provider 且在本 provider 声明内的条目）
	marked := 0
	pool := loadPool()
	poolMu.Lock()
	for i := range pool.Models {
		m := &pool.Models[i]
		if m.Source != "provider" || !declared[m.ID] {
			continue
		}
		if containsStr(toMark, m.ID) {
			if !m.Delisted {
				m.Delisted = true
				marked++
			}
		} else if upstream[m.ID] && m.Delisted {
			m.Delisted = false
		}
	}
	alreadyMarked := 0
	for _, m := range pool.Models {
		if m.Source == "provider" && m.Delisted && declared[m.ID] {
			alreadyMarked++
		}
	}
	poolMu.Unlock()
	if marked > 0 {
		savePool()
	}

	// 3) 新上游模型 → Auto 预览批次（不在声明里也不在池里；每 provider 至多一个待审批次）
	poolHas := map[string]bool{}
	poolMu.Lock()
	for _, m := range pool.Models {
		poolHas[m.ID] = true
	}
	poolMu.Unlock()
	fresh := modelcatalog.FilterNew(entries, declared, poolHas)
	batchID, queued := "", 0
	if len(fresh) > 0 {
		importsMu.Lock()
		for id, old := range pendingImports {
			if old.Auto && old.ProviderID == p.ID {
				delete(pendingImports, id)
			}
		}
		importsMu.Unlock()
		batch := buildImportBatch(p, fresh)
		batch.Auto = true
		for _, it := range batch.Items {
			if it.Action == "add" {
				queued++
			}
		}
		if queued == 0 {
			importsMu.Lock()
			delete(pendingImports, batch.ID)
			importsMu.Unlock()
		} else {
			batchID = batch.ID
		}
	}

	st = providerRecheckState{
		LastCheckedMs:  time.Now().UnixMilli(),
		DelistedMarked: alreadyMarked,
		MissCounting:   counting,
		NewQueued:      queued,
		BatchID:        batchID,
	}
	setProviderRecheckState(p.ID, st)
	if marked > 0 || queued > 0 {
		log.Printf("provider recheck %s (%s): +%d delisted-marked, %d new queued for review", p.ID, p.Name, marked, queued)
	}
	return st
}

func setProviderRecheckState(providerID string, st providerRecheckState) {
	loadProviderRecheckState()
	cp := st
	provRecheckMu.Lock()
	if prev, ok := providerRecheckStat[providerID]; ok && st.LastCheckedMs == 0 {
		cp.LastCheckedMs = prev.LastCheckedMs // 失败轮保留上次成功时间
	}
	providerRecheckStat[providerID] = &cp
	saveProviderRecheckStateLocked()
	provRecheckMu.Unlock()
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// clearProviderRecheck provider 删除时清掉其检测状态与待审 Auto 批次。
func clearProviderRecheck(providerID string) {
	provRecheckMu.Lock()
	delete(providerRecheckStat, providerID)
	delete(provRecheckMiss, providerID)
	saveProviderRecheckStateLocked()
	provRecheckMu.Unlock()
	importsMu.Lock()
	for id, b := range pendingImports {
		if b.Auto && b.ProviderID == providerID {
			delete(pendingImports, id)
		}
	}
	importsMu.Unlock()
}

// runAllProviderRechecks 对全部启用 provider 顺序检测一轮（单飞：并发触发时后来的直接跳过）。
func runAllProviderRechecks() map[string]providerRecheckState {
	loadProviderRecheckState()
	provRecheckMu.Lock()
	if provRecheckBusy {
		provRecheckMu.Unlock()
		return nil
	}
	provRecheckBusy = true
	provRecheckMu.Unlock()
	defer func() {
		provRecheckMu.Lock()
		provRecheckBusy = false
		provRecheckMu.Unlock()
	}()

	out := map[string]providerRecheckState{}
	for _, p := range listProviders() {
		if !p.Enabled {
			continue
		}
		out[p.ID] = recheckProviderModels(p)
		time.Sleep(providerRecheckGap)
	}
	return out
}

// recheckOneProvider 手动触发单个 provider 检测。
func recheckOneProvider(providerID string) (providerRecheckState, error) {
	for _, p := range listProviders() {
		if p.ID == providerID {
			return recheckProviderModels(p), nil
		}
	}
	return providerRecheckState{}, fmt.Errorf("provider not found")
}

// startProviderModelRecheck 启动后台检测循环：开机延迟一轮（避开首启模型同步抢带宽），
// 之后 6h + 随机抖动周期执行。
func startProviderModelRecheck() {
	go func() {
		time.Sleep(providerRecheckBootDelay + time.Duration(rand.Intn(90))*time.Second)
		runAllProviderRechecks()
		for {
			interval := providerRecheckInterval + time.Duration(rand.Intn(45))*time.Minute
			time.Sleep(interval)
			runAllProviderRechecks()
		}
	}()
}

// ---------- admin 端点 ----------

// POST /admin/api/providers/recheck  body: {id} 或 {all:true}
// 手动触发检测（同步执行返回摘要）。all 时串行检测全部启用 provider。
// POST /admin/api/providers/recheck  body: {id} 或 {all:true}
// 手动触发检测（同步执行返回摘要）。all 时串行检测全部启用 provider。
func handleProviderRecheck(w http.ResponseWriter, r *http.Request) {
	adminSync.ProviderRecheck(w, r)
}

// GET /admin/api/providers/sync-status — 各 provider 最近检测状态（含从未检测的启用 provider）。
func handleProviderSyncStatus(w http.ResponseWriter, r *http.Request) {
	adminSync.ProviderStatus(w, r)
}
