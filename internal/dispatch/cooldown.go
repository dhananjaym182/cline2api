// Package dispatch holds routing policy helpers: retry classification, cooldown
// durations, and fallback decisions. It performs no HTTP writes and owns no
// account storage.
package dispatch

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// RateLimitedByBody reports explicit quota/rate-limit keywords. It distinguishes
// a rerouteable 421 from a real quota problem.
func RateLimitedByBody(body string) bool {
	low := strings.ToLower(body)
	for _, kw := range []string{"quota", "rate limit", "too many requests", "resourceexhausted", "insufficient", "billing", "payment"} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

// IsRateLimited classifies upstream responses that should trigger retry/backoff
// rather than an immediate user-visible failure.
func IsRateLimited(status int, body string) bool {
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		return true
	}
	if status == http.StatusBadGateway || status == http.StatusForbidden {
		low := strings.ToLower(body)
		for _, kw := range []string{"resourceexhausted", "limit reached", "rate limit", "too many", "overloaded", "busy"} {
			if strings.Contains(low, kw) {
				return true
			}
		}
	}
	return false
}

// ParseRetryAfter parses a Retry-After header (seconds or HTTP date). It is
// relative to time.Now; use ParseRetryAfterAt for deterministic callers.
func ParseRetryAfter(h string) time.Duration {
	return ParseRetryAfterAt(h, time.Now())
}

// ParseRetryAfterAt parses Retry-After relative to now. Invalid values return 0.
func ParseRetryAfterAt(h string, now time.Time) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	var secs int
	if _, err := fmt.Sscanf(h, "%d", &secs); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

// ProviderCooldownUntil decides how long a provider:model pair should cool down
// after an upstream error. A zero time means no cooldown.
//
// Configuration errors (400/401/403) get a long cooldown because retrying cannot
// heal them. 421 without quota keywords is not cooled: another provider/proxy
// node can usually serve it immediately. 429 honors Retry-After (capped at 30m)
// and defaults to 2m. 5xx errors get a short cooldown.
func ProviderCooldownUntil(status int, retryAfterHeader, body string) time.Time {
	return ProviderCooldownUntilAt(status, retryAfterHeader, body, time.Now())
}

// ProviderCooldownUntilAt is ProviderCooldownUntil with an injected clock.
func ProviderCooldownUntilAt(status int, retryAfterHeader, body string, now time.Time) time.Time {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return now.Add(30 * time.Minute)
	case http.StatusMisdirectedRequest: // 421
		if RateLimitedByBody(body) {
			return now.Add(30 * time.Second)
		}
		return time.Time{}
	case http.StatusTooManyRequests:
		ra := ParseRetryAfterAt(retryAfterHeader, now)
		if ra <= 0 {
			ra = 2 * time.Minute
		}
		if ra > 30*time.Minute {
			ra = 30 * time.Minute
		}
		return now.Add(ra)
	}
	if status >= 500 && status < 600 {
		return now.Add(30 * time.Second)
	}
	return time.Time{}
}
