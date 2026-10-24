package deviceflow

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cline-go-proxy/internal/platform"
)

// JSONPathString resolves a dotted path (with numeric array indexes) in a
// decoded JSON object, returning "" when the path is missing or not a string.
func JSONPathString(body map[string]any, path string) string {
	var cur any = body
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			continue
		}
		if m, ok := cur.(map[string]any); ok {
			cur = m[seg]
			continue
		}
		if arr, ok := cur.([]any); ok {
			var idx int
			if _, err := fmt.Sscanf(seg, "%d", &idx); err == nil && idx >= 0 && idx < len(arr) {
				cur = arr[idx]
				continue
			}
		}
		return ""
	}
	if s, ok := cur.(string); ok {
		return s
	}
	return ""
}

// NewUUIDv4 generates a RFC 4122 v4 UUID (qoder nonce / machine_id shape).
func NewUUIDv4() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// NewPKCE returns an S256 verifier/challenge pair (RFC 7636).
func NewPKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// StatePollHeaders returns the fixed CodeBuddy-family request headers. X-Domain
// comes from the StateURL host; the rest mirror the upstream CLI constants.
func StatePollHeaders(spec Spec, isPoll bool) map[string]string {
	host := ""
	if u, e := url.Parse(spec.StateURL); e == nil {
		host = u.Host
	}
	h := map[string]string{
		"Accept":             "application/json",
		"User-Agent":         spec.UserAgent,
		"X-Requested-With":   "XMLHttpRequest",
		"X-Domain":           host,
		"X-No-Authorization": "true",
		"X-No-User-Id":       "true",
		"X-Product":          "SaaS",
	}
	if isPoll {
		h["X-No-Enterprise-Id"] = "true"
		h["X-No-Department-Info"] = "true"
	} else {
		h["Content-Type"] = "application/json"
	}
	return h
}

// QoderExpiryMs interprets Qoder's token expiry: numeric expires_at (seconds or
// milliseconds), RFC3339 string, or expires_in seconds. Qoder's refresh endpoint
// rejects device tokens, so short lifetimes are floored to one day; with no hint
// at all the upstream default is 30 days.
func QoderExpiryMs(data map[string]any) int64 {
	var ms int64
	switch v := data["expires_at"].(type) {
	case float64:
		ms = EpochToMs(int64(v))
	case string:
		if n, e := strconv.ParseInt(v, 10, 64); e == nil {
			ms = EpochToMs(n)
		} else if ts, e := time.Parse(time.RFC3339, v); e == nil {
			ms = ts.UnixMilli()
		}
	}
	if ms == 0 {
		if n, ok := data["expires_in"].(float64); ok {
			ms = time.Now().Add(time.Duration(n) * time.Second).UnixMilli()
		}
	}
	if ms == 0 {
		return time.Now().Add(30 * 24 * time.Hour).UnixMilli()
	}
	if floor := time.Now().Add(24 * time.Hour).UnixMilli(); ms < floor {
		return floor
	}
	return ms
}

// EpochToMs normalizes a unix timestamp in seconds or milliseconds to ms.
func EpochToMs(n int64) int64 {
	if n <= 0 {
		return 0
	}
	if n < 1_000_000_000_000 {
		return n * 1000
	}
	return n
}

// htmlBlockedError reports an HTML page where JSON was expected — typically a
// 405 SPA error page from a blocked/moved endpoint — with actionable hints.
func htmlBlockedError(resp *http.Response, body []byte) error {
	snippet := strings.ToLower(strings.TrimSpace(string(body)))
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html") && !strings.HasPrefix(snippet, "<") {
		return nil
	}
	return fmt.Errorf("upstream returned an HTML page (HTTP %d) instead of JSON — the login endpoint is likely blocked locally (hosts-file sinkhole / DNS) or has moved upstream; configure an egress proxy in Settings or use Token import", resp.StatusCode)
}

// decodeJSONBody parses an upstream auth response. On non-200 or unparseable
// bodies it returns an actionable error: HTML bodies get the blocked-endpoint
// hint, everything else keeps the status + body snippet.
func decodeJSONBody(op string, resp *http.Response, body []byte) (map[string]any, error) {
	if resp.StatusCode == http.StatusOK {
		var data map[string]any
		if err := json.Unmarshal(body, &data); err == nil {
			return data, nil
		}
		if htmlErr := htmlBlockedError(resp, body); htmlErr != nil {
			return nil, fmt.Errorf("%s decode: %v", op, htmlErr)
		}
		return nil, fmt.Errorf("%s decode: unexpected JSON payload", op)
	}
	if htmlErr := htmlBlockedError(resp, body); htmlErr != nil {
		return nil, fmt.Errorf("%s failed: %v", op, htmlErr)
	}
	return nil, fmt.Errorf("%s failed: %d %s", op, resp.StatusCode, platform.Truncate(strings.TrimSpace(string(body)), 200))
}

// dialHintError flags dial/lookup failures that point at local name resolution
// (e.g. a hosts-file 0.0.0.1 sinkhole) instead of leaving the raw Go error.
func dialHintError(op string, err error) error {
	msg := err.Error()
	if strings.Contains(msg, "dial tcp 0.0.0.1:") || strings.Contains(msg, "no such host") || strings.Contains(msg, "server misbehaving") {
		return fmt.Errorf("%s failed: %v — domain did not resolve (check /etc/hosts or Windows hosts for a block entry) or is unreachable without a proxy; configure the egress proxy in Settings to route this request", op, err)
	}
	return fmt.Errorf("%s failed: %v", op, err)
}
