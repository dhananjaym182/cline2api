package main

// ============================================================================
// Wave-4 §A: per-provider connection pooling + connection-scoped usage stats.
//
// Two capabilities live here:
//  1. Usage attribution: provider traffic previously called recordTokenUsage
//     with a nil account (no-op). Now every served response carries the
//     Connection that authenticated it, so per-model token/request stats are
//     recorded ON the connection instead of polluting Cline accounts.
//  2. Pooling: a CustomProvider may bind several connections
//     (ConnectionIDs). Requests rotate round-robin over the usable ones; a
//     connection killed by 401/403 (reauth) or 429 (timed cooldown) is skipped
//     while its siblings keep serving. Providers with the legacy single
//     ConnectionID keep working through the read-path alias in
//     providerConnIDs — no data migration.
// ============================================================================

import (
	"io"
	"net/http"
	"sync"
	"time"
)

// connPoolMu guards the round-robin cursors and the in-memory cooldown table.
// Kept separate from connectionsMu (the store lock) to avoid lock-ordering
// entanglement with pickers that also touch the store.
var connPoolMu sync.Mutex

// connCursor providerID -> next candidate index (round-robin position).
var connCursor = map[string]int{}

// connCooling connID -> cooldown expiry. In-memory only: a restart simply
// re-probes connections, mirroring provRecheckMiss policy for transient state.
var connCooling = map[string]time.Time{}

const connectionRateLimitCooldown = 2 * time.Minute

// providerConnIDs returns the connection IDs bound to a provider:
// ConnectionIDs when present, else the legacy single ConnectionID.
// Unknown/duplicate IDs are tolerated here (handleProviderSave prunes them).
func providerConnIDs(p *CustomProvider) []string {
	var out []string
	seen := map[string]bool{}
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range p.ConnectionIDs {
		add(id)
	}
	add(p.ConnectionID)
	return out
}

// connectionUsableLocked checks one stored connection; caller holds
// connectionsMu. Mirrors the historical activeConnectionForProvider semantics:
// expired without a refresh token -> marked reauth_required (persisted) and
// skipped; expired WITH a refresh token -> still returned (refresh is handled
// by connectionBearer/token exchange on the call path).
func connectionUsableLocked(c *Connection, now time.Time) bool {
	if c == nil || c.Status != "active" {
		return false
	}
	if c.ExpiresAt > 0 && now.UnixMilli() >= c.ExpiresAt && c.RefreshToken == "" {
		c.Status = "reauth_required"
		saveConnectionsLocked()
		return false
	}
	connPoolMu.Lock()
	defer connPoolMu.Unlock()
	if until, cool := connCooling[c.ID]; cool && now.Before(until) {
		return false
	}
	return true
}

// usableConnectionsForProvider snapshots all currently usable connections of
// a provider in rotation order (starting at the provider's cursor). Copies
// are returned; callers never mutate the store through them.
func usableConnectionsForProvider(p *CustomProvider) []*Connection {
	ids := providerConnIDs(p)
	if len(ids) == 0 {
		return nil
	}
	loadConnections()
	now := time.Now()
	connectionsMu.Lock()
	defer connectionsMu.Unlock()
	connPoolMu.Lock()
	start := connCursor[p.ID] % len(ids)
	connPoolMu.Unlock()
	out := make([]*Connection, 0, len(ids))
	for i := 0; i < len(ids); i++ {
		id := ids[(start+i)%len(ids)]
		for _, c := range connections {
			if c.ID != id {
				continue
			}
			if !connectionUsableLocked(c, now) {
				break
			}
			cp := *c
			out = append(out, &cp)
			break
		}
	}
	return out
}

// hasUsableConnection reports whether at least one bound connection is
// currently usable WITHOUT advancing the rotation cursor. Use for pure
// availability gates; only real dispatch may consume a cursor slot.
func hasUsableConnection(p *CustomProvider) bool {
	return len(usableConnectionsForProvider(p)) > 0
}

// advanceConnCursor moves the provider cursor past connID in the full id list,
// so a cooled-down entry does not stall rotation for its siblings.
func advanceConnCursor(p *CustomProvider, connID string) {
	ids := providerConnIDs(p)
	if len(ids) == 0 {
		return
	}
	connPoolMu.Lock()
	defer connPoolMu.Unlock()
	for i, id := range ids {
		if id == connID {
			connCursor[p.ID] = (i + 1) % len(ids)
			return
		}
	}
}

// pickConnectionForProvider returns the next usable connection (advancing the
// round-robin cursor), or nil when the provider binds connections but none is
// usable. Replaces activeConnectionForProvider on the call path.
func pickConnectionForProvider(p *CustomProvider) *Connection {
	conns := usableConnectionsForProvider(p)
	if len(conns) == 0 {
		if len(providerConnIDs(p)) > 0 {
			// All bound connections are dead: remember nothing (do not advance
			// a cursor over an empty set) but leave cooldowns intact.
			return nil
		}
		return nil
	}
	chosen := conns[0]
	advanceConnCursor(p, chosen.ID)
	return chosen
}

// setConnectionCooldown parks one connection for rate-limit recovery (429).
// Auth failures (401/403) use markConnectionReauth instead — they need a human.
func setConnectionCooldown(connID string, until time.Time) {
	connPoolMu.Lock()
	connCooling[connID] = until
	connPoolMu.Unlock()
}

// connectionCooldownActive reports whether a connection is currently parked.
func connectionCooldownActive(connID string) bool {
	connPoolMu.Lock()
	defer connPoolMu.Unlock()
	until, ok := connCooling[connID]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(connCooling, connID)
		return false
	}
	return true
}

// connectionExists helper for save-time validation of bound connection IDs.
func connectionExists(id string) bool {
	loadConnections()
	connectionsMu.Lock()
	defer connectionsMu.Unlock()
	for _, c := range connections {
		if c.ID == id {
			return true
		}
	}
	return false
}

// pruneConnectionRefs drops a deleted connection ID from every provider's
// binding list (both pool and legacy single field). Called by removeConnection.
func pruneConnectionRefs(connID string) {
	loadProviders()
	providersMu.Lock()
	defer providersMu.Unlock()
	changed := false
	for _, p := range customProviders {
		if p.ConnectionID == connID {
			p.ConnectionID = ""
			changed = true
		}
		if len(p.ConnectionIDs) > 0 {
			kept := p.ConnectionIDs[:0]
			for _, id := range p.ConnectionIDs {
				if id != connID {
					kept = append(kept, id)
				}
			}
			if len(kept) != len(p.ConnectionIDs) {
				p.ConnectionIDs = kept
				changed = true
			}
		}
	}
	if changed {
		saveProvidersLocked()
	}
	// A vanished connection must not keep a cursor pointing out of range;
	// modulo arithmetic already tolerates that, so nothing else to reset.
}

// ---------- connection-scoped usage recording ----------

// recordConnectionUsage adds request/token counters to a connection, keyed per
// model. Unlike Account.ModelStats (free models only, a Cline quota concern),
// connection stats cover EVERY model: the whole point is showing which models
// each logged-in upstream account actually served.
func recordConnectionUsage(connID, model string, usage tokenUsage) {
	if connID == "" {
		return
	}
	loadConnections()
	connectionsMu.Lock()
	defer connectionsMu.Unlock()
	for _, c := range connections {
		if c.ID != connID {
			continue
		}
		c.UsageCount++
		c.PromptTokens += usage.Prompt
		c.CompletionTokens += usage.Completion
		c.TotalTokens += usage.Total
		c.CachedTokens += usage.Cached
		if model != "" {
			if c.ModelStats == nil {
				c.ModelStats = make(map[string]*ModelStat)
			}
			st := c.ModelStats[model]
			if st == nil {
				cost := "paid"
				if isFreeModelID(model) {
					cost = "free"
				}
				st = &ModelStat{ModelID: model, Cost: cost}
				c.ModelStats[model] = st
			}
			st.UsageCount++
			st.PromptTokens += usage.Prompt
			st.CompletionTokens += usage.Completion
			st.TotalTokens += usage.Total
			st.CachedTokens += usage.Cached
		}
		saveConnectionsLocked()
		return
	}
}

// connTaggedBody ties the connection that authenticated a (200) response to
// the response itself, by wrapping its body. The tag shares the response's
// lifetime, so abandoned responses can never leak map entries.
type connTaggedBody struct {
	io.ReadCloser
	conn *Connection
}

func tagResponseConn(resp *http.Response, conn *Connection) {
	if resp != nil && conn != nil {
		resp.Body = &connTaggedBody{ReadCloser: resp.Body, conn: conn}
	}
}

func responseConnection(resp *http.Response) *Connection {
	if resp == nil {
		return nil
	}
	if tb, ok := resp.Body.(*connTaggedBody); ok {
		return tb.conn
	}
	return nil
}

// recordUsage is the single attribution choke point for every finalize site.
// Cline-path responses carry an account; provider-path responses carry a
// tagged connection (acc == nil); zen traffic has neither and stays log-only,
// exactly as before wave-4.
func recordUsage(acc *Account, upstream *http.Response, reqLog *RequestLog, model string, usage tokenUsage) {
	if acc != nil {
		recordTokenUsage(acc, model, usage)
		return
	}
	conn := responseConnection(upstream)
	if conn == nil {
		return
	}
	if reqLog != nil {
		reqLog.ConnectionID = conn.ID
		reqLog.ConnectionEmail = conn.Email
	}
	if usage.Valid {
		recordConnectionUsage(conn.ID, model, usage)
	}
}
