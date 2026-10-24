package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func strKeys(keys ...string) func() []string {
	return func() []string { return keys }
}

func unauthorized(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusUnauthorized)
}

func TestAPIKeyAuthOpenWhenNoKeys(t *testing.T) {
	auth := &APIKeyAuth{Keys: strKeys(), Unauthorized: unauthorized}
	called := false
	h := auth.Wrap(func(w http.ResponseWriter, r *http.Request) { called = true })
	h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if !called {
		t.Fatal("expected next handler")
	}
}

func TestAPIKeyAuthAcceptsBothHeaders(t *testing.T) {
	auth := &APIKeyAuth{Keys: strKeys("sk-1"), Unauthorized: unauthorized}
	for _, tc := range []struct {
		name  string
		setup func(*http.Request)
	}{
		{"x-api-key", func(r *http.Request) { r.Header.Set("x-api-key", "sk-1") }},
		{"bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer sk-1") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			tc.setup(req)
			auth.Wrap(func(w http.ResponseWriter, r *http.Request) { called = true })(httptest.NewRecorder(), req)
			if !called {
				t.Fatal("expected next handler")
			}
		})
	}
}

func TestAPIKeyAuthRejectsBadKey(t *testing.T) {
	auth := &APIKeyAuth{Keys: strKeys("sk-1"), Unauthorized: unauthorized}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("x-api-key", "nope")
	auth.Wrap(func(w http.ResponseWriter, r *http.Request) { t.Fatal("next must not run") })(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestAPIKeyAuthSetsCORS(t *testing.T) {
	auth := &APIKeyAuth{Keys: strKeys(), Unauthorized: unauthorized}
	rec := httptest.NewRecorder()
	auth.Wrap(func(w http.ResponseWriter, r *http.Request) {})(rec, httptest.NewRequest(http.MethodOptions, "/v1/models", nil))
	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("missing CORS header")
	}
}

func TestExtractKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if ExtractKey(r) != "" {
		t.Fatal("expected empty")
	}
	r.Header.Set("Authorization", "Bearer abc")
	if ExtractKey(r) != "abc" {
		t.Fatalf("bearer = %q", ExtractKey(r))
	}
	r.Header.Set("x-api-key", "xyz")
	if ExtractKey(r) != "xyz" {
		t.Fatalf("x-api-key should win: %q", ExtractKey(r))
	}
}

func TestHealthHandler(t *testing.T) {
	h := Health{Version: func() string { return "1.2.3" }, ActiveAccounts: func() int { return 7 }}
	rec := httptest.NewRecorder()
	h.Handler(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`"status":"ok"`, `"version":"1.2.3"`, `"activeAccounts":7`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body %s missing %s", body, want)
		}
	}
}
