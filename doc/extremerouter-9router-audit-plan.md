# Audit and implementation plan: ExtremeRouter / 9router → Cline2API (Go)

Status: draft audit plan, not yet reviewed.
Scope notes:
- 9router provenance in this codebase is historical: `src/lib/dataMigration.js` migrates legacy `~/.9router` / `%APPDATA%/9router` data into ExtremeRouter. READMEs also cite [9router](https://github.com/9router/9router) as the routing lineage.
- The working copy here is ExtremeRouter at `/home/dhan3713/extremerouter`.
- Cline2API is Go at `/home/dhan3713/cline2api` (module `cline-go-proxy`, Go 1.25, Echo via Wails transitive dep, SQLite via `modernc.org/sqlite` indirect dep).

This is a plan for the audit deliverable only. No edits, dependency installs, migrations, or config changes are proposed yet.

## 1. Existing Go architecture

Files already present in cline2api:
- `providers.go` — custom OpenAI-compatible provider registry, cold-down, sticky sessions, provider budget (RPM/daily), outcome scoring, fallback into combo/cline.
- `combos.go` — named model combos with ordered entries and source pinning (`cline`, `zen`, `provider`).
- `proxy.go` — dispatch/routing, combo execution, Anthropic streaming combos, upstream attribution.
- `responses.go` — OpenAI Responses proxy path and related handling.
- `pool.go` — model pool / account pool, enable/disable, defaults.
- `models_sync.go` — sync of remote/provider/zen models into the pool.
- `auth.go`, `capture.go`, `context_management.go`, `compact.go` — auth, capture, context/compact handling.
- `request_logs.go` — request log storage and attribution.
- `admin.go`, `admin_html.go`, `admin_model_test_api.go` — admin HTTP and inline HTML UI, including provider/combo UIs.
- `cline_proxy.go`, `zen.go`, `zen_proxy.go` — Cline and OpenCode Zen upstream handling.
- `types.go`, `main.go`, `go.mod` — types and bootstrap.

Confirmed Go capabilities from the repository:
- OpenAI-compatible custom providers with per-provider cooldown and budget checks before dispatch.
- Per-provider-model sticky sessions for paste sessions.
- Real-time outcome scoring for quality-ordered provider choice.
- Combos with ordered fallback and source pinning.
- Admin provider/combo management APIs and an inline HTML admin UI.
- Request log attribution to upstream (`cline`, `opencode`, `provider`).

Likely gaps to confirm during audit:
- No dedicated circuit breaker state machine file is visible yet in cline2api. Cooldown exists; circuit-breaker CLOSED/OPEN/HALF_OPEN semantics may be partial or expressed through cooldown + health logic.
- Free/free-tier/subscription/paid taxonomy is not obviously explicit as an access-class enum yet. Cost hints exist (`Cost` on models), but the audit must confirm there is no silent “unknown == free” behavior.
- Model discovery/import automation is present for Cline/Zen/provider models, but the audit must check how aggressive/visible auto-import is, and whether there is explicit source provenance plus refresh timestamps.

## 2. Model registry and automatic discovery

ExtremeRouter artifacts to inspect for the registry pattern:
- `open-sse/providers/schema.js`
- `open-sse/providers/REGISTRY_TEMPLATE.js`
- `open-sse/providers/registry/index.js`
- `open-sse/config/providerModels.js`
- `open-sse/providers/catalogSource.js`
- `.docs/model-catalog.md`
- `src/shared/constants/providers.js`
- `src/shared/constants/providersDisplay.js`
- `src/lib/modelCatalog/*`

9router lineage artifacts to inspect:
- `src/lib/dataMigration.js` (legacy ~/.9router migration)
- `open-sse/config/providerModels.js` mentions a port of a 9router commit for tiered model routing.
- README lineage references to 9router.

What I expect the audit to verify, not assume:
- Registry schema fields: id, alias, display, category/access class, auth modes, transport base URL, headers, model list source, validation URL, pricing/freeNote, capability metadata, priority, regions, passthrough, deprecation/notice.
- Catalog source pattern: static seed vs dynamic fetch, refresh timestamps, staleness handling, manual override precedence, atomic snapshot updates, availability verification.
- Access-class separation: explicit free, free-tier, subscription/OAuth, paid API key, trial, unknown. Confirm that “unknown” is never treated as free.
- Model aliasing and passthrough behavior: which providers allow arbitrary model IDs vs catalog-only models.

Proposed normalized Go registry shape (to be validated by audit, not adopted yet):
- Provider entity with explicit `AccessClass` enum and provenance fields (`Source`, `FirstSeen`, `LastVerified`, `VerifiedBy`).
- Model entity with provider scope, optional catalog source, optional override flag, capability tags, and access-class inheritance rules.
- Catalog snapshot stored atomically, with manual overrides layered on top, and explicit policy that unknown/unverified paid gating is denied for Free-only requests.

## 3. Reliability

ExtremeRouter artifacts to inspect:
- `open-sse/services/accountFallback.js`
- `open-sse/services/circuitBreaker.js`
- `open-sse/services/healthMonitor.js`
- `open-sse/services/cooldown.js` (if present in working tree)
- `open-sse/services/tokenRefresh.js` and related refresh providers
- `open-sse/services/usage.js`

What the audit should confirm:
- Account fallback classification rules: which errors are transient vs permanent, and when an account is marked unavailable and for how long.
- Circuit breaker states and transitions, including probe/recovery behavior.
- Health monitoring signals and aggregation windowing.
- Cooldown vs circuit breaker distinction.
- Token refresh triggers, expiry buffers, provider-specific refresh routing.

Proposed Go direction to evaluate:
- Per-account cooldown table (already partially present via provider cooldowns; audit whether account-level semantics are enough).
- Explicit circuit breaker states per provider/account with configurable thresholds and HALF_OPEN probes.
- Retry classification enum and Retry-After handling.
- Token refresh as a separate package with provider-specific adapters and expiry buffers.

## 4. Routing profiles

ExtremeRouter artifact:
- `open-sse/services/combo.js`

Smallest useful portions to evaluate:
- Ordered fallback over combo members.
- Round-robin rotation with sticky limit.
- Capability-aware reordering for hard modalities (vision/pdf/audio/video).
- Breaker pre-filter before attempting members.
- Fusion/smart-routing/cascade/swarm are larger; audit should separate the minimal fallback/round-robin/capability pieces from the orchestration-heavy parts.

Dependencies to identify before any port:
- Model capability catalog and resolution for “provider/model” strings.
- Account/provider selection and credential resolution.
- Request format normalization if combo members are heterogeneous.
- Telemetry/observability hooks if rotated state and routing decisions must be recorded.

## 5. Compatibility

ExtremeRouter artifacts to inspect:
- `open-sse/translator/index.js`
- `open-sse/translator/formats.js`
- provider executors in `open-sse/executors/*`
- streaming utilities in `open-sse/utils/*`

Audit must restrict to protocol pairs cline2api currently needs, then build a compatibility checklist:
- OpenAI chat completions inbound/outbound.
- Anthropic messages inbound/outbound, including streaming.
- OpenAI Responses if cline2api exposes it.
- Streaming SSE preservation.
- Tool calls / function calling round-trip.
- Reasoning/thinking payloads where supported.
- Structured output if supported.
- Usage metadata passthrough.
- Cancellation / client disconnect handling.
- Error shape preservation and mapping.

## 6. Later-stage features

Candidates to rank:
- Token compression / compaction.
- Cost/quota budgets and per-account/per-provider limits.
- Usage analytics and dashboards.
- Smart routing (intent/tool-calling aware).
- Cascade, fusion, swarm.

Each should be ranked by:
- User value for a free-first gateway.
- Implementation cost in Go.
- Latency impact on the hot path.
- Free-quota impact and risk of accidental paid usage.

## Deliverable outline

The final deliverable will be a feature matrix with:
- Feature and source project/file.
- Existing Go implementation and evidence.
- Missing or incomplete behavior.
- Recommended Go package/module.
- Dependencies and risks.
- Required regression tests.
- Priority: P0 / P1 / P2 / defer.

Then a phased implementation plan with acceptance criteria and compatibility risks. Nothing will be implemented before review and approval.

### Audit work-list before writing the matrix

- [ ] Confirm cline2api current access-class model and whether “unknown” can be misclassified as free.
- [ ] Confirm whether circuit breaker is already present or only cooldown-based.
- [ ] Confirm automatic model discovery/import scope and provenance tracking.
- [ ] Inspect ExtremeRouter registry schema/template/registry index and catalog source.
- [ ] Inspect accountFallback/circuitBreaker/healthMonitor/tokenRefresh/usage.
- [ ] Extract minimal combo semantics and dependencies.
- [ ] Extract only the translator pairs cline2api needs.
- [ ] Rank later-stage features and mark deferrals.

If you want, I can start the audit by reading the specific ExtremeRouter files listed above and mapping them against the cline2api Go files already identified.
