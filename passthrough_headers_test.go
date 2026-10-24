package main

// Tests for the passthrough-header filter and the client request body cap
// (task #67). Both are opt-in via ProxyConfig and must be no-ops by default.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func installPassthroughConfig(t *testing.T, enabled bool) {
	t.Helper()
	cfg := defaultProxyConfig()
	cfg.PassthroughHeaders = enabled
	cfg.Normalize()
	setProxyConfig(cfg)
	t.Cleanup(func() { setProxyConfig(defaultProxyConfig()) })
}

func TestCopyPassthroughHeadersDisabledByDefault(t *testing.T) {
	installPassthroughConfig(t, false)
	rec := httptest.NewRecorder()
	up := http.Header{}
	up.Set("X-Ratelimit-Requests", "42")
	copyPassthroughHeaders(rec, up)
	if rec.Header().Get("X-Ratelimit-Requests") != "" {
		t.Fatal("must not copy headers when disabled")
	}
}

func TestCopyPassthroughHeadersCopiesSafeBlocksUnsafe(t *testing.T) {
	installPassthroughConfig(t, true)
	rec := httptest.NewRecorder()
	up := http.Header{}
	up.Set("X-Ratelimit-Requests", "42")
	up.Set("X-Request-Id", "req-1")
	up.Set("Retry-After", "30")
	up.Set("Content-Type", "application/json")
	up.Set("Content-Length", "123")
	up.Set("Transfer-Encoding", "chunked")
	up.Set("Set-Cookie", "session=abc")
	up.Set("Authorization", "Bearer x")
	up.Add("X-Multi", "a")
	up.Add("X-Multi", "b")
	up.Set("X-Huge", strings.Repeat("z", 600))
	up.Set("X-Api-Key", "secret")
	up.Set("Access-Control-Allow-Origin", "*")
	copyPassthroughHeaders(rec, up)

	h := rec.Header()
	for want, val := range map[string]string{
		"X-Ratelimit-Requests": "42",
		"X-Request-Id":         "req-1",
		"Retry-After":          "30",
	} {
		if h.Get(want) != val {
			t.Fatalf("%s = %q, want %q", want, h.Get(want), val)
		}
	}
	if len(h.Values("X-Multi")) != 2 {
		t.Fatalf("multi-value header must be fully copied, got %v", h.Values("X-Multi"))
	}
	for _, blocked := range []string{
		"Content-Type", "Content-Length", "Transfer-Encoding",
		"Set-Cookie", "Authorization", "X-Huge", "X-Api-Key",
		"Access-Control-Allow-Origin",
	} {
		if h.Get(blocked) != "" {
			t.Fatalf("unsafe header %s must not be copied", blocked)
		}
	}
}

func TestCopyPassthroughHeadersNilUpstream(t *testing.T) {
	installPassthroughConfig(t, true)
	rec := httptest.NewRecorder()
	copyPassthroughHeaders(rec, nil) // must not panic
}

func TestWithBodyLimitRejectsOversizedBody(t *testing.T) {
	cfg := defaultProxyConfig()
	cfg.MaxRequestBodyBytes = 16
	cfg.Normalize()
	setProxyConfig(cfg)
	t.Cleanup(func() { setProxyConfig(defaultProxyConfig()) })

	var readErr error
	h := withBodyLimit(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		if readErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(readErr.Error()))
		}
	})

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(strings.Repeat("a", 64)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "too large") {
		t.Fatalf("oversized body must fail the read: status=%d body=%q", resp.StatusCode, body)
	}

	// Within limit: reads fine.
	resp2, err := http.Post(srv.URL, "application/json", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK || readErr != nil {
		t.Fatalf("small body must pass: status=%d err=%v", resp2.StatusCode, readErr)
	}
}

func TestWithBodyLimitDisabledPassesEverything(t *testing.T) {
	installPassthroughConfig(t, false) // MaxRequestBodyBytes zero
	big := strings.Repeat("b", 1<<20)
	var got string
	h := withBodyLimit(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if len(got) != len(big) {
		t.Fatalf("disabled limit must pass full body, got %d bytes", len(got))
	}
}
