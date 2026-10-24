package dispatch

import (
	"net/http"
	"testing"
	"time"
)

func TestRateLimitedByBody(t *testing.T) {
	for _, body := range []string{"Quota exceeded", "RATE LIMIT", "insufficient funds", "billing issue", "payment required", "ResourceExhausted"} {
		if !RateLimitedByBody(body) {
			t.Fatalf("expected limited: %q", body)
		}
	}
	if RateLimitedByBody("model not found") {
		t.Fatal("unexpected limit")
	}
}

func TestIsRateLimited(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   bool
	}{
		{http.StatusTooManyRequests, "", true},
		{http.StatusServiceUnavailable, "", true},
		{http.StatusBadGateway, "overloaded", true},
		{http.StatusForbidden, "busy", true},
		{http.StatusBadGateway, "bad gateway", false},
		{http.StatusForbidden, "forbidden", false},
		{http.StatusOK, "rate limit", false},
	}
	for _, tc := range cases {
		if got := IsRateLimited(tc.status, tc.body); got != tc.want {
			t.Fatalf("status=%d body=%q got=%v want=%v", tc.status, tc.body, got, tc.want)
		}
	}
}

func TestParseRetryAfterAt(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if got := ParseRetryAfterAt(" 30 ", now); got != 30*time.Second {
		t.Fatalf("seconds = %s", got)
	}
	date := now.Add(45 * time.Second).Format(http.TimeFormat)
	if got := ParseRetryAfterAt(date, now); got != 45*time.Second {
		t.Fatalf("date = %s", got)
	}
	if got := ParseRetryAfterAt("", now); got != 0 {
		t.Fatalf("empty = %s", got)
	}
	if got := ParseRetryAfterAt("0", now); got != 0 {
		t.Fatalf("zero = %s", got)
	}
	if got := ParseRetryAfterAt("garbage", now); got != 0 {
		t.Fatalf("garbage = %s", got)
	}
	past := now.Add(-time.Minute).Format(http.TimeFormat)
	if got := ParseRetryAfterAt(past, now); got != 0 {
		t.Fatalf("past = %s", got)
	}
}

func TestProviderCooldownUntilAt(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		status     int
		retryAfter string
		body       string
		want       time.Duration
	}{
		{"bad request", http.StatusBadRequest, "", "", 30 * time.Minute},
		{"unauthorized", http.StatusUnauthorized, "", "", 30 * time.Minute},
		{"forbidden", http.StatusForbidden, "", "", 30 * time.Minute},
		{"421 reroute", http.StatusMisdirectedRequest, "", "wrong edge", 0},
		{"421 quota", http.StatusMisdirectedRequest, "", "quota exceeded", 30 * time.Second},
		{"429 default", http.StatusTooManyRequests, "", "", 2 * time.Minute},
		{"429 retry-after", http.StatusTooManyRequests, "90", "", 90 * time.Second},
		{"429 capped", http.StatusTooManyRequests, "7200", "", 30 * time.Minute},
		{"5xx", http.StatusInternalServerError, "", "", 30 * time.Second},
		{"ok", http.StatusOK, "", "", 0},
	}
	for _, tc := range cases {
		got := ProviderCooldownUntilAt(tc.status, tc.retryAfter, tc.body, now)
		if tc.want == 0 {
			if !got.IsZero() {
				t.Fatalf("%s: got %s want zero", tc.name, got)
			}
			continue
		}
		if want := now.Add(tc.want); !got.Equal(want) {
			t.Fatalf("%s: got %s want %s", tc.name, got, want)
		}
	}
}
