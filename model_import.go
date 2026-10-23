package main

// ============================================================================
// §9.3 受控模型导入：拉取上游 /models → 有序信号分类每个模型的 free/paid →
// 生成预览批次（preview）→ 管理员勾选批准（apply）后才写入池与 provider。
// 核心护栏：未匹配任何免费信号的模型一律归为 paid，绝不猜免费；已有模型
// 的层级变化只以 diff 呈现，remote/zen/manual 归属的条目永远 protected。
// ============================================================================

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// 分类信号（写入 Model.TierSignal，管理页可见「为什么免费」并支持手动覆盖为 "manual"）
const (
	tierSignalPricingZero  = "pricing-zero"  // 上游 pricing.prompt/completion 均为 0
	tierSignalSuffixFree   = "suffix-free"   // id 以 :free / -free 结尾
	tierSignalAllowlist    = "allowlist"     // tags 含 free，或命中内置免费种子白名单
	tierSignalProviderFree = "provider-free" // provider 整源免费（Free=true / 免密钥源）
	tierSignalPaidDefault  = "paid-default"  // 无任何免费信号 → paid，不猜测
)

// upstreamModelEntry 是上游 /models 单条目的解析结果（OpenAI 列表 + OpenRouter 风格定价）。
type upstreamModelEntry struct {
	ID             string
	PricingPrompt  *float64
	PricingComplet *float64
	Tags           []string
}

// freeIDAllowlist 内置免费模型 id 白名单（zen 种子表 + 别名），随包初始化。
var freeIDAllowlist = func() map[string]bool {
	m := map[string]bool{}
	for _, sm := range zenSeedModels {
		m[strings.ToLower(sm.ID)] = true
		for _, al := range sm.Aliases {
			m[strings.ToLower(al)] = true
		}
	}
	return m
}()

// classifyUpstreamModel 有序信号、首个命中即返回；providerFree=整源免费（含免密钥 preset）。
func classifyUpstreamModel(e upstreamModelEntry, providerFree bool) (tier, signal string) {
	if e.PricingPrompt != nil && e.PricingComplet != nil && *e.PricingPrompt == 0 && *e.PricingComplet == 0 {
		return "free", tierSignalPricingZero
	}
	lower := strings.ToLower(e.ID)
	if strings.HasSuffix(lower, ":free") || strings.HasSuffix(lower, "-free") {
		return "free", tierSignalSuffixFree
	}
	for _, tag := range e.Tags {
		if strings.EqualFold(strings.TrimSpace(tag), "free") {
			return "free", tierSignalAllowlist
		}
	}
	if freeIDAllowlist[lower] {
		return "free", tierSignalAllowlist
	}
	if providerFree {
		return "free", tierSignalProviderFree
	}
	return "pass", tierSignalPaidDefault
}

// discoverModelEntries 拉取 {baseURL}/models（404 时补试 /v1/models），解析 id + 定价 + 标签。
func discoverModelEntries(baseURL, apiKey string, headers map[string]string) ([]upstreamModelEntry, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("baseURL is empty")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	rawNum := func(v any) *float64 {
		switch n := v.(type) {
		case float64:
			return &n
		case string:
			var f float64
			if _, err := fmt.Sscanf(strings.TrimSpace(n), "%g", &f); err == nil {
				return &f
			}
		}
		return nil
	}
	parse := func(body []byte) ([]upstreamModelEntry, error) {
		var payload struct {
			Data   []map[string]any `json:"data"`
			Models []map[string]any `json:"models"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("unexpected response format")
		}
		var out []upstreamModelEntry
		seen := map[string]bool{}
		for _, list := range [][]map[string]any{payload.Data, payload.Models} {
			for _, it := range list {
				id, _ := it["id"].(string)
				id = strings.TrimSpace(id)
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				e := upstreamModelEntry{ID: id}
				if pr, ok := it["pricing"].(map[string]any); ok {
					e.PricingPrompt = rawNum(pr["prompt"])
					e.PricingComplet = rawNum(pr["completion"])
				}
				if tags, ok := it["tags"].([]any); ok {
					for _, t := range tags {
						if s, ok := t.(string); ok {
							e.Tags = append(e.Tags, s)
						}
					}
				}
				out = append(out, e)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("upstream returned no models")
		}
		return out, nil
	}
	fetch := func(u string) (int, []byte, error) {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Accept", "application/json")
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		for k, v := range headers {
			if strings.TrimSpace(v) != "" {
				req.Header.Set(strings.TrimSpace(k), v)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		return resp.StatusCode, body, nil
	}
	status, body, err := fetch(baseURL + "/models")
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	if status == http.StatusNotFound {
		s2, b2, e2 := fetch(baseURL + "/v1/models")
		if e2 == nil && s2 == http.StatusOK {
			status, body = s2, b2
		}
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(strings.TrimSpace(string(body)), 200))
	}
	entries, err := parse(body)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

// ---------- 预览批次 ----------

type importItem struct {
	ModelID string `json:"modelId"`
	Tier    string `json:"tier"`
	Signal  string `json:"signal"`
	Action  string `json:"action"` // add | tier_change | nochange | protected
	OldTier string `json:"oldTier,omitempty"`
	Reason  string `json:"reason,omitempty"` // protected 的原因
}

type importBatch struct {
	ID           string       `json:"id"`
	ProviderID   string       `json:"providerId"`
	ProviderName string       `json:"providerName"`
	CreatedAt    time.Time    `json:"createdAt"`
	Items        []importItem `json:"items"`
}

var (
	importsMu      sync.Mutex
	pendingImports = map[string]*importBatch{}
)

func newImportBatchID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

// buildImportBatch 对条目分类并对比当前池，产出仅内存的待批准批次。
func buildImportBatch(p CustomProvider, entries []upstreamModelEntry) *importBatch {
	batch := &importBatch{ID: newImportBatchID(), ProviderID: p.ID, ProviderName: p.Name, CreatedAt: time.Now()}
	pool := loadPool()
	poolMu.Lock()
	existing := make(map[string]*Model, len(pool.Models))
	for i := range pool.Models {
		existing[pool.Models[i].ID] = &pool.Models[i]
	}
	poolMu.Unlock()
	for _, e := range entries {
		tier, signal := classifyUpstreamModel(e, p.Free)
		item := importItem{ModelID: e.ID, Tier: tier, Signal: signal, Action: "add"}
		if m, ok := existing[e.ID]; ok {
			switch {
			case m.Source != "" && m.Source != "provider":
				item.Action, item.Reason = "protected", "模型归属 "+m.Source+" 同步链，不跨源改层级"
			case m.TierSignal == "manual":
				item.Action, item.Reason = "protected", "用户手动设置过层级"
			case m.Cost == tier:
				item.Action = "nochange"
			default:
				item.Action, item.OldTier = "tier_change", m.Cost
			}
		}
		batch.Items = append(batch.Items, item)
	}
	importsMu.Lock()
	for id, old := range pendingImports {
		if len(pendingImports) >= 20 && time.Since(old.CreatedAt) > time.Hour {
			delete(pendingImports, id)
		}
	}
	pendingImports[batch.ID] = batch
	importsMu.Unlock()
	return batch
}

// ---------- admin 端点 ----------

// POST /admin/api/models/import/preview  body: {id}
// 拉取 + 分类 + 生成预览批次。上游失败按既有约定：HTTP 200 包 {error}。
func handleModelImportPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method_not_allowed"})
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	loadProviders()
	providersMu.Lock()
	var prov *CustomProvider
	for _, p := range customProviders {
		if p.ID == req.ID {
			cp := *p
			prov = &cp
			break
		}
	}
	providersMu.Unlock()
	if prov == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "provider not found"})
		return
	}
	entries, err := discoverModelEntries(prov.BaseURL, prov.APIKey, prov.Headers)
	if err != nil {
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"error": err.Error()}})
		return
	}
	batch := buildImportBatch(*prov, entries)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"batch": batch}})
}

// POST /admin/api/models/import/apply  body: {batchId, approve:[modelId…]}
// 仅批准列表内的 add / tier_change 落盘；写入 provider.ModelIDs 使其在
// syncProviderModels 清理逻辑中存续。
func handleModelImportApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method_not_allowed"})
		return
	}
	var req struct {
		BatchID string   `json:"batchId"`
		Approve []string `json:"approve"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	importsMu.Lock()
	batch := pendingImports[req.BatchID]
	importsMu.Unlock()
	if batch == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "batch not found (already applied or expired)"})
		return
	}
	approved := make(map[string]bool, len(req.Approve))
	for _, id := range req.Approve {
		approved[id] = true
	}
	byID := make(map[string]importItem, len(batch.Items))
	for _, it := range batch.Items {
		byID[it.ModelID] = it
	}
	prov := func() *CustomProvider {
		loadProviders()
		providersMu.Lock()
		defer providersMu.Unlock()
		for _, p := range customProviders {
			if p.ID == batch.ProviderID {
				cp := *p
				return &cp
			}
		}
		return nil
	}()
	if prov == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "provider no longer exists"})
		return
	}
	added, changed, now := 0, 0, time.Now().Unix()
	pool := loadPool()
	poolMu.Lock()
	existing := make(map[string]*Model, len(pool.Models))
	for i := range pool.Models {
		existing[pool.Models[i].ID] = &pool.Models[i]
	}
	for id := range approved {
		it, ok := byID[id]
		if !ok || (it.Action != "add" && it.Action != "tier_change") {
			continue
		}
		if m, exists := existing[id]; exists {
			m.Cost, m.TierSignal, m.TierDecidedAt = it.Tier, it.Signal, now
			changed++
			continue
		}
		pool.Models = append(pool.Models, Model{
			ID: id, Provider: prov.Name, Cost: it.Tier, Status: "active",
			Source: "provider", TierSignal: it.Signal, TierDecidedAt: now,
		})
		// 进入 provider.ModelIDs：syncProviderModels 的清理循环以它为准
		found := false
		for _, mid := range prov.ModelIDs {
			if mid == id {
				found = true
				break
			}
		}
		if !found {
			prov.ModelIDs = append(prov.ModelIDs, id)
		}
		added++
	}
	poolMu.Unlock()
	if added+changed == 0 {
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"added": 0, "changed": 0}})
		return
	}
	providersMu.Lock()
	for i, p := range customProviders {
		if p.ID == prov.ID {
			customProviders[i].ModelIDs = prov.ModelIDs
			break
		}
	}
	saveProvidersLocked()
	providersMu.Unlock()
	savePool()
	importsMu.Lock()
	delete(pendingImports, batch.ID)
	importsMu.Unlock()
	syncProviderModels()
	log.Printf("model import applied: batch %s provider %s added=%d tierChanged=%d", batch.ID, prov.Name, added, changed)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"added": added, "changed": changed}})
}

// GET /admin/api/models/import/pending — 当前待批准批次（摘要）。
func handleModelImportPending(w http.ResponseWriter, r *http.Request) {
	importsMu.Lock()
	out := make([]map[string]any, 0, len(pendingImports))
	for _, b := range pendingImports {
		out = append(out, map[string]any{
			"id": b.ID, "providerId": b.ProviderID, "providerName": b.ProviderName,
			"createdAt": b.CreatedAt, "items": len(b.Items),
		})
	}
	importsMu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i]["createdAt"].(time.Time).After(out[j]["createdAt"].(time.Time))
	})
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"batches": out}})
}

// POST /admin/api/models/import/discard  body: {batchId}
func handleModelImportDiscard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BatchID string `json:"batchId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	importsMu.Lock()
	delete(pendingImports, req.BatchID)
	importsMu.Unlock()
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// POST /admin/api/models/cost  body: {id, cost:"free"|"pass"|""}
// 手动定级（TierSignal="manual"）；cost 传空字符串=恢复自动（清除 manual 标记，
// 层级保留现值，等下一次导入/同步再判定）。§9.3：manual 条目在导入批次中 protected。
func handleAdminModelCost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method_not_allowed"})
		return
	}
	var req struct {
		ID   string `json:"id"`
		Cost string `json:"cost"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	if req.ID == "" || (req.Cost != "free" && req.Cost != "pass" && req.Cost != "") {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "id and cost(free|pass|empty) required"})
		return
	}
	p := loadPool()
	poolMu.Lock()
	found := false
	for i, m := range p.Models {
		if m.ID == req.ID {
			if req.Cost == "" {
				p.Models[i].TierSignal = ""
				p.Models[i].TierDecidedAt = 0
			} else {
				p.Models[i].Cost = req.Cost
				p.Models[i].TierSignal = "manual"
				p.Models[i].TierDecidedAt = time.Now().Unix()
			}
			found = true
			break
		}
	}
	poolMu.Unlock()
	if !found {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "model not found"})
		return
	}
	savePool()
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}
