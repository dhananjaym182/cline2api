# Router Stability Review & Optimization Plan

Status: **P1–P5 fully implemented** (providers.go / proxy.go / admin.go / admin_html.go, uncommitted as of 2026-10-17).
This file documents the full plan so it survives outside the chat.

## 1. Baseline audit — what this codebase did

| Area | Behavior before fix | Location |
|---|---|---|
| Provider selection | Only ONE provider per model (`resolveProviderForModel` picked best-priority; failure → jump to next *model*, never next *provider*) | providers.go:246, 516 |
| Provider cooldown | Any non-200 → blanket 5-min cooldown on `provider:model` (400/401/421/5xx treated alike), in-memory only | providers.go:356-365 |
| Account cooldowns | Model-level 429 cooldown parsed from "Try again in Xh Ym" (fallback 1h); 402 → 30m; pre-first-token stream failure → 2m; 5m account cooldown on transport error; 30s background probe auto-recovers cooldown accounts | proxy.go:1557-1585, pool.go |
| Fallback chain | Requested model + admin `modelChain` (or default free chain), delisted models filtered, per-request re-sorted by availability then least-used | proxy.go:1411, pool.go:554 |
| Combo / UI config | `strategy` (round_robin/random/fill) + `modelChain` text field + separate zen/account proxy strategies | admin.go:893, admin_html.go:730 |
| Zen upstream | Outage detector (N consecutive failures → fail over to cline pool for a window), per-proxy cooldown with Retry-After cap, keyword-based rate-limit detection | zen.go:424, 862 |

**Root cause of "random model when a provider fails":** no failover *group* — a model backed
by N providers got a single attempt; on failure the request silently degraded to a different
model (different quality), which looked random from the client.

**421 handling before fix:** unclassified everywhere. Provider path → blind 5-min cooldown;
zen path → generic error. 421 (gateway misdirect; edge node can't serve the model) is usually
an instant-retry condition, so healthy providers were frozen for nothing.

## 2. Reference architectures compared

Both reference designs are described generically here on purpose: no third-party project
names are referenced in this repository (docs included).

**Tiered-combo style router**: named chains (Subscription → Cheap → Free), multi-account
per provider with round-robin/priority, quota tracking with reset countdowns
(5h/daily/weekly), auto token refresh, automatic recovery back to the highest-priority
provider when its window resets.

**Free-pool style router**: unified model identity with *in-group failover*
(same model on N providers → try all N before moving to the next model), routing
strategies ranked by live speed/reliability scores, per-key RPM/RPD/TPM/TPD counters that
learn provider ceilings so routing stays under the cap *before* a 429, sticky sessions
(~30 min per conversation) with compact context handoff, periodic key health checks,
served-by observability header.

## 3. Phased plan

### P1 — Status-code classification + 421 handling ✅ implemented
`providerCooldownUntil` in providers.go:
- 400/401/403 → 30-min cooldown (config-dead; retry can't self-heal)
- 429 → Retry-After honored (cap 30m, default 2m)
- 421 → **no cooldown** ( immediate failover); quota/billing keywords in body → short 30s
- 5xx → 30s cooldown
- anything else → no cooldown

### P2 — In-group provider failover ✅ implemented
`resolveProvidersForModel()` returns all eligible providers priority-sorted;
`callCustomProviderAPI()` walks the group — failing primary hands over to the next provider
for the *same model*. A model is only degraded to another model after the entire group fails.
Priority is preserved across cooldown expiry (automatic recovery to the preferred provider).

### P3 — Sticky sessions ✅ implemented
Same conversation (hash of first user message) stays pinned to one healthy provider for
30 min (`stickySessions` + `stickySet` refresh on each success). Pinned provider that
goes cold/disabled/budget-exhausted is un-pinned instantly and re-enters group failover.
New sessions are unaffected — they start at the priority head of the group.

### P4 — Score-based routing ✅ implemented
Rolling success ratio + average latency per (provider, model), 1-hour window over a
24-sample ring (`recordProviderOutcome` / `providerScoreStats`). New `quality` strategy
(admin Settings → 轮询策略) orders the provider group by measured health first,
config priority second. No samples → neutral 0.5, so priority order is preserved.

### P5 — Budget-aware pre-routing ✅ implemented
Per custom provider: `rpmLimit` (sliding 60s window) and `dailyRequestLimit` (UTC day
counter), editable in the Providers editor with a quota hint. Over-cap providers are
excluded from the candidate set *before dispatch* — no more discovering 429s by trial.
Dispatch-counting happens per upstream attempt (`recordProviderDispatch`).

## 4. Already stronger than the references — keep as-is
- Delisted-model detection (`markModelGone`) and stale-chain filtering
- 30s account self-healing probe loop
- Zen outage circuit breaker
- Per-request least-used spreading

## 5. Verification record (2026-10-17, post P3–P5)
- `go build ./...`, `go vet .` — clean
- Full suite `go test . -count=1` — ok, exit 0; provider tests also pass under `-race`
- New tests: `TestProviderGroupFailover`, `TestProviderCooldownClassification`,
  `TestProviderStickySession` (pin → un-pin on cooldown → no mid-session switch back →
  fresh sessions unaffected), `TestQualityStrategySorting`, `TestProviderBudgetExcluded`,
  `TestProviderCooldownPersistence` (fresh persisted, expired dropped, reload restores)
- Application binary built as `cline-proxy` (15 MB, runs, usage output OK); gitignored (old `cline-go-proxy` name retired)
- No external project names referenced anywhere in the codebase (checked by grep)
- **E2E test of the root intent** (2026-10-17): isolated instance (`CLINE2API_DATA_DIR`) +
  two mock upstreams — `test/same-model` on provA (priority 10, returns HTTP 500) and
  provB (priority 20, valid completion). A chat completion request came back with
  `"model":"test/same-model"` served by provB; log shows
  `provider attempt failed name=provA ... API 500` then group handover. The pre-fix
  behavior (silent degradation to a *different* model on first provider failure) is gone.

## 6. Open items
- Changes uncommitted pending review
- Sticky-session TTL is fixed at 30 min (no per-request opt-out header yet)
- Quality scores and budget counters are in-memory only (reset on restart by design; only
  cooldowns are persisted)
