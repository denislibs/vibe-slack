# Frontend architecture

Three threads, FSD layers, window-hub bus. Full design:
docs/superpowers/specs/2026-06-17-frontend-scaffold-design.md.

## Import rules (FSD — imports point DOWN the layers only)
app → pages → widgets → features → entities → shared

- `shared/ui` (ui-kit): pure presentational components. MUST NOT import features/entities/app.
- `widgets`, `pages`: presentation only — data + callbacks via props. NO worker/store access, NO business logic.
- `features`, `entities`, `app`: business logic lives here. The orchestrator (app) is the only place wiring the protocol + crypto clients to stores.
- `shared/lib/{crypto,transport,rpc}`: infrastructure (workers, bus). UI never imports these directly.

## Threads
- UI (window): SolidJS + orchestrator (bus hub).
- Protocol worker: WebSocket ↔ DS (frame contract in shared/api/ds.ts).
- Crypto worker: WASM MLS engine (shared/lib/crypto).
