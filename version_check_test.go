package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func resetVersionState(t *testing.T, url string) func() {
	t.Helper()
	oldURL, oldState, oldApp := versionCheckURL, versionState, appVersion
	versionState = &versionCheckCache{}
	versionCheckURL = url
	appVersion = "v1.0.0"
	return func() {
		versionCheckURL = oldURL
		versionState = oldState
		appVersion = oldApp
	}
}

func releaseServer(body string, status int, hits *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

func TestVersionCheckFetchAndCache(t *testing.T) {
	var hits int32
	srv := releaseServer(`{"tag_name":"v9.9.9","html_url":"u9","published_at":"p9"}`, 200, &hits)
	defer srv.Close()
	defer resetVersionState(t, srv.URL)()

	versionState.refresh(false)
	if hits != 1 || versionState.latest != "v9.9.9" || versionState.err != "" {
		t.Fatalf("first refresh: hits=%d latest=%q err=%q", hits, versionState.latest, versionState.err)
	}
	// 缓存新鲜 → 不再请求
	srv.Config.Handler = releaseHandler(`{"tag_name":"v8.0.0"}`, &hits)
	versionState.refresh(false)
	if hits != 1 {
		t.Fatalf("cached refresh should not hit upstream, hits=%d", hits)
	}
	// force → 重新拉取并覆盖
	versionState.refresh(true)
	if hits != 2 || versionState.latest != "v8.0.0" {
		t.Fatalf("forced refresh: hits=%d latest=%q", hits, versionState.latest)
	}
}

func releaseHandler(body string, hits *int32) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		w.Write([]byte(body))
	})
}

func TestVersionCheckErrorKeepsLastLatest(t *testing.T) {
	var hits int32
	srv := releaseServer(`{"tag_name":"v2.0.0"}`, 200, &hits)
	defer srv.Close()
	defer resetVersionState(t, srv.URL)()

	versionState.refresh(true)
	srv.Config.Handler = releaseHandler(`boom`, &hits) // 200 + 非法 JSON
	versionState.refresh(true)
	if versionState.latest != "v2.0.0" || versionState.err == "" {
		t.Fatalf("bad JSON should keep latest and record err: latest=%q err=%q", versionState.latest, versionState.err)
	}
}

func TestVersionSemverCompare(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v1.2.9", "v1.2.10", true},
		{"v1.2.10", "v1.2.9", false},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0", "v1.0.1", true},
		{"1.0.0", "1.1.0", true},
		{"dev", "v1.0.0", false},
		{"v1.0.0-rc1", "v1.0.0", false}, // 后缀剥离后同版本
		{"", "v1.0.0", false},
	}
	for _, c := range cases {
		if got := versionNewer(c.current, c.latest); got != c.want {
			t.Errorf("versionNewer(%q,%q)=%v want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestVersionCheckHandlerPayload(t *testing.T) {
	var hits int32
	srv := releaseServer(`{"tag_name":"v1.3.0","html_url":"h","published_at":"2026-01-01T00:00:00Z"}`, 200, &hits)
	defer srv.Close()
	defer resetVersionState(t, srv.URL)()

	rec := httptest.NewRecorder()
	handleAdminVersionCheck(rec, httptest.NewRequest("GET", "/admin/api/version-check", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Current         string `json:"current"`
			Latest          string `json:"latest"`
			UpdateAvailable bool   `json:"updateAvailable"`
			ReleaseURL      string `json:"releaseURL"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.Data.Current != "v1.0.0" || resp.Data.Latest != "v1.3.0" || !resp.Data.UpdateAvailable || resp.Data.ReleaseURL != "h" {
		t.Fatalf("payload mismatch: %+v", resp)
	}
}
