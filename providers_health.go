package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Provider 健康快照：配额用量 / 冷却恢复 / 近期成败热度。数据全部在内存
// （score ring、RPM/日计数、冷却表），重启后从零学起，与 quality 策略同源。

type modelCooldown struct {
	Model string    `json:"model"`
	Until time.Time `json:"until"`
}

type providerHealth struct {
	Daily        int64           `json:"daily"`
	Rpm          int             `json:"rpm"`
	Cooldowns    []modelCooldown `json:"cooldowns"`
	RecoveryAt   string          `json:"recoveryAt,omitempty"` // 最早恢复（排序用）
	Rate         float64         `json:"rate"`
	Samples      int             `json:"samples"`
	AvgLatencyMs float64         `json:"avgLatencyMs"`
	Recent       []int           `json:"recent"` // 时间升序的最近成败 1/0
	Limits       map[string]any  `json:"limits"`
}

const providerRecentWindow = 16

// providerHealthSnapshot 按 providerID 汇总健康数据。锁序：providersMu 之外
// 只短暂取 budgetMu/scoreMu，避免与派发路径形成嵌套。
func providerHealthSnapshot() map[string]*providerHealth {
	loadProviders()
	providersMu.Lock()
	ids := make([]string, 0, len(customProviders))
	limits := map[string]map[string]any{}
	cooldowns := map[string]time.Time{}
	for k, v := range providerCooldowns {
		cooldowns[k] = v
	}
	for _, p := range customProviders {
		ids = append(ids, p.ID)
		limits[p.ID] = map[string]any{"rpm": p.RpmLimit, "daily": p.DailyLimit}
	}
	providersMu.Unlock()

	out := map[string]*providerHealth{}
	for _, id := range ids {
		out[id] = &providerHealth{Cooldowns: []modelCooldown{}, Recent: []int{}, Limits: limits[id]}
	}
	health := func(id string) *providerHealth {
		if h := out[id]; h != nil {
			return h
		}
		return nil
	}

	now := time.Now()
	today := now.UTC().Format("2006-01-02")
	cutoff := now.Add(-providerRPMWindow)

	budgetMu.Lock()
	for key, times := range providerRPM {
		id := strings.SplitN(key, ":", 2)[0]
		h := health(id)
		if h == nil {
			continue
		}
		for _, t := range times {
			if t.After(cutoff) {
				h.Rpm++
			}
		}
	}
	for id, days := range providerDaily {
		if h := health(id); h != nil {
			h.Daily = days[today]
		}
	}
	budgetMu.Unlock()

	var earliest time.Time
	for key, until := range cooldowns {
		id := strings.SplitN(key, ":", 2)[0]
		h := health(id)
		if h == nil || !now.Before(until) {
			continue
		}
		model := key
		if i := strings.Index(key, ":"); i >= 0 {
			model = key[i+1:]
		}
		h.Cooldowns = append(h.Cooldowns, modelCooldown{Model: model, Until: until})
		if earliest.IsZero() || until.Before(earliest) {
			earliest = until
		}
	}
	for _, h := range out {
		sort.Slice(h.Cooldowns, func(i, j int) bool { return h.Cooldowns[i].Until.Before(h.Cooldowns[j].Until) })
		if len(h.Cooldowns) > 0 {
			h.RecoveryAt = h.Cooldowns[0].Until.UTC().Format(time.RFC3339)
		}
	}

	scoreMu.Lock()
	cut := now.Add(-providerScoreWindow)
	type sample struct {
		at  time.Time
		ok  bool
		lat int64
	}
	recent := map[string][]sample{}
	for key, s := range providerScores {
		id := strings.SplitN(key, ":", 2)[0]
		h := health(id)
		if h == nil {
			continue
		}
		for _, o := range s.ring {
			if o.at.IsZero() || o.at.Before(cut) {
				continue
			}
			h.Samples++
			if o.ok {
				recent[id] = append(recent[id], sample{at: o.at, ok: true, lat: o.latMS})
			} else {
				recent[id] = append(recent[id], sample{at: o.at, ok: false})
			}
		}
	}
	scoreMu.Unlock()

	for id, samples := range recent {
		h := health(id)
		sort.Slice(samples, func(i, j int) bool { return samples[i].at.Before(samples[j].at) })
		var okN, latSum, latN int64
		for _, sm := range samples {
			if sm.ok {
				okN++
				latSum += sm.lat
				latN++
			}
		}
		h.Samples = len(samples)
		if len(samples) > 0 {
			h.Rate = float64(okN) / float64(len(samples))
		} else {
			h.Rate = providerNeutralSuccess
		}
		if latN > 0 {
			h.AvgLatencyMs = float64(latSum) / float64(latN)
		}
		if len(samples) > providerRecentWindow {
			samples = samples[len(samples)-providerRecentWindow:]
		}
		for _, sm := range samples {
			v := 0
			if sm.ok {
				v = 1
			}
			h.Recent = append(h.Recent, v)
		}
	}
	return out
}

// accountRecentOutcomes 从请求日志提取每个账号最近 N 次成败（时间升序）。
func accountRecentOutcomes() map[string][]int {
	result := map[string][]int{}
	type acc struct {
		items []int
	}
	counts := map[string]*acc{}
	requestLogsMu.Lock()
	for _, e := range requestLogs { // 新→旧
		if e.AccountID == "" {
			continue
		}
		a := counts[e.AccountID]
		if a == nil {
			a = &acc{}
			counts[e.AccountID] = a
		}
		if len(a.items) >= providerRecentWindow {
			continue
		}
		v := 0
		if e.Completed && e.Error == "" {
			v = 1
		}
		a.items = append(a.items, v)
	}
	requestLogsMu.Unlock()
	for id, a := range counts {
		out := make([]int, 0, len(a.items))
		for i := len(a.items) - 1; i >= 0; i-- {
			out = append(out, a.items[i])
		}
		result[id] = out
	}
	return result
}

// GET /admin/api/account-health — 账号最近成败热度（请求日志派生，纯内存）。
func handleAccountHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"accounts": accountRecentOutcomes()}})
}

// POST /admin/api/providers/cooldown-reset  body: {id, model?}
func handleProviderCooldownReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Model = strings.TrimSpace(req.Model)
	if req.ID == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "id is required"})
		return
	}
	providersMu.Lock()
	var removed int
	for key := range providerCooldowns {
		id := strings.SplitN(key, ":", 2)[0]
		if id != req.ID {
			continue
		}
		if req.Model != "" {
			model := key
			if i := strings.Index(key, ":"); i >= 0 {
				model = key[i+1:]
			}
			if model != req.Model {
				continue
			}
		}
		delete(providerCooldowns, key)
		removed++
	}
	if removed > 0 {
		saveProviderCooldownsLocked()
	}
	providersMu.Unlock()
	if removed == 0 {
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"removed": 0}})
		return
	}
	log.Printf("admin: provider cooldown reset id=%s model=%s removed=%d", req.ID, req.Model, removed)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"removed": removed}})
}
