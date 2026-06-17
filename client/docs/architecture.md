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

## Known integration gaps (follow-ups)
- **Auth token over WebSocket:** browsers can't set the `Authorization` header on the WS handshake, so the protocol worker passes the session token as `?access_token=`. The DS gateway currently reads a `Bearer` header — a backend change (accept a query-param/subprotocol token or a short-lived ticket) is required before live frontend↔DS.
- **DS_WS_URL not threaded:** `shared/config/env.ts` exports `DS_WS_URL`, but the protocol worker uses its own fallback; the env value isn't yet passed into the worker's connect. Wire it when the connect flow lands.
- **connect() not invoked:** `main.tsx` builds the orchestrator but never calls `connect(token)` — there's no session token yet (OPAQUE-client is a separate plan). The transport is wired but does not dial DS.
- **Unhandled DS frames:** the protocol connection currently demuxes only `message` frames; `sent`/`error`/sync-ack frames are defined in `shared/api/ds.ts` but not yet consumed.
- **features/ layer:** the plan listed a `features/` layer; for this scaffold those use-cases are folded into `app/orchestrator.ts` (the documented home for business logic). Add `features/` when the use-cases grow.
