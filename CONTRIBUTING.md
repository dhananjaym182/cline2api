# Contributing

This repository is a `package main` composition layer plus `internal/`
packages. Keep the dependency direction:

```
platform -> domain -> config -> transport / httpx / i18n -> store/*
  -> providerreg / upstream/* / protocol/* / deviceflow
  -> modelcatalog -> dispatch -> httpapi / adminapi -> root main
```

Rule of thumb: stores persist state and never call upstream APIs; device flows
return results and the caller persists them; converters never write HTTP
responses; dispatch owns retries and the SSE writer.

## Embedded presets vs runtime providers

There are two different provider paths:

- **Embedded presets** live in `internal/providerreg/presets/*.json` and are
  compiled into the binary with `//go:embed`. Changing a preset requires a
  rebuild.
- **Runtime custom providers** are stored in `.cline-providers.json` under the
  data directory and take effect without a rebuild (see the admin UI).

A provider-level `kind`/`freeTier` flag is metadata used by the admin UI. It is
not a per-model access entitlement.

### Adding an embedded preset

1. Add `internal/providerreg/presets/<provider-id>.json`; the filename must
   equal the preset `id`.
2. Fill the schema in `internal/providerreg/registry.go` (`id`, `name`,
   `baseURL`, `kind`, `freeTier`, protocol/auth/fetcher fields, notes).
3. Regenerate or adjust the preset data. The tracked generator is
   `tools/gen-provider-presets.mjs`; hand-written presets are kept as data.
4. Run `go test ./internal/providerreg/` (IDs must be unique, filenames must
   match IDs, required fields and enum values are validated).
5. Rebuild. Embedded preset edits do not apply to an existing binary.

A conventional OpenAI-compatible provider usually needs only a preset because
the shared `internal/upstream/openaicompat` client handles its protocol. A
provider with a private authentication flow, nonstandard request shape, or
special stream format needs a Go adapter as well — do not claim that every
provider can be added with data alone.

### Adding a runtime provider

Use the admin UI or `POST /admin/api/providers/save`. Runtime providers are
validated at save time and are not embedded into the binary.

## Adding a device-auth flow

1. Add a flow spec in `internal/deviceflow/spec.go` (provider, mode, endpoints,
   JSON paths, polling interval, expiry).
2. Reuse an existing strategy (`rfc8628`, `statepoll`, `qoder`, `kilo`) where
   possible; add a new strategy only when the exchange semantics differ.
3. Return a `deviceflow.Result`. `deviceflow` must not import the connections
   store. The root caller persists the connection and refreshes tokens.
4. Cover initiate, pending, approved, denied, expired, and refresh paths with
   `httptest` upstreams in `internal/deviceflow/deviceflow_test.go`.

## Adding a model capability or entitlement

Keep these concepts separate:

- `domain.Model` catalog fields (`ID`, `Provider`, `Cost`, `Context`, `Output`,
  `TierSignal`, …).
- Access entitlement: `domain.AccessClass` — `free`, `free_tier`,
  `subscription`, `paid_api`, `trial`, `unknown`.
- Provider metadata: preset `kind`/`freeTier`, provider `free` flags.

Classification lives in `modelcatalog` (`Classify`, `TrackMissing`,
`FilterNew`). Entitlement policy lives in `dispatch`:

- `free` is eligible for Free-only routing.
- `free_tier` is eligible only with a verified active allowance.
- `subscription`, `paid_api`, `trial`, and `unknown` never silently qualify.
- A provider-level free flag maps to `unknown` until the route is verified, so
  it must not be used to mark a model `free`.

When adding a capability, update domain types, classifier signals, and tests.
When changing Free-only behavior, add a dispatch policy test and a routing test;
never enforce the policy only in UI labels.

## Adding a protocol adapter

1. Implement `protocol.Protocol` (`Name`, `Capabilities`). Include only features
   the wire shape can actually carry.
2. Implement optional interfaces as needed: `RequestDecoder`, `RequestEncoder`,
   `BodyBuilder`, `StreamTransformer`, `Replayable`.
3. Return `*protocol.Error` with the intended status; converters must not write
   HTTP responses.
4. Preserve tool calls, IDs, reasoning fields, and usage wherever the target
   protocol can represent them.
5. Honor `context.Context` and never leak goroutines. Do not retry across
   providers after stream bytes have been committed unless the adapter reports
   replay support.
6. Add regression tests for streaming ordering, tool calls, reasoning, usage,
   cancellation, and malformed upstream frames.

Dispatch owns the SSE writer and retry policy; the adapter only transforms
bytes.

## Testing

Run the full gates from the repository root:

```bash
gofmt -l .
go build ./...
go vet ./...
env -u CLINE2API_DATA_DIR go test ./...
go list ./...
go build -o /tmp/cline-proxy .
```

- Tests must use temporary data directories (`CLINE2API_DATA_DIR` or the
  existing test isolation helpers) and must never touch the real data directory.
- Registry changes need validation/parity tests.
- Protocol changes need streaming and tool-call regression coverage.
- Store/config changes must prove temporary data never lands in production
  paths.
- If a change touches `adminui/`, concatenate the JS and run `node --check`.
- Desktop builds (`-tags "desktop production"`) need GTK/WebKit system
  libraries; they may be verified in CI when the local environment lacks them.

Do not run smoke tests against the live `:3477` server. Use a scratch port and a
fresh `CLINE2API_DATA_DIR`, record the PID you start, and terminate only that
process.
