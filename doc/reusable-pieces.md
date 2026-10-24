# ExtremeRouter — reusable pieces you can copy into another project

A quick reference for what’s portable from this ExtremeRouter checkout, what’s MIT-licensed, and which files are the cleanest starting points. Use this when you want to borrow behavior (token savers, combo routing, format translation, health/circuit patterns, swarm orchestration, smart routing, etc.) rather than run the whole gateway.

This doc now also covers **which providers are realistically copyable** and **which UI style pieces are reusable**, with a free-first lens.

## License

ExtremeRouter is MIT-licensed. See `extremerouter/LICENSE`. That usually means you can copy, adapt, and reuse code in other projects, but you should still keep the license file and copyright notice if you redistribute a substantial portion. Confirm the current license before shipping anything that depends on these files.

## What this checkout is

ExtremeRouter is a JavaScript/Node AI gateway. The core engine lives under:

- `open-sse/` — provider-agnostic SSE engine (`index.js` is the public barrellist)
- `open-sse/services/` — combo, swarm, circuit breaker, health, token refresh, smart routing, budgets, etc.
- `open-sse/translator/` — request/response format translation + shared concerns
- `open-sse/rtk/` — tool-result compression and prompt injectors
- `open-sse/config/` — runtime config, provider registry, model lists, limits, pricing
- `open-sse/providers/` — provider capabilities / pricing / registry helpers
- `open-sse/executors/` — per-provider upstream executors
- `open-sse/handlers/` — modality cores: chat, image, embedding, tts, stt, search
- `open-sse/utils/` — stream helpers, SSE, error handling, proxy fetch, cursor/protobuf, etc.
- `open-sse/shared/` — cross-provider auth/identity helpers
- `src/` — dashboard app, lib helpers, model catalog sync
- `cli/` — the CLI/npm entrypoint and its hooks

## How to copy pieces safely

The cleanest approach is to copy a vertical slice and make it standalone:

1. Pick the smallest file that already isolates the behavior you want.
2. Copy its direct dependencies first.
3. Remove dashboard/db/Next/provider-specific code you do not need.
4. Re-test the slice in a small script before wiring it into a larger system.

If a file depends on `@/lib/...` or `../config/...` paths, you will need to either keep that path layout or rewrite the imports. Many modules are designed to run inside the ExtremeRouter dev/runtime environment and may rely on globals, env vars, or sibling modules.

## Strong reuse candidates (good first copies)

- Token saver pipeline — `open-sse/rtk/index.js` plus `open-sse/rtk/*.js` and `open-sse/rtk/filters/*`
  - Good for compressing tool results before they reach a model.
- Format translation layer — `open-sse/translator/*.js`, especially request/response translators and `open-sse/translator/concerns/*`
  - Good if you need OpenAI ↔ Claude ↔ Gemini-style request/response normalization.
- Combo fallback + round-robin routing — `open-sse/services/combo.js`
  - Good if you want a model list with failover, sticky rotation, capability reordering, and retry-after handling.
- Circuit breaker — `open-sse/services/circuitBreaker.js`
  - Good for per-provider or per-backend health gating with CLOSED/OPEN/HALF_OPEN behavior.
- Health monitor — `open-sse/services/healthMonitor.js`
  - Good for sliding-window success/latency tracking and degradation detection.
- Smart routing combos — `open-sse/services/smartRouting.js`, `open-sse/services/smartRoutingTelemetry.js`
  - Good if you want task-aware ordering of model pools.
- Fusion + swarm orchestration — `open-sse/services/combo.js` fusion parts and `open-sse/services/swarm.js`
  - Good if you want parallel panel calls, quorum-grace collection, or hierarchical multi-stage orchestration.
- Combo budget / cost estimation — `open-sse/services/comboBudget.js`, `open-sse/services/comboSimulator.js`, `open-sse/services/comboConfig.js`
  - Good if you want pre-call cost estimation and output/call ceilings.
- Account fallback + cooldown logic — `open-sse/services/accountFallback.js`
  - Good for backoff, quota-exhaustion detection, cooldown timestamps, and availability filtering.
- Token refresh helpers — `open-sse/services/tokenRefresh.js` and `open-sse/services/tokenRefresh/providers.js`
  - Good if you want OAuth refresh dispatch and per-provider refresh routing.

## Providers — what is realistically copyable

The provider layer is one of the most reusable parts of this repo, but “copyable” depends on which layer you mean. The cleanest pieces are the **registry schema, template, shared helpers, and the free/cheap provider entries themselves**. The big generated registry index is usable as a catalog, but copying all of it blindly is usually more than you want.

### Copyable provider plumbing

- `open-sse/providers/schema.js`
  - Portable contract for a provider entry: `PROVIDER_DEFAULTS`, `ENDPOINT_DEFAULTS`, `resolveProvider()`.
  - Good if you want to build your own registry in another project using the same shape.
- `open-sse/providers/REGISTRY_TEMPLATE.js`
  - The best “how to add a provider” starting point. Delete the blocks you do not need; only `id` + `category` are required.
- `open-sse/providers/shared.js`
  - Portable shared helpers: Anthropic API version, Claude-compatible headers, a few known OAuth client constants, and env-override helpers.
  - Useful if you are building Anthropic-compatible or OpenAI-compatible providers elsewhere. Be careful with the OAuth client constants if you want your own client identity.

### Copyable provider entries

The most directly copyable provider definitions are the small, self-contained registry entries that do not reach into a huge amount of provider-specific runtime glue. From the ones inspected here:

- `open-sse/providers/registry/openai.js`
  - Good reference for an API-key provider with chat + media endpoints and a rich model list.
- `open-sse/providers/registry/anthropic.js`
  - Good reference for a Claude-format provider with transport headers and a small canonical model list.
- `open-sse/providers/registry/ollama.js`
  - Most interesting for a free-first setup: it is a `freeTier` catalog entry with a real free-tier notice and usage flags.

### What the registry makes easy to reuse

- `open-sse/providers/registry/index.js`
  - This is basically a generated catalog of a very large provider set.
  - Portable as a **reference catalog or discovery list**, less ideal as a drop-in unless you also want all the dependency assumptions behind every entry.
- `src/shared/constants/providers.js`
  - Shows a clean way to derive UI-ready provider objects from the registry by category: `FREE_PROVIDERS`, `FREE_TIER_PROVIDERS`, `OAUTH_PROVIDERS`, `APIKEY_PROVIDERS`, `WEB_COOKIE_PROVIDERS`, plus helpers like `getProvidersByKind()` and alias resolution.
  - Good pattern to copy if you want category-filtered provider lists in your own UI.
- `src/shared/constants/providersDisplay.js`
  - Thin layer that turns `registry.display` into `PROVIDER_DISPLAY`. Good example of keeping provider UI metadata close to the registry.

### Providers to prioritize if you want free first

If the goal is “start with free models, then add paid later”, this checkout already has a strong free/cheap backbone:

- `open-sse/providers/registry/ollama.js` is the clearest free-tier example here:
  - `category: "freeTier"`
  - `hasFree: true`
  - Free-tier notice text plus API key URL in `display.notice`
  - Usage/usageApikey feature flags
- The dashboard already separates free and free-tier providers at the UI layer via `FREE_PROVIDERS` and `FREE_TIER_PROVIDERS` in `src/shared/constants/providers.js`.
- The provider register/tooling pieces around `open-sse/providers/registry/*` and `src/app/(dashboard)/dashboard/providers/components/*` are built to let you add more free/cheap OpenAI-compatible and Anthropic-compatible nodes without rewriting the provider system.

The strongest free-first copy path is therefore:

1. Copy the provider schema, template, shared helpers, and one or two clean registry entries.
2. Port the “derive providers by category” helpers from `src/shared/constants/providers.js`.
3. Add your own `freeTier` / `free` entries first, then layer paid providers on top later.

### Providers to treat as context first

Many registry entries are best treated as reference rather than as standalone copies:

- Large or OAuth-heavy entries can carry client IDs, token URLs, device-code flows, and other per-provider assumptions.
- OAuth client constants in `open-sse/providers/shared.js` are real packaged defaults; if you copy them, decide whether you want your own OAuth app identity instead.
- Web-cookie and subscription-derived providers can depend on session/cookie behavior that does not translate cleanly to a different project.

## UI style — what is realistically copyable

The dashboard’s reusable style is concentrated in a small set of shared components, constants, and provider/icon helpers. These are the easiest pieces to port if you want the same look-and-feel without copying entire pages.

### Most portable UI style pieces

- `src/shared/components/Card.js`
  - Good base component: padding variants, title/subtitle/icon/action header, section/row/list-item sub-components, and the panel/border/shadow styling pattern.
- `src/shared/components/PageHeader.js`
  - Clean page-header pattern: title, description, icon, actions, breadcrumbs.
- `src/shared/components/ProviderIcon.js`
  - Portable icon component with img loading + fallback text/color behavior.
- `src/shared/components/Sidebar.js`
  - Portable sidebar pattern: nav groups, active-state logic, search box, and “local gateway” status block.
  - Note: it does import `useNewBadge` and `DonateModal`, so treat it as a starting point rather than a pure standalone copy.
- `src/shared/components/index.js`
  - Useful as an export-map reference for which shared components exist and how they are exposed.

### Portable provider/UI constants and helpers

- `src/shared/constants/providers.js`
  - Very reusable if you want the same provider categorization, alias resolution, and “get providers by kind” helpers.
- `src/shared/constants/providersDisplay.js`
  - Reusable pattern for deriving UI display metadata from the registry.
- `src/app/(dashboard)/dashboard/providers/components/ProviderTile.js`
  - Good reference for a rich provider card: icon box, status badges, compatible badges, action bar with test/settings/toggle.
- `src/app/(dashboard)/dashboard/providers/components/ProviderKpis.js`
  - Portable KPI tile pattern with color-tinted icon chips and micro-labels.
- `src/app/(dashboard)/dashboard/providers/components/ProviderToolbar.js`
  - Portable filter-chip + sort bar pattern.
- `src/app/(dashboard)/dashboard/providers/components/ModelsCard.js`
  - Portable “show models for a provider” card with inline test/copy/alias/delete patterns.
- `src/app/(dashboard)/dashboard/providers/components/ConnectionsCard.js`
  - Portable connection-list patterns: priority arrows, proxy dropdown, cooldown timer, test-status badges.
- `src/app/(dashboard)/dashboard/providers/components/ProviderTestResults.js`
  - Portable test-results summary + item list.
- `src/app/(dashboard)/dashboard/providers/components/AddCompatibleModal.js`
  - Portable “add OpenAI/Anthropic-compatible node” modal pattern.
- `src/app/(dashboard)/dashboard/providers/components/helpers.js`
  - Portable provider-page helpers: status display, connection error tags, stats maker, search matching, auth-type resolution, and priority sorting.
- `src/app/(dashboard)/dashboard/combos/components/ModelItem.js`
  - Portable drag-and-drop sortable model row with inline editing, provider icon, and capacity badges.
- `src/app/(dashboard)/dashboard/combos/components/ComboCard.js`
  - Rich combo UI reference: collapsed/expanded card, strategy picker, fusion/swarm role pickers, thinking config, and budget control. Large but very reusable as a design reference.
- `src/app/(dashboard)/dashboard/endpoint/endpointConstants.js`
  - Reusable constants for caveman/ponytail levels and tunnel benefit copy.
- `src/app/(dashboard)/dashboard/token-saver/TokenSaverClient.js`
  - Good UI reference for a settings/feature toggle page with RTK, headroom, caveman, ponytail, semantic cache, and pxpipe sections.

### UI style foundations likely needed to reproduce the look

The dashboard UI depends on a few styling foundations that are not all in the files inspected here. If you want the exact look, plan to port or recreate:

- Tailwind/utility class tokens such as `rounded-panel`, `bg-panel`, `border-border-subtle`, `text-text-main`, `text-text-muted`, `bg-primary`, and shadow variables.
- The `cn()` utility used throughout (`@/shared/utils/cn`).
- Material Symbols icon usage and the provider-icon resolution helper (`getProviderIconPath` from `@/shared/utils/providerIcon`).
- Global CSS variables and theme tokens (likely in the dashboard’s global styles).

So the UI pieces above are very copyable as React components and patterns, but reproducing the full visual style may also require the design-token layer.

### Context-only UI pieces

Some dashboard pieces are large and deeply tied to the dashboard’s data flow:

- Full provider detail pages, provider form pages, and provider onboarding flows are better treated as examples than as drop-in copies.
- Pages that depend heavily on dashboard API routes, DB state, or auth are usually not worth copying unless you are porting the whole console.

## Orthogonal reference docs worth copying conceptually

- Model capability catalog — `extremerouter/.docs/model-catalog.md`
  - Useful as design reference for precedence rules, confidence tagging, staleness semantics, worker-thread refresh, and atomic snapshot writes.
- Agent guidance — `extremerouter/AGENTS.md` and `open-sse/AGENTS.md` (`open-sse/AGENTS.md` is especially useful for the `open-sse` module layout and conventions)

## Files to treat as context, not as drop-in copies

These are valuable for learning the architecture, but they tend to reach deep into the rest of the project:

- `open-sse/index.js` — re-exports a lot of the engine; useful as an API map, but copying it blindly can pull in more than you need.
- `open-sse/config/providers.js`, `open-sse/config/providerModels.js`, `open-sse/providers/registry/*.js` — provider registry is valuable as a reference catalog, but copying the whole registry may import provider-specific assumptions you do not want.
- `open-sse/executors/` — many executors are provider-specific glue.
- `src/` dashboard files — usually only relevant if you are also porting the web UI.

## Practical reuse ideas

- If you only want safer retries for an upstream LLM client, start from the circuit breaker plus the fallback check in `accountFallback.js`.
- If you only want smaller tool results, start from the RTK `compressMessages()` path and its filters.
- If you only want a handful of format translators, copy one translator pair plus the shared concerns it uses, then wire it through your own request/response layer.
- If you want multi-model routing without the full gateway, start from `combo.js` fallback/round-robin logic and stub out the handler closure it expects.
- If you want a free-first provider model in your own project, copy the registry schema + template + shared helpers, then port the “derive providers by category” UI helpers and start from `ollama.js` as the free-tier shape.
- If you want the same dashboard style elsewhere, start from `Card.js`, `PageHeader.js`, `ProviderIcon.js`, `Sidebar.js`, and the provider/card/toolbar/kpi helpers, then recreate the design tokens they expect.

## Before you copy anything

- Read the files you plan to copy end-to-end, not just the exported function names.
- Check imports for hidden coupling to dashboard, DB, env, or global singleton state.
- Decide whether you want the behavior as-is or only the pattern; sometimes the reusable idea is smaller than the file.
- Keep the MIT license notice if you copy a substantial amount of code or redistribute it.

If you want, I can turn this into a narrower “copy this file and its direct deps” checklist for one specific feature, like combo routing, format translation, the RTK token saver, or a free-first provider registry.
