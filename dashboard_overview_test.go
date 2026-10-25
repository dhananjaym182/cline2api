package main

import (
	"testing"
	"time"
)

func TestDashboardOverviewAggregatesWindow(t *testing.T) {
	old := requestLogs
	requestLogsMu.Lock()
	defer func() {
		requestLogsMu.Lock()
		requestLogs = old
		requestLogsMu.Unlock()
	}()

	now := time.Now()
	logs := []RequestLog{
		{ID: "d", StartedAt: now.Add(-1 * time.Minute), Completed: true, Upstream: "provider", ProviderName: "openrouter", Model: "gpt-x", TotalTokens: 100, DurationMs: 900, TTFTMs: 200},
		{ID: "c", StartedAt: now.Add(-2 * time.Minute), Completed: false, Error: "boom", Upstream: "cline", Model: "gpt-x", DurationMs: 300, FallbackCount: 2},
		{ID: "b", StartedAt: now.Add(-10 * time.Minute), Completed: true, Upstream: "cline", Model: "gemini-y", TotalTokens: 50, DurationMs: 600},
		// Outside the 24h window: the newest-first walk must stop here.
		{ID: "a", StartedAt: now.Add(-30 * time.Hour), Completed: true, Upstream: "cline", Model: "old-z", TotalTokens: 9999},
	}
	requestLogs = logs
	requestLogsMu.Unlock()

	got := dashboardOverview(24)

	totals := got["totals"].(map[string]any)
	if totals["requests"] != int64(3) || totals["success"] != int64(2) || totals["fail"] != int64(1) {
		t.Fatalf("totals = %+v", totals)
	}
	if totals["tokens"] != int64(150) || totals["fallbacks"] != int64(2) {
		t.Fatalf("tokens/fallbacks = %+v", totals)
	}
	if rate := totals["successRate"].(float64); rate < 0.66 || rate > 0.67 {
		t.Fatalf("successRate = %v", rate)
	}
	if got["windowHours"] != 24 {
		t.Fatalf("windowHours = %v", got["windowHours"])
	}

	buckets := got["buckets"].([]dashBucket)
	var sumOK, sumErr int64
	for _, b := range buckets {
		sumOK += b.OK
		sumErr += b.Err
	}
	if sumOK != 2 || sumErr != 1 {
		t.Fatalf("bucket sums ok=%d err=%d, want 2/1 (n=%d bucketSecs=%v)", sumOK, sumErr, len(buckets), got["bucketSecs"])
	}

	fleet := got["fleet"].([]dashFleetRow)
	if len(fleet) != 2 {
		t.Fatalf("fleet rows = %+v", fleet)
	}
	if fleet[0].Key != "cline" || fleet[0].Requests != 2 || fleet[0].Success != 1 {
		t.Fatalf("fleet[0] = %+v", fleet[0])
	}
	if fleet[1].Key != "provider:openrouter" || fleet[1].Label != "openrouter" || fleet[1].Rate != 1 {
		t.Fatalf("fleet[1] = %+v", fleet[1])
	}
	var seriesSum int64
	for _, v := range fleet[0].Series {
		seriesSum += v
	}
	if seriesSum != 2 {
		t.Fatalf("fleet series sum = %d", seriesSum)
	}

	models := got["models"].([]dashModelRow)
	if len(models) != 2 || models[0].Model != "gpt-x" || models[0].Requests != 2 {
		t.Fatalf("models = %+v", models)
	}
	if models[1].Model != "gemini-y" || models[1].Tokens != 50 {
		t.Fatalf("models[1] = %+v", models[1])
	}

	rt := got["runtime"].(map[string]any)
	if rt["version"] != appVersion || rt["defaultModel"] != getDefaultModel() {
		t.Fatalf("runtime = %+v", rt)
	}
}

func TestDashboardOverviewBucketSizing(t *testing.T) {
	old := requestLogs
	requestLogsMu.Lock()
	requestLogs = nil
	requestLogsMu.Unlock()
	defer func() {
		requestLogsMu.Lock()
		requestLogs = old
		requestLogsMu.Unlock()
	}()

	for _, tc := range []struct {
		hours      int
		wantBucket int64
	}{
		{1, 120},      // 60/96 < 2 -> clamp 2 min
		{24, 900},     // 15 min
		{168, 3600},   // 105 min -> clamp 60 min
		{0, 900},      // defaults to 24h
		{10000, 3600}, // clamped to 168h
	} {
		got := dashboardOverview(tc.hours)
		if bs := got["bucketSecs"].(int64); bs != tc.wantBucket {
			t.Fatalf("hours=%d bucketSecs=%d, want %d", tc.hours, bs, tc.wantBucket)
		}
	}
	if got := dashboardOverview(0); got["windowHours"] != 24 {
		t.Fatalf("windowHours default = %v", got["windowHours"])
	}
	if got := dashboardOverview(10000); got["windowHours"] != 168 {
		t.Fatalf("windowHours clamp = %v", got["windowHours"])
	}
}
