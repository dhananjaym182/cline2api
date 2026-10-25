package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GitHub Release 更新检查：结果缓存 6 小时，?force=1 强制刷新。
// 「关于」页读取本接口显示 current/latest/是否可更新。

var versionCheckURL = "https://api.github.com/repos/luawei1/cline2api/releases/latest"

const versionCheckTTL = 6 * time.Hour

type versionCheckCache struct {
	mu        sync.Mutex
	latest    string
	htmlURL   string
	published string
	err       string
	checkedAt time.Time
}

var versionState = &versionCheckCache{}

type githubReleaseJSON struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
}

// refresh 拉取最新版本信息并更新缓存；force=false 且缓存新鲜时直接返回。
func (v *versionCheckCache) refresh(force bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !force && !v.checkedAt.IsZero() && time.Since(v.checkedAt) < versionCheckTTL {
		return
	}
	v.checkedAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, versionCheckURL, nil)
	if err != nil {
		v.recordFetchError(err)
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := httpClient.Do(req)
	if err != nil {
		v.recordFetchError(err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		v.recordFetchError(errStatus(resp.StatusCode))
		return
	}
	var rel githubReleaseJSON
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		v.recordFetchError(err)
		return
	}
	v.err = ""
	v.latest = strings.TrimSpace(rel.TagName)
	v.htmlURL = rel.HTMLURL
	v.published = rel.PublishedAt
}

// 失败也做短缓存语义：保留旧 latest，仅记录错误，避免错误信息覆盖历史结果。
func (v *versionCheckCache) recordFetchError(err error) {
	v.err = err.Error()
}

type statusError int

func errStatus(code int) error { return statusError(code) }

func (e statusError) Error() string { return "version check upstream status " + strconv.Itoa(int(e)) }

// parseSemver 解析 v1.2.3（可带 -rc 后缀，后缀忽略），返回数值三元组。
func parseSemver(s string) ([3]int, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || parts[0] == "" {
		return [3]int{}, false
	}
	var out [3]int
	for i := 0; i < 3; i++ {
		if i >= len(parts) {
			continue
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// versionNewer 判断 latest 是否比 current 新；current 非 semver（如 dev）不提示更新。
func versionNewer(current, latest string) bool {
	c, ok := parseSemver(current)
	if !ok {
		return false
	}
	l, ok := parseSemver(latest)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func handleAdminVersionCheck(w http.ResponseWriter, r *http.Request) {
	versionState.refresh(r.URL.Query().Get("force") == "1")
	versionState.mu.Lock()
	defer versionState.mu.Unlock()
	data := map[string]any{
		"current":         appVersion,
		"latest":          versionState.latest,
		"releaseURL":      versionState.htmlURL,
		"publishedAt":     versionState.published,
		"checkedAt":       versionState.checkedAt.UnixMilli(),
		"updateAvailable": versionState.latest != "" && versionNewer(appVersion, versionState.latest),
	}
	if versionState.err != "" {
		data["error"] = versionState.err
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
}
