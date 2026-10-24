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
	"unicode"
)

// Tier signals (stored in Model.TierSignal; admin UI shows why a model is free
// and supports manual override to "manual"). Ordered strongest-first.
const (
	tierSignalPricingZero  = "pricing-zero"      // upstream pricing: prompt/completion 0 (and no per-request fee)
	tierSignalFlagFree     = "flag-free"         // entry declares free itself: free=true or mode:"free"
	tierSignalSuffixFree   = "suffix-free"       // id ends with :free / -free
	tierSignalNameToken    = "name-token-free"   // id has a standalone free segment (free-x, free/ns/x, x:free:y, cline-free/x)
	tierSignalDisplayName  = "display-name-free" // upstream display name carries a standalone free word, e.g. "Flash (Free)"
	tierSignalAllowlist    = "allowlist"         // tags contain free, or hit the built-in zen seed allowlist
	tierSignalProviderFree = "provider-free"     // whole provider is free (Free=true / no-key source)
	tierSignalPricingPaid  = "pricing-paid"      // explicit nonzero upstream pricing → paid, outranks provider-free
	tierSignalPaidDefault  = "paid-default"      // no free signal → paid, never guessed
)

// upstreamModelEntry is one parsed /models item (OpenAI list + OpenRouter-style pricing).
type upstreamModelEntry struct {
	ID             string
	Name           string // display name (OpenAI "name" / "display_name")
	PricingPrompt  *float64
	PricingComplet *float64
	PricingRequest *float64 // per-request fee, when the catalog exposes pricing.request
	Tags           []string
	FreeFlag       bool   // boolean free:true on the entry (some gateways mark it this way)
	Mode           string // string mode field (Cline catalog uses mode:"free")
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

// freeWord reports whether s carries a standalone "free" word. Any
// non-alphanumeric rune is a separator, so Cline/Zen style ids
// ("cline-free/x", "y-free") and display names ("Flash (Free)") match while
// freemap/freemium substrings never do.
func freeWord(s string) bool {
	for _, tok := range strings.FieldsFunc(strings.ToLower(strings.TrimSpace(s)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if tok == "free" {
			return true
		}
	}
	return false
}

// classifyUpstreamModel checks signals in order and returns on the first hit.
// Explicit free markers (flags, :free/-free suffixes, free words in the id or
// display name, tags, the zen seed allowlist) outrank explicit non-zero
// pricing: gateway catalogs commonly list their free variants with the
// reference (non-zero) list price, mirroring how Cline (FREE tag / free array)
// and Zen (-free suffix / seed allowlist) decide.
func classifyUpstreamModel(e upstreamModelEntry, providerFree bool) (tier, signal string) {
	if e.PricingPrompt != nil && e.PricingComplet != nil && *e.PricingPrompt == 0 && *e.PricingComplet == 0 &&
		(e.PricingRequest == nil || *e.PricingRequest == 0) {
		return "free", tierSignalPricingZero
	}
	if e.FreeFlag || strings.EqualFold(strings.TrimSpace(e.Mode), "free") {
		return "free", tierSignalFlagFree
	}
	lower := strings.ToLower(e.ID)
	if strings.HasSuffix(lower, ":free") || strings.HasSuffix(lower, "-free") {
		return "free", tierSignalSuffixFree
	}
	if freeWord(lower) {
		return "free", tierSignalNameToken
	}
	if freeWord(e.Name) {
		return "free", tierSignalDisplayName
	}
	for _, tag := range e.Tags {
		if strings.EqualFold(strings.TrimSpace(tag), "free") {
			return "free", tierSignalAllowlist
		}
	}
	if freeIDAllowlist[lower] {
		return "free", tierSignalAllowlist
	}
	// Explicit non-zero pricing (including "-1" variable/unknown, e.g. kilo auto
	// routing) means paid, and it outranks the provider-wide free flag so a
	// mixed catalog marked free:true does not turn every model free.
	if (e.PricingPrompt != nil && *e.PricingPrompt != 0) || (e.PricingComplet != nil && *e.PricingComplet != 0) ||
		(e.PricingRequest != nil && *e.PricingRequest != 0) {
		return "pass", tierSignalPricingPaid
	}
	if providerFree {
		return "free", tierSignalProviderFree
	}
	return "pass", tierSignalPaidDefault
}

// fetcherBaseForBase 按规范化 baseURL 匹配预设的 ModelsFetcher 目录端点。
// 仅采信以 /models 结尾的 fetcher URL（openrouter / kilo 形态）；其他自定义格式
// （pricing 页等）解析不了，宁可不回退也不瞎猜。返回去掉尾部 /models 的基址。
func fetcherBaseForBase(baseURL string) string {
	norm := func(u string) string {
		u = strings.ToLower(strings.TrimRight(strings.TrimSpace(u), "/"))
		u = strings.TrimSuffix(u, "/chat/completions")
		return strings.Replace(u, "//localhost", "//127.0.0.1", 1)
	}
	target := norm(baseURL)
	if target == "" {
		return ""
	}
	for _, m := range []map[string]providerPreset{providerPresets, providerPresetsImported} {
		for _, p := range m {
			if p.ModelsFetcher == nil || !strings.HasSuffix(strings.ToLower(p.ModelsFetcher.URL), "/models") {
				continue
			}
			if norm(p.BaseURL) == target {
				fb := strings.TrimRight(strings.TrimSuffix(strings.TrimRight(p.ModelsFetcher.URL, "/"), "/models"), "/")
				if fb != "" && fb != target {
					return fb
				}
			}
		}
	}
	return ""
}

// discoverModelEntries 拉取 {baseURL}/models（404 时补试 /v1/models，再回退预设
// ModelsFetcher 目录端点），解析 id + 定价 + 标签。
func discoverModelEntries(baseURL, apiKey string, headers map[string]string) ([]upstreamModelEntry, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("baseURL is empty")
	}
	client := oauthHTTPClient(15 * time.Second)
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
				if n, ok := it["name"].(string); ok {
					e.Name = strings.TrimSpace(n)
				} else if n, ok := it["display_name"].(string); ok {
					e.Name = strings.TrimSpace(n)
				}
				if pr, ok := it["pricing"].(map[string]any); ok {
					e.PricingPrompt = rawNum(pr["prompt"])
					e.PricingComplet = rawNum(pr["completion"])
					e.PricingRequest = rawNum(pr["request"])
				}
				if tags, ok := it["tags"].([]any); ok {
					for _, t := range tags {
						if s, ok := t.(string); ok {
							e.Tags = append(e.Tags, s)
						}
					}
				}
				switch fv := it["free"].(type) {
				case bool:
					e.FreeFlag = fv
				case string:
					e.FreeFlag = strings.EqualFold(strings.TrimSpace(fv), "true")
				}
				if mv, ok := it["mode"].(string); ok {
					e.Mode = strings.TrimSpace(mv)
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
	tryBase := func(b string) ([]upstreamModelEntry, error) {
		status, body, err := fetch(b + "/models")
		if err != nil {
			return nil, fmt.Errorf("request failed: %v", err)
		}
		if status == http.StatusNotFound {
			s2, b2, e2 := fetch(b + "/v1/models")
			if e2 == nil && s2 == http.StatusOK {
				status, body = s2, b2
			}
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: %s", status, truncate(strings.TrimSpace(string(body)), 200))
		}
		return parse(body)
	}
	bases := []string{baseURL}
	if fb := fetcherBaseForBase(baseURL); fb != "" {
		bases = append(bases, fb)
	}
	var entries []upstreamModelEntry
	var lastErr error
	for _, b := range bases {
		entries, lastErr = tryBase(b)
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return nil, lastErr
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
	Auto         bool         `json:"auto,omitempty"` // 后台自动检测生成（同一 provider 至多一个，重复检测时刷新）
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

// POST /admin/api/models/import/preview  body: {id} 或草稿 {baseURL,apiKey,headers,free,name}
// 拉取 + 分类 + 生成预览批次。草稿（未保存 Provider）走后者，批次 providerId 为空、
// 仅可预览：apply 端点找不到 provider 会拒绝，前端改为「所选并入并保存」。
// 上游失败按既有约定：HTTP 200 包 {error}。
func handleModelImportPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method_not_allowed"})
		return
	}
	var req struct {
		ID      string            `json:"id"`
		BaseURL string            `json:"baseURL"`
		APIKey  string            `json:"apiKey"`
		Headers map[string]string `json:"headers"`
		Free    bool              `json:"free"`
		Name    string            `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	loadProviders()
	providersMu.Lock()
	var prov *CustomProvider
	if req.ID != "" {
		for _, p := range customProviders {
			if p.ID == req.ID {
				cp := *p
				prov = &cp
				break
			}
		}
	} else if strings.TrimSpace(req.BaseURL) != "" {
		prov = &CustomProvider{
			Name:    req.Name,
			BaseURL: strings.TrimRight(strings.TrimSpace(req.BaseURL), "/"),
			APIKey:  req.APIKey,
			Headers: req.Headers,
			Free:    req.Free,
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
			"createdAt": b.CreatedAt, "items": len(b.Items), "auto": b.Auto,
		})
	}
	importsMu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i]["createdAt"].(time.Time).After(out[j]["createdAt"].(time.Time))
	})
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"batches": out}})
}

// GET /admin/api/models/import/batch?id=x — 读回待批准批次全文（横幅点击打开 Auto 批次用）。
func handleModelImportBatch(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	importsMu.Lock()
	batch := pendingImports[id]
	importsMu.Unlock()
	if batch == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "batch not found (already applied or expired)"})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"batch": batch}})
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
