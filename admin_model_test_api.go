package main

// 管理后台「模型可用性测试」API 三个入口共用一个小型 best-effort 测试器：
//
//	EP3 POST /admin/api/models/test  {model}                    → 任一模型单测
//	EP4 POST /admin/api/combos/test  {id, model?}               → 逐条目测组合
//	    （/admin/api/providers/models 见文件尾 handleProviderModels —— EP2）
//
// quickModelTest 是共享入口（EP3/EP4 内核）：max_tokens=16 + 单条 "hi" user 消息，
// 复用既有路由判定（provider 冷却/预算表是只读，测试旁路粘性会话与日志）。
// 上游失败 ≠ API 失败：一律 200 包裹 {success:true, data:{ok:false, error:...}}。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// quickTestPoolTimeout 组合条目测试里 Cline 账号池路径的 ctx 超时（EP4 要求 ~20s）。
	quickTestPoolTimeout = 20 * time.Second
	// quickTestProviderTimeoutSec EP4 里 provider 路径的兜底超时秒数（callProvider 用
	// provider.TimeoutSec 建独立 client，不接 ctx；测试时夹紧到 20s）。
	quickTestProviderTimeoutSec = 20
	// quickTestMaxAccounts 一次测试最多尝试的账号数（轮询换号上限，避免测试烧池）。
	quickTestMaxAccounts = 2
	// quickTestModelsTimeout /admin/api/providers/models 上游探测超时。
	quickTestModelsTimeout = 15 * time.Second
)

// miniTestParams 一条 min-token 测试请求体（与 handleProviderTest 同口径）。
func miniTestParams(model string) map[string]any {
	return map[string]any{
		"model":      model,
		"max_tokens": 16,
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
	}
}

// ---- 同模型并行测试的批量守卫 --------------------------------------------

// quickTestLocks model → 互斥锁（仅内存态，不持久化）：并行 UI 测试同一模型时
// 串行化，避免多份 16-token 探测同时砸向上游/账号池造成伪 429。
var (
	quickTestMu    sync.Mutex
	quickTestLocks = map[string]*sync.Mutex{}
)

func quickTestLock(model string) *sync.Mutex {
	quickTestMu.Lock()
	defer quickTestMu.Unlock()
	mu := quickTestLocks[model]
	if mu == nil {
		mu = &sync.Mutex{}
		quickTestLocks[model] = mu
	}
	return mu
}

// ---- 共享测试器 ---------------------------------------------------------

// quickModelTest 任一模型的 best-effort 小补全测试（EP3 入口；EP4 明细见
// runQuickModelTest）。路由：自定义 provider → zen（免费）→ Cline 账号池，
// 与 callSingleModel 的派发语义一致。
func quickModelTest(model string, forcedProvider *CustomProvider) map[string]any {
	return runQuickModelTest(model, forcedProvider, "", 0)
}

// runQuickModelTest quickModelTest 的展开形式：
//   - forcedProvider 非空 → 只测这一家 provider（EP4 source=provider）。
//   - pinSource "provider" → 只走 forcedProvider；"cline" → 只走账号池；
//     "zen" → 只走 zen；"" 自动（provider → zen → 池，callSingleModel 语义）。
//   - poolTimeout > 0 时账号池调用带 ctx 超时（EP4 每条目 ~20s）。
//
// 返回 map 键：ok / upstream("provider"|"opencode"|"cline") / model / durationMs /
// error? / inputTokens? / outputTokens?。上游失败也返回（ok=false），不 panic。
func runQuickModelTest(model string, forcedProvider *CustomProvider, pinSource string, poolTimeout time.Duration) map[string]any {
	model = strings.TrimSpace(model)
	start := time.Now()
	result := map[string]any{"ok": false, "model": model}
	setResult := func(upstream, errMsg string) {
		result["upstream"] = upstream
		result["durationMs"] = time.Since(start).Milliseconds()
		result["ok"] = errMsg == ""
		if errMsg != "" {
			result["error"] = truncate(errMsg, 300)
		}
	}
	recordSuccess := func(upstream string, resp *http.Response) {
		setResult(upstream, "")
		if resp == nil {
			return
		}
		if in, out, valid := readMiniTestUsage(resp); valid {
			result["inputTokens"] = in
			result["outputTokens"] = out
		}
	}

	if model == "" {
		setResult("", "model is required")
		return result
	}

	// 同模型串行化：锁内覆盖整次上游调用（成功或失败都会释放）。
	lk := quickTestLock(model)
	lk.Lock()
	defer lk.Unlock()

	params := miniTestParams(model)

	// ---- provider 路径 ----
	tryProvider := func(p *CustomProvider) {
		resp, err := callProvider(p, params, false)
		if err != nil {
			setResult(upstreamProvider, err.Error())
			return
		}
		recordSuccess(upstreamProvider, resp)
	}

	// ---- zen 路径 ----
	tryZen := func() {
		zm, ok := resolveZenInfo(model)
		if !ok || !isZenFreeModel(zm) {
			setResult(upstreamOpenCode, fmt.Sprintf("model %q is not an available free zen model", model))
			return
		}
		resp, err := callZenAPI(params, false)
		if err != nil {
			setResult(upstreamOpenCode, err.Error())
			return
		}
		recordSuccess(upstreamOpenCode, resp)
	}

	// ---- Cline 账号池路径 ----
	tryPool := func() {
		resp, err := quickPoolAttempt(model, params, poolTimeout)
		if err != nil {
			setResult(upstreamCline, err.Error())
			return
		}
		recordSuccess(upstreamCline, resp)
	}

	switch strings.TrimSpace(pinSource) {
	case "provider":
		if forcedProvider != nil {
			tryProvider(forcedProvider)
			return result
		}
		setResult(upstreamProvider, "provider not resolvable for test")
		return result
	case "cline":
		tryPool()
		return result
	case "zen":
		tryZen()
		return result
	}

	// ---- 自动派发（pinSource=""）：与 callSingleModel 的 auto 分支同序 ----
	if forcedProvider != nil {
		tryProvider(forcedProvider)
		return result
	}

	kind := comboRouteKind(model)
	switch kind {
	case "reject":
		setResult(upstreamOpenCode, fmt.Sprintf("model %q is a paid opencode model; only free models are proxied", model))
		return result
	case "zen-degraded":
		setResult(upstreamOpenCode, fmt.Sprintf("model %q temporarily unavailable (opencode zen degraded)", model))
		return result
	case "zen":
		tryZen()
		return result
	}

	// cline 侧：先自定义 provider（若有），失败/缺席再走账号池。
	var lastProviderErr error
	for _, p := range resolveProvidersForModel(model) {
		resp, err := callProvider(p, params, false)
		if err == nil {
			recordSuccess(upstreamProvider, resp)
			return result
		}
		lastProviderErr = err
		log.Printf("  model test: provider attempt failed name=%s model=%q: %v", p.Name, model, err)
	}
	if lastProviderErr != nil {
		log.Printf("  model test: model %q provider fallback to account pool: %v", model, lastProviderErr)
	}
	tryPool()
	return result
}

// quickPoolAttempt 账号池小补全尝试：最多试 quickTestMaxAccounts 个账号（严格
// 挑选 + 402/换号 + 429/换号 + 确定性 400 立即失败），与 callSingleModel 的池
// 分支同语义但不无限循环、不走模型冷却后的兜底挑选。
func quickPoolAttempt(model string, params map[string]any, timeout time.Duration) (*http.Response, error) {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	tried := map[string]bool{}
	var lastErr error
	for i := 0; i < quickTestMaxAccounts; i++ {
		acc := pickAccountForModelStrict(model)
		if acc == nil {
			return nil, fmt.Errorf("model %q has no available account in the pool", model)
		}
		if tried[acc.AccountID] {
			break // 防呆：轮询边界不应撞同一个账号
		}
		tried[acc.AccountID] = true
		resp, usedAcc, err := callClineAPIWithAccountCtx(ctx, acc, params, false)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		var acctErr *clineAccountUnavailableError
		if errors.As(err, &acctErr) {
			continue // token 失效等：换下一个账号
		}
		if isDeterministicRequestError(err) {
			return nil, err // 换账号不会自愈的 payload 400
		}
		if apiErr, ok := err.(*clineAPIError); ok {
			switch apiErr.statusCode {
			case http.StatusPaymentRequired: // 402：冷却该账号该模型后换号
				if usedAcc != nil {
					setModelCooldown(usedAcc, model, time.Now().Add(30*time.Minute))
				}
				continue
			case http.StatusTooManyRequests: // 429：模型级冷却已记录，换号
				continue
			}
		}
		return nil, err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("model %q unavailable on all eligible accounts", model)
	}
	return nil, lastErr
}

// readMiniTestUsage 从测试响应读取 token 用量（复用响应归一化与解析）。
func readMiniTestUsage(resp *http.Response) (prompt, completion int64, valid bool) {
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		if d, ok := obj["data"].(map[string]any); ok {
			obj = d
		}
		obj = normalizeOpenAIResponse(obj)
	}
	if u := parseTokenUsage(obj["usage"]); u.Valid {
		return u.Prompt, u.Completion, true
	}
	return 0, 0, false
}

// ---- EP3：单模型测试 ----------------------------------------------------

// POST /admin/api/models/test  body: {model}
// data: quickModelTest 结果（ok / upstream / model / durationMs / error? /
// inputTokens? / outputTokens?）。上游失败仍是 200 + ok:false。
func handleAdminModelTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "model_id_required")})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: quickModelTest(model, nil)})
}

// ---- EP4：组合测试 ------------------------------------------------------

// findEnabledProviderByName 仅在存在「启用中」且 Name 匹配（去空白、大小写
// 不敏感）的 provider 时返回其副本；供组合条目 source=provider 测试用。
func findEnabledProviderByName(name string) *CustomProvider {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	for _, p := range listProviders() {
		if p.Enabled && strings.EqualFold(strings.TrimSpace(p.Name), name) {
			cp := p
			return &cp
		}
	}
	return nil
}

// comboEntryTest 按条目的上游固定语义测一个条目（不改动组合本身）。
func comboEntryTest(entry ComboEntry) map[string]any {
	m := strings.TrimSpace(entry.Model)
	switch strings.TrimSpace(entry.Source) {
	case "provider":
		name := strings.TrimSpace(entry.Provider)
		p := findEnabledProviderByName(name)
		if p == nil {
			return map[string]any{
				"ok": false, "model": m, "upstream": upstreamProvider, "durationMs": 0,
				"error": fmt.Sprintf("provider %q not found or disabled", name),
			}
		}
		cp := *p
		if cp.TimeoutSec <= 0 || cp.TimeoutSec > quickTestProviderTimeoutSec {
			cp.TimeoutSec = quickTestProviderTimeoutSec // 测试专用超时夹紧
		}
		return runQuickModelTest(m, &cp, "provider", quickTestPoolTimeout)
	case "cline":
		return runQuickModelTest(m, nil, "cline", quickTestPoolTimeout)
	case "zen":
		return runQuickModelTest(m, nil, "zen", quickTestPoolTimeout)
	default:
		return quickModelTest(m, nil)
	}
}

// comboTestResult 把 quickModelTest 的结果包上条目元数据（source/固定 provider）。
func comboTestResult(entry ComboEntry) map[string]any {
	out := comboEntryTest(entry)
	out["source"] = strings.TrimSpace(entry.Source)
	if name := strings.TrimSpace(entry.Provider); name != "" {
		out["provider"] = name
	}
	return out
}

// POST /admin/api/combos/test  body: {id, model?}
// data: {comboId, results:[{model, source, ok, durationMs, upstream, error?,
// inputTokens?, outputTokens?}], allOk}。禁用条目跳过；model 给定时只测
// 匹配该模型的第一条可用条目。测试不改动组合（纯读）。
func handleComboTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	var req struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}
	combo, ok := findCombo(req.ID)
	if !ok {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: tAPI(r, "combo_not_found")})
		return
	}
	filter := strings.TrimSpace(req.Model)

	results := make([]map[string]any, 0, len(combo.Entries))
	for _, entry := range combo.Entries {
		if entry.Disabled {
			continue // 禁用条目不测
		}
		m := strings.TrimSpace(entry.Model)
		if m == "" || (filter != "" && m != filter) {
			continue
		}
		results = append(results, comboTestResult(entry))
	}
	if filter != "" && len(results) == 0 {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: fmt.Sprintf("enabled entry %q not found in combo %q", filter, combo.ID)})
		return
	}
	if len(results) == 0 {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "combo has no enabled entries"})
		return
	}
	allOk := true
	for _, res := range results {
		if o, _ := res["ok"].(bool); !o {
			allOk = false
		}
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"comboId": combo.ID,
		"results": results,
		"allOk":   allOk,
	}})
}

// ---- EP2：provider 模型发现（/admin/api/providers/models）--------------

// discoverOpenAIModels 拉取 OpenAI 兼容 {baseURL}/models（首个 404 时再试一次
// {baseURL}/v1/models），返回去重排序的模型 ID；失败时返回可解析错误串
// （HTTP 状态 + 截断 body ≤300 字符，或请求异常原因）。
func discoverOpenAIModels(baseURL, apiKey string, headers map[string]string) ([]string, string) {
	client := &http.Client{Transport: httpClient.Transport, Timeout: quickTestModelsTimeout}
	tryURLs := []string{strings.TrimRight(baseURL, "/") + "/models"}

	fetch := func(rawURL string) (int, []byte, string) {
		req, err := http.NewRequest("GET", rawURL, nil)
		if err != nil {
			return 0, nil, fmt.Sprintf("create request: %v", err)
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		req.Header.Set("Accept", "application/json")
		for k, v := range headers {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			if strings.TrimSpace(v) == "" {
				req.Header.Del(k)
				continue
			}
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, fmt.Sprintf("request failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return resp.StatusCode, body, ""
	}

	status, body, reqErr := fetch(tryURLs[0])
	if reqErr != "" {
		return nil, reqErr
	}
	probeOK := status == http.StatusOK
	if status == http.StatusNotFound {
		// 一些 baseURL 不含 /v1（如 https://host）：补一次 /v1/models
		altStatus, altBody, altErr := fetch(strings.TrimRight(baseURL, "/") + "/v1/models")
		if altErr == "" && altStatus == http.StatusOK {
			status, body = altStatus, altBody
			probeOK = true
		}
	}
	if !probeOK {
		// 目录端点与 baseURL 不同的预设（如 kilo gateway/models）：回退一次 ModelsFetcher URL
		if fb := fetcherBaseForBase(baseURL); fb != "" {
			if st2, b2, e2 := fetch(fb + "/models"); e2 == "" && st2 == http.StatusOK {
				status, body = st2, b2
			} else {
				return nil, fmt.Sprintf("HTTP %d: %s", status, truncate(strings.TrimSpace(string(body)), 300))
			}
		} else {
			return nil, fmt.Sprintf("HTTP %d: %s", status, truncate(strings.TrimSpace(string(body)), 300))
		}
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Sprintf("unexpected response format: %s", truncate(strings.TrimSpace(string(body)), 300))
	}
	seen := map[string]bool{}
	var ids []string
	collect := func(list []struct {
		ID string `json:"id"`
	}) { for _, it := range list {
		id := strings.TrimSpace(it.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	} }
	collect(payload.Data)
	collect(payload.Models)
	if len(ids) == 0 {
		return nil, "upstream returned no models"
	}
	sort.Strings(ids)
	return ids, ""
}

// POST /admin/api/providers/models  body: {id} 或 {baseURL, apiKey?, headers?}
// 发现 provider 的模型列表（GET {baseURL}/models）。data 成功：
// {models:[{id:"..."}], count:N}；失败：{error:"...", models:[]}（200 包裹）。
// 纯探测，不写任何状态。
func handleProviderModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	var req struct {
		ID      string            `json:"id"`
		BaseURL string            `json:"baseURL"`
		APIKey  string            `json:"apiKey"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}

	baseURL := strings.TrimSpace(strings.TrimRight(req.BaseURL, "/"))
	if req.ID != "" {
		loadProviders()
		providersMu.Lock()
		for _, p := range customProviders {
			if p.ID == req.ID {
				if baseURL == "" {
					baseURL = strings.TrimRight(p.BaseURL, "/")
				}
				if req.APIKey == "" {
					req.APIKey = p.APIKey
				}
				if req.Headers == nil {
					req.Headers = p.Headers
				}
				// §8.3：绑定登录连接的 provider，模型发现也用连接票
				if conn := activeConnectionForProvider(p); conn != nil {
					if spec, ok := providerDeviceFlows[conn.Provider]; ok {
						req.APIKey = connectionBearer(conn, spec)
					}
				}
				break
			}
		}
		providersMu.Unlock()
		if baseURL == "" {
			writeAPI(w, http.StatusNotFound, apiResponse{Error: "provider not found"})
			return
		}
	}
	if baseURL == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "id or baseURL is required"})
		return
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "baseURL must start with http:// or https://"})
		return
	}

	ids, errMsg := discoverOpenAIModels(baseURL, strings.TrimSpace(req.APIKey), req.Headers)
	if errMsg != "" {
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
			"error":  errMsg,
			"models": []any{},
		}})
		return
	}
	out := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]string{"id": id})
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"models": out,
		"count":  len(ids),
	}})
}
