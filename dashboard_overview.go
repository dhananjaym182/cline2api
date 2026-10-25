package main

import (
	"net/http"
	"sort"
	"strconv"
	"time"
)

// Dashboard overview aggregation. The admin UI's dashboard tab calls
// GET /admin/api/dashboard/overview once per refresh and renders verdict hero,
// throughput buckets, per-provider fleet and top models from the result.

const (
	dashboardDefaultHours = 24
	dashboardMaxHours     = 168
	dashboardMaxBuckets   = 96
	dashboardMaxFleet     = 12
	dashboardTopModels    = 8
)

type dashBucket struct {
	T   int64 `json:"t"`  // bucket start, unix seconds
	OK  int64 `json:"ok"` // completed requests
	Err int64 `json:"err"`
}

type dashFleetRow struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Requests int64   `json:"requests"`
	Success  int64   `json:"success"`
	Rate     float64 `json:"rate"`
	Series   []int64 `json:"series"`
}

type dashModelRow struct {
	Model    string `json:"model"`
	Requests int64  `json:"requests"`
	Tokens   int64  `json:"tokens"`
}

// dashboardOverview aggregates the in-memory request log over the trailing
// window. requestLogs is newest-first, so the walk stops at the window start.
func dashboardOverview(hours int) map[string]any {
	if hours < 1 {
		hours = dashboardDefaultHours
	}
	if hours > dashboardMaxHours {
		hours = dashboardMaxHours
	}
	now := time.Now()
	from := now.Add(-time.Duration(hours) * time.Hour)

	bucketMin := hours * 60 / dashboardMaxBuckets
	if bucketMin < 2 {
		bucketMin = 2
	}
	if bucketMin > 60 {
		bucketMin = 60
	}
	bucketSecs := int64(bucketMin) * 60
	nBuckets := int(now.Sub(from) / (time.Duration(bucketSecs) * time.Second))
	if nBuckets < 1 {
		nBuckets = 1
	}
	bucketStart := now.Add(-time.Duration(nBuckets) * time.Duration(bucketSecs) * time.Second)

	buckets := make([]dashBucket, nBuckets)
	for i := range buckets {
		buckets[i].T = bucketStart.Add(time.Duration(i) * time.Duration(bucketSecs) * time.Second).Unix()
	}

	var (
		totalReqs, totalOK, totalErr, totalTokens int64
		sumLatency, sumTTFT, nLatency, nTTFT      int64
		fallbacks                                 int64
	)
	type fleetAgg struct {
		label    string
		requests int64
		success  int64
		series   []int64
	}
	type modelAgg struct {
		requests int64
		tokens   int64
	}
	fleets := map[string]*fleetAgg{}
	models := map[string]*modelAgg{}

	bucketIndex := func(ts time.Time) int {
		if ts.Before(bucketStart) {
			return -1
		}
		idx := int(ts.Sub(bucketStart) / (time.Duration(bucketSecs) * time.Second))
		if idx < 0 || idx >= nBuckets {
			return -1
		}
		return idx
	}

	requestLogsMu.Lock()
	for _, e := range requestLogs {
		if e.StartedAt.Before(from) {
			break // newest-first: everything after this is older
		}
		totalReqs++
		ok := e.Completed && e.Error == ""
		if ok {
			totalOK++
		} else {
			totalErr++
		}
		totalTokens += e.TotalTokens
		if e.DurationMs > 0 {
			sumLatency += e.DurationMs
			nLatency++
		}
		if e.TTFTMs > 0 {
			sumTTFT += e.TTFTMs
			nTTFT++
		}
		fallbacks += int64(e.FallbackCount)

		if idx := bucketIndex(e.StartedAt); idx >= 0 {
			if ok {
				buckets[idx].OK++
			} else {
				buckets[idx].Err++
			}
		}

		upstream := e.Upstream
		if upstream == "" {
			upstream = "cline"
		}
		key := upstream
		if e.ProviderName != "" {
			key = upstream + ":" + e.ProviderName
		}
		f := fleets[key]
		if f == nil {
			label := upstream
			if e.ProviderName != "" {
				label = e.ProviderName
			}
			f = &fleetAgg{label: label, series: make([]int64, nBuckets)}
			fleets[key] = f
		}
		f.requests++
		if ok {
			f.success++
		}
		if idx := bucketIndex(e.StartedAt); idx >= 0 {
			f.series[idx]++
		}

		m := e.Model
		if m == "" {
			m = "-"
		}
		mo := models[m]
		if mo == nil {
			mo = &modelAgg{}
			models[m] = mo
		}
		mo.requests++
		mo.tokens += e.TotalTokens
	}
	requestLogsMu.Unlock()

	// Only fleet rows for providers seen inside the window get a series; rows
	// created after the walk (none) keep lengths consistent.
	fleetRows := make([]dashFleetRow, 0, len(fleets))
	for key, f := range fleets {
		rate := 0.0
		if f.requests > 0 {
			rate = float64(f.success) / float64(f.requests)
		}
		fleetRows = append(fleetRows, dashFleetRow{
			Key: key, Label: f.label, Requests: f.requests, Success: f.success, Rate: rate, Series: f.series,
		})
	}
	sort.Slice(fleetRows, func(i, j int) bool {
		if fleetRows[i].Requests != fleetRows[j].Requests {
			return fleetRows[i].Requests > fleetRows[j].Requests
		}
		return fleetRows[i].Label < fleetRows[j].Label
	})
	if len(fleetRows) > dashboardMaxFleet {
		fleetRows = fleetRows[:dashboardMaxFleet]
	}

	modelRows := make([]dashModelRow, 0, len(models))
	for m, mo := range models {
		modelRows = append(modelRows, dashModelRow{Model: m, Requests: mo.requests, Tokens: mo.tokens})
	}
	sort.Slice(modelRows, func(i, j int) bool {
		if modelRows[i].Requests != modelRows[j].Requests {
			return modelRows[i].Requests > modelRows[j].Requests
		}
		return modelRows[i].Model < modelRows[j].Model
	})
	if len(modelRows) > dashboardTopModels {
		modelRows = modelRows[:dashboardTopModels]
	}

	var avgLatency, avgTTFT float64
	if nLatency > 0 {
		avgLatency = float64(sumLatency) / float64(nLatency)
	}
	if nTTFT > 0 {
		avgTTFT = float64(sumTTFT) / float64(nTTFT)
	}
	successRate := 0.0
	if totalReqs > 0 {
		successRate = float64(totalOK) / float64(totalReqs)
	}

	strategy := "round_robin"
	onlyFree := false
	cfg := getProxyConfig()
	if cfg != nil {
		strategy = cfg.Strategy
		onlyFree = cfg.OnlyFree
	}

	return map[string]any{
		"windowHours": hours,
		"from":        from.UTC().Format(time.RFC3339),
		"to":          now.UTC().Format(time.RFC3339),
		"bucketSecs":  bucketSecs,
		"totals": map[string]any{
			"requests":     totalReqs,
			"success":      totalOK,
			"fail":         totalErr,
			"tokens":       totalTokens,
			"successRate":  successRate,
			"avgLatencyMs": avgLatency,
			"avgTtftMs":    avgTTFT,
			"fallbacks":    fallbacks,
		},
		"buckets": buckets,
		"fleet":   fleetRows,
		"models":  modelRows,
		"runtime": map[string]any{
			"strategy":     strategy,
			"version":      appVersion,
			"defaultModel": getDefaultModel(),
			"onlyFree":     onlyFree,
		},
	}
}

// GET /admin/api/dashboard/overview?hours=24
func handleDashboardOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	hours := dashboardDefaultHours
	if v := r.URL.Query().Get("hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			hours = n
		}
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: dashboardOverview(hours)})
}
