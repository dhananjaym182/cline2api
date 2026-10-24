package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// isolateConnections resets the connection store + pool state to a temp dir.
func isolateConnections(t *testing.T) {
	t.Helper()
	oldConns, oldLoaded, oldPath := connections, connectionsLoaded, connectionsFilePath
	oldCursor, oldCooling := connCursor, connCooling
	connectionsFilePath = filepath.Join(t.TempDir(), ".cline-connections.json")
	connections, connectionsLoaded = nil, true
	connCursor = map[string]int{}
	connCooling = map[string]time.Time{}
	t.Cleanup(func() {
		connections, connectionsLoaded, connectionsFilePath = oldConns, oldLoaded, oldPath
		connCursor, connCooling = oldCursor, oldCooling
	})
}

func mkActiveConn(id, email, token string) *Connection {
	return &Connection{ID: id, Provider: "testprov", AuthType: "import_token",
		Email: email, AccessToken: token, Status: "active", CreatedAt: time.Now()}
}

func findConn(t *testing.T, id string) *Connection {
	t.Helper()
	for _, c := range connections {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("connection %s not in store", id)
	return nil
}

func TestConnectionPoolRotationAndSkip(t *testing.T) {
	isolateConnections(t)
	connections = []*Connection{mkActiveConn("c1", "a@x.io", "tok-c1"), mkActiveConn("c2", "b@x.io", "tok-c2")}
	p := &CustomProvider{ID: "p1", Name: "P", BaseURL: "http://127.0.0.1:1", ConnectionIDs: []string{"c1", "c2"}}

	if got := pickConnectionForProvider(p); got == nil || got.ID != "c1" {
		t.Fatalf("first pick = %+v, want c1", got)
	}
	if got := pickConnectionForProvider(p); got == nil || got.ID != "c2" {
		t.Fatalf("second pick = %+v, want c2 (round-robin)", got)
	}
	if got := pickConnectionForProvider(p); got == nil || got.ID != "c1" {
		t.Fatalf("third pick = %+v, want c1", got)
	}

	// reauth'd connection is skipped; the sibling keeps serving.
	findConn(t, "c2").Status = "reauth_required"
	for i := 0; i < 3; i++ {
		if got := pickConnectionForProvider(p); got == nil || got.ID != "c1" {
			t.Fatalf("pick %d with dead c2 = %+v, want c1", i, got)
		}
	}

	// rate-limit cooldown parks one connection for its duration.
	findConn(t, "c2").Status = "active"
	setConnectionCooldown("c1", time.Now().Add(connectionRateLimitCooldown))
	if got := pickConnectionForProvider(p); got == nil || got.ID != "c2" {
		t.Fatalf("pick with cooled c1 = %+v, want c2", got)
	}
	connCooling["c1"] = time.Now().Add(-time.Second) // expire it
	if got := pickConnectionForProvider(p); got == nil || got.ID != "c1" {
		t.Fatalf("pick after cooldown expiry = %+v, want c1", got)
	}
}

func TestConnectionPoolLegacyIDAndUnion(t *testing.T) {
	isolateConnections(t)
	connections = []*Connection{mkActiveConn("c1", "a@x.io", "t1"), mkActiveConn("c2", "b@x.io", "t2")}
	legacy := &CustomProvider{ID: "pL", ConnectionID: "c1"}
	if ids := providerConnIDs(legacy); len(ids) != 1 || ids[0] != "c1" {
		t.Fatalf("legacy alias ids = %v", ids)
	}
	if got := pickConnectionForProvider(legacy); got == nil || got.ID != "c1" {
		t.Fatalf("legacy pick = %+v", got)
	}
	both := &CustomProvider{ID: "pB", ConnectionIDs: []string{"c2"}, ConnectionID: "c1"}
	if ids := providerConnIDs(both); len(ids) != 2 {
		t.Fatalf("union ids = %v, want 2", ids)
	}
	if got := pickConnectionForProvider(both); got == nil {
		t.Fatal("union pick returned nil")
	}
	gone := &CustomProvider{ID: "pG", ConnectionID: "nope"}
	if got := pickConnectionForProvider(gone); got != nil {
		t.Fatal("missing connection must not be picked")
	}
}

func TestConnectionExpiryWithoutRefreshMarksReauth(t *testing.T) {
	isolateConnections(t)
	dead := mkActiveConn("c1", "a@x.io", "t1")
	dead.ExpiresAt = time.Now().Add(-time.Hour).UnixMilli()
	connections = []*Connection{dead, mkActiveConn("c2", "b@x.io", "t2")}
	p := &CustomProvider{ID: "p1", ConnectionIDs: []string{"c1", "c2"}}
	if got := pickConnectionForProvider(p); got == nil || got.ID != "c2" {
		t.Fatalf("pick = %+v, want c2", got)
	}
	if findConn(t, "c1").Status != "reauth_required" {
		t.Error("expired no-refresh connection should be marked reauth_required")
	}
	// expired WITH refresh token stays usable (refresh handled on the call path)
	live := mkActiveConn("c3", "c@x.io", "t3")
	live.ExpiresAt = time.Now().Add(-time.Hour).UnixMilli()
	live.RefreshToken = "r"
	p2 := &CustomProvider{ID: "p2", ConnectionIDs: []string{"c3"}}
	connections = append(connections, live)
	if got := pickConnectionForProvider(p2); got == nil || got.ID != "c3" {
		t.Fatalf("refreshable connection should stay pickable, got %+v", got)
	}
}

func TestRecordConnectionUsageRoundTrip(t *testing.T) {
	isolateConnections(t)
	connections = []*Connection{mkActiveConn("c1", "a@x.io", "t1")}
	recordConnectionUsage("c1", "m:free", tokenUsage{Valid: true, Prompt: 10, Completion: 5, Total: 15, Cached: 2})
	recordConnectionUsage("c1", "m-paid", tokenUsage{Valid: true, Prompt: 1, Completion: 1, Total: 2})
	recordConnectionUsage("c1", "", tokenUsage{}) // no model: totals still count
	c := findConn(t, "c1")
	if c.UsageCount != 3 || c.TotalTokens != 17 || c.CachedTokens != 2 {
		t.Fatalf("totals wrong: %+v", c)
	}
	if c.ModelStats["m:free"].UsageCount != 1 || c.ModelStats["m:free"].Cost != "free" {
		t.Errorf("m:free stat wrong: %+v", c.ModelStats["m:free"])
	}
	if c.ModelStats["m-paid"].Cost != "paid" {
		t.Errorf("paid classification wrong: %+v", c.ModelStats["m-paid"])
	}
	// persistence round-trip through the store file
	connections, connectionsLoaded = nil, false
	loadConnections()
	c = findConn(t, "c1")
	if c.UsageCount != 3 || len(c.ModelStats) != 2 {
		t.Fatalf("stats lost after reload: %+v", c)
	}
}

func TestUpsertConnectionPreservesStats(t *testing.T) {
	isolateConnections(t)
	old := mkActiveConn("c1", "a@x.io", "t1")
	old.UsageCount, old.TotalTokens = 7, 100
	old.ModelStats = map[string]*ModelStat{"m": {ModelID: "m", UsageCount: 7}}
	connections = []*Connection{old}
	re := upsertConnection(&Connection{Provider: "testprov", Email: "a@x.io", AccessToken: "t1-new", AuthType: "device_code", Status: "active"})
	if re.ID != "c1" || re.UsageCount != 7 || re.TotalTokens != 100 || re.ModelStats["m"] == nil {
		t.Fatalf("re-login upsert lost usage history: %+v", re)
	}
}

func TestCallProviderConnFailoverAndTag(t *testing.T) {
	isolateConnections(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer tok-c1":
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"bad token"}}`))
		case "Bearer tok-c2":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"ok\"}}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":4,\"total_tokens\":7}}\n"))
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()
	connections = []*Connection{mkActiveConn("c1", "a@x.io", "tok-c1"), mkActiveConn("c2", "b@x.io", "tok-c2")}
	installProviders(t, &CustomProvider{ID: "fp1", Name: "FailProv", BaseURL: srv.URL, Enabled: true,
		ConnectionIDs: []string{"c1", "c2"}, ModelIDs: []string{"x"}})

	params := map[string]any{"model": "x", "messages": []any{}}
	resp, err := callProvider(&CustomProvider{ID: "fp1", Name: "FailProv", BaseURL: srv.URL, ConnectionIDs: []string{"c1", "c2"}}, params, false)
	if err != nil {
		t.Fatalf("want failover to c2, got err: %v", err)
	}
	conn := responseConnection(resp)
	if conn == nil || conn.ID != "c2" {
		t.Fatalf("response not tagged with c2: %+v", conn)
	}
	if findConn(t, "c1").Status != "reauth_required" {
		t.Error("401 conn should be marked reauth, sibling keeps serving")
	}
	if findConn(t, "c2").Status != "active" {
		t.Error("working conn must stay active")
	}
	resp.Body.Close()

	// recordUsage attributes stats to the tagged connection, not any account.
	rl := RequestLog{Model: "x"}
	usage := tokenUsage{Valid: true, Prompt: 3, Completion: 4, Total: 7}
	resp2, err := callProvider(&CustomProvider{ID: "fp1", Name: "FailProv", BaseURL: srv.URL, ConnectionIDs: []string{"c2"}}, params, false)
	if err != nil {
		t.Fatal(err)
	}
	recordUsage(nil, resp2, &rl, "x", usage)
	if rl.ConnectionID != "c2" || rl.ConnectionEmail != "b@x.io" {
		t.Errorf("reqLog stamp = %+v", rl)
	}
	if c2 := findConn(t, "c2"); c2.UsageCount != 1 || c2.ModelStats["x"].TotalTokens != 7 {
		t.Errorf("conn stats wrong: %+v", c2)
	}
	resp2.Body.Close()
}

func TestCallProviderRotatesAcrossConnections(t *testing.T) {
	isolateConnections(t)
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"ok\"}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n"))
	}))
	defer srv.Close()
	connections = []*Connection{mkActiveConn("c1", "a@x.io", "tok-c1"), mkActiveConn("c2", "b@x.io", "tok-c2")}
	installProviders(t, &CustomProvider{ID: "rp1", Name: "RotProv", BaseURL: srv.URL, Enabled: true,
		ConnectionIDs: []string{"c1", "c2"}, ModelIDs: []string{"x"}})
	p := &CustomProvider{ID: "rp1", Name: "RotProv", BaseURL: srv.URL, ConnectionIDs: []string{"c1", "c2"}}
	params := map[string]any{"model": "x", "messages": []any{}}
	want := []string{"Bearer tok-c1", "Bearer tok-c2", "Bearer tok-c1", "Bearer tok-c2"}
	for i := range want {
		resp, err := callProvider(p, params, false)
		if err != nil {
			t.Fatalf("dispatch %d: %v", i, err)
		}
		tag := responseConnection(resp)
		wantID := []string{"c1", "c2"}[i%2]
		if tag == nil || tag.ID != wantID {
			t.Errorf("dispatch %d tagged %+v, want %s", i, tag, wantID)
		}
		rl := RequestLog{Model: "x"}
		recordUsage(nil, resp, &rl, "x", tokenUsage{Valid: true, Prompt: 1, Completion: 1, Total: 2})
		resp.Body.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("upstream auth order = %v, want alternation %v", got, want)
	}
	if c1, c2 := findConn(t, "c1"), findConn(t, "c2"); c1.UsageCount != 2 || c2.UsageCount != 2 {
		t.Errorf("usage split c1=%d c2=%d, want 2/2", c1.UsageCount, c2.UsageCount)
	}
}

func TestCallProviderAllConnsDeadFailsOnce(t *testing.T) {
	isolateConnections(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	connections = []*Connection{mkActiveConn("c1", "a@x.io", "t1")}
	p := &CustomProvider{ID: "d1", Name: "D", BaseURL: srv.URL, ConnectionIDs: []string{"c1"}}
	if _, err := callProvider(p, map[string]any{"model": "x"}, false); err == nil {
		t.Fatal("single dead conn should fail")
	}
	if findConn(t, "c1").Status != "reauth_required" {
		t.Error("final 401 should still mark reauth")
	}
	// now no usable connections left at all: fail fast, request never sent
	if _, err := callProvider(p, map[string]any{"model": "x"}, false); err == nil {
		t.Fatal("want connection unavailable error")
	}
}

func TestRemoveConnectionPrunesProviders(t *testing.T) {
	isolateConnections(t)
	connections = []*Connection{mkActiveConn("c1", "a@x.io", "t1"), mkActiveConn("c2", "b@x.io", "t2")}
	installProviders(t, &CustomProvider{ID: "pp1", Name: "Pr", ConnectionIDs: []string{"c1", "c2"}, ConnectionID: "c2"})

	if !removeConnection("c2") {
		t.Fatal("remove failed")
	}
	loadProviders()
	providersMu.Lock()
	p := customProviders[0]
	providersMu.Unlock()
	if p.ConnectionID != "" {
		t.Errorf("legacy field not pruned: %q", p.ConnectionID)
	}
	if len(p.ConnectionIDs) != 1 || p.ConnectionIDs[0] != "c1" {
		t.Errorf("pool not pruned: %v", p.ConnectionIDs)
	}
}
