package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestSplitProviderPin(t *testing.T) {
	cases := []struct{ in, bare, pin string }{
		{"deepseek-chat@MyProvider", "deepseek-chat", "MyProvider"},
		{"org/model:free@KeyP", "org/model:free", "KeyP"},
		{"a@b@c", "a@b", "c"},
		{"plain/model", "plain/model", ""},
		{"@leading", "@leading", ""},
		{"trail@", "trail@", ""},
		{"x@ ", "x@ ", ""},
	}
	for _, c := range cases {
		bare, pin := splitProviderPin(c.in)
		if bare != c.bare || pin != c.pin {
			t.Errorf("splitProviderPin(%q) = %q/%q want %q/%q", c.in, bare, pin, c.bare, c.pin)
		}
	}
}

// Two providers serve the same model id. A pinned request must hit exactly the
// named provider, stamp attribution, and never touch the sibling; pinned
// failures must not fall back; an unknown pin must fail without upstream calls.
func TestCallClineAPIPinnedProvider(t *testing.T) {
	isolateConnections(t)
	var hitsA, hitsB int32
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitsA, 1)
		w.Write([]byte(`{"choices":[{"message":{"content":"from-A"}}],"usage":{"total_tokens":1}}`))
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitsB, 1)
		w.Write([]byte(`{"choices":[{"message":{"content":"from-B"}}],"usage":{"total_tokens":1}}`))
	}))
	defer srvB.Close()
	srvDown := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitsB, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srvDown.Close()

	installProviders(t,
		&CustomProvider{ID: "pA", Name: "Alpha", BaseURL: srvA.URL, APIKey: "ka", Enabled: true, Priority: 0, ModelIDs: []string{"shared/x"}},
		&CustomProvider{ID: "pB", Name: "Beta", BaseURL: srvB.URL, APIKey: "kb", Enabled: true, Priority: 5, ModelIDs: []string{"shared/x"}},
	)

	params := map[string]any{"model": "shared/x@Beta", "messages": []any{}}
	resp, acc, err := callClineAPI(params, false)
	if err != nil {
		t.Fatalf("pinned request failed: %v", err)
	}
	defer resp.Body.Close()
	if acc != nil {
		t.Error("pinned provider path must not bind a Cline account")
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"choices":[{"message":{"content":"from-B"}}],"usage":{"total_tokens":1}}` {
		t.Errorf("wrong upstream served: %s", body)
	}
	if atomic.LoadInt32(&hitsA) != 0 {
		t.Error("pin to Beta must never touch Alpha")
	}
	if got, _ := params["model"].(string); got != "shared/x" {
		t.Errorf("model should be rewritten to bare id for logging/upstream, got %q", got)
	}
	if pn, _ := params[providerNameParam].(string); pn != "Beta" {
		t.Errorf("provider name attribution = %q", pn)
	}
	if sb, _ := params[servedByParam].(string); sb != upstreamProvider {
		t.Errorf("served-by = %q", sb)
	}

	// Unknown pin: loud error, zero upstream dispatch.
	_, _, err = callClineAPI(map[string]any{"model": "shared/x@Gamma", "messages": []any{}}, false)
	if err == nil {
		t.Fatal("unknown provider pin should error")
	}

	// Pinned failure must NOT fall back to the sibling provider.
	atomic.StoreInt32(&hitsA, 0)
	atomic.StoreInt32(&hitsB, 0)
	installProviders(t,
		&CustomProvider{ID: "pA", Name: "Alpha", BaseURL: srvA.URL, APIKey: "ka", Enabled: true, Priority: 0, ModelIDs: []string{"shared/x"}},
		&CustomProvider{ID: "pB", Name: "Beta", BaseURL: srvDown.URL, APIKey: "kb", Enabled: true, Priority: 5, ModelIDs: []string{"shared/x"}},
	)
	_, _, err = callClineAPI(map[string]any{"model": "shared/x@Beta", "messages": []any{}}, false)
	if err == nil {
		t.Fatal("pinned 5xx should surface as error")
	}
	if atomic.LoadInt32(&hitsA) != 0 {
		t.Error("pinned failure must not fall back to Alpha")
	}

	// Unpinned stays priority-ordered (Alpha first) — pin must not change defaults.
	atomic.StoreInt32(&hitsA, 0)
	resp2, _, err2 := callClineAPI(map[string]any{"model": "shared/x", "messages": []any{}}, false)
	if err2 != nil {
		t.Fatalf("unpinned request failed: %v", err2)
	}
	resp2.Body.Close()
	if atomic.LoadInt32(&hitsA) == 0 {
		t.Error("unpinned request should use the priority-first provider")
	}
}
