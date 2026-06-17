# HttpOnly-cookie session (persist login across refresh, XSS-safe)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`).

**Goal:** Keep users logged in across page reloads WITHOUT storing the token in JS-readable storage. The session token is set as an `HttpOnly; SameSite=Strict; Path=/` cookie by the AS on login; the backend accepts the cookie for HTTP + WebSocket auth; the frontend uses same-origin cookies (`credentials:"include"`) and restores the session on startup via `GET /auth/session`. No localStorage (XSS-stealable) anywhere.

**Architecture:** Backend sets/clears the `session` cookie on login-finish/logout (Secure gated by `COOKIE_SECURE` env; dev=false over the localhost proxy). `authMW` and the WS gateway read the token from the cookie as a fallback to the `Authorization: Bearer` header (header kept for tests/API clients). Frontend API clients add `credentials:"include"`; on boot, the app probes `GET /auth/session` — if the cookie authenticates, it restores the logged-in state and skips the auth screen. The in-memory token (from the login response) is still used during the active session; after a refresh it's gone but the HttpOnly cookie carries auth.

**Security note:** in-memory token during a live session is the accepted baseline (unavoidable for an SPA). The win: nothing persistent is readable by JS — a refresh relies solely on the HttpOnly cookie, which XSS cannot read. `SameSite=Strict` blocks the main CSRF vector; a CSRF token for mutations can follow later.

---

### Task 1: AS sets/clears the session cookie + config

**Files:** `backend/internal/config/config.go` (add `CookieSecure bool`), `backend/internal/httpapi/auth_handlers.go` (set cookie on loginFinish, clear on logout/logoutAll), `backend/internal/httpapi/router.go` (thread the flag), `cmd/server/main.go` (pass `cfg.CookieSecure`). Tests in `auth_handlers_test.go`.

- [ ] **Config:** add `CookieSecure bool` to `Config`; in `Load()`, `CookieSecure: getenvBool("COOKIE_SECURE", true)` (add a small `getenvBool` helper: returns false only for "false"/"0", default param otherwise). Update `backend/.env` (gitignored) note: dev sets `COOKIE_SECURE=false`.
- [ ] **Cookie helper** in httpapi (e.g. `middleware.go`): 
```go
const sessionCookie = "session"
func setSessionCookie(w http.ResponseWriter, token string, secure bool) {
    http.SetCookie(w, &http.Cookie{
        Name: sessionCookie, Value: token, Path: "/", HttpOnly: true,
        Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: int((24 * time.Hour).Seconds()),
    })
}
func clearSessionCookie(w http.ResponseWriter, secure bool) {
    http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}
```
- [ ] `authHandlers` gains a `cookieSecure bool` field. In `loginFinish`, after a successful `LoginFinish` (before writing the JSON), call `setSessionCookie(w, token, h.cookieSecure)`. In `logout` and `logoutAll`, call `clearSessionCookie(w, h.cookieSecure)`. Thread `cookieSecure` through `NewRouter`/`NewRouterFull` (new param) → `cmd/server` passes `cfg.CookieSecure`. Keep the JSON body returning `session_token` (in-memory use during the session).
- [ ] **Tests:** in `auth_handlers_test.go`, a login round trip asserts the response carries a `Set-Cookie: session=...; HttpOnly` (parse `rec.Result().Cookies()` — find `session`, assert `HttpOnly`, value == token). Logout asserts a `session` cookie with `MaxAge<0`/empty value. RED→GREEN.
- [ ] `cd backend && go build ./... && go vet ./... && go test ./internal/httpapi/ ./internal/config/`. Commit `feat(backend): set HttpOnly session cookie on login, clear on logout`.

### Task 2: authMW + WS gateway accept the cookie

**Files:** `backend/internal/httpapi/middleware.go` (authMW), `backend/internal/ws/gateway.go` (`tokenFromRequest`/`resolve... `). Tests.

- [ ] **authMW:** extract the token as: `Authorization: Bearer <t>` if present and non-empty; ELSE the `session` cookie value. (Factor a helper `tokenFromHTTP(r) string`.) Then `sess.Validate` as today; empty → 401. Add a unit test: a request with only the `session` cookie (no header) authenticates; neither → 401; header takes precedence.
- [ ] **WS gateway:** extend `tokenFromRequest` to also fall back to the `session` cookie (after header and `?access_token=`). Add to `gateway_token_test.go` (or wherever `tokenFromRequest` is tested): cookie-only request → returns the cookie token; precedence header > query > cookie.
- [ ] `go build ./... && go test ./internal/httpapi/ ./internal/ws/`. Commit `feat(backend): accept session cookie for HTTP + WebSocket auth`.

### Task 3: Frontend uses cookies + restores session on boot

**Files:** `client/src/shared/api/{as,workspace,conversations,kt}.ts` (add `credentials:"include"`), `client/src/shared/api/as.ts` (add `session()` method), `client/src/entities/session/store.ts` (add `restore()`), `client/src/app/bootstrap.ts` (already returns `orchestrator`), `client/src/main.tsx` (restore-on-boot). Tests for the clients + store.

- [ ] **API clients:** add `credentials: "include"` to every `fetch`/`call` request init in `as.ts`, `workspace.ts`, `conversations.ts`, `kt.ts` (so the same-origin cookie is always sent). Keep the `Authorization: Bearer` header (harmless; empty after refresh — the cookie auths). Update the affected fetch-mock tests only if they assert `init` shape (add `credentials` to expectations as needed).
- [ ] **AsClient.session():** `async session(token: string): Promise<{ user_id: string; device_id: string }>` → `GET /auth/session` (Bearer + credentials:include). Test against a fetch mock (200 → parsed; 401 → throws).
- [ ] **session store `restore()`:** add a method that sets `status` to `"onboarded"` without a token (token stays `""`; the cookie carries auth). Keep the in-memory `authenticated`/`onboarded` as-is. Test: `restore()` → `status()==="onboarded"`. (Do NOT persist anything to localStorage.)
- [ ] **main.tsx restore-on-boot:** destructure `orchestrator` from bootstrap. Before/independent of login, run a probe:
```ts
void (async () => {
  try {
    await as.session("");            // cookie-authed; 401 if no/expired cookie
    session.restore();               // status → onboarded (skip auth screen)
    await workspaces.load();
    await conversations.load();
    orchestrator.connect("");        // WS via cookie
  } catch { /* no session: stay on the auth screen */ }
})();
```
(Expose `as` from bootstrap's return, or add a `probeSession()` to bootstrap that does the `GET /auth/session`. Cleanest: have bootstrap return a `restoreSession(): Promise<boolean>` that does the probe+loads+connect and returns whether it restored; main calls it on boot. Prefer that — keeps the AsClient encapsulated.) Also: `userEmail` for the rail isn't known after refresh (not in the cookie); `GET /auth/session` returns `user_id`/`device_id`, not email — show initials from `user_id` or leave the avatar generic; acceptable (note it).
- [ ] `npx vitest run && npx tsc --noEmit && npx vite build`. Commit `feat(client): cookie-based auth + restore session on reload`.

### Task 4: Wire + live verify (refresh stays logged in)

**Files:** `client/vite.config.ts` (verify the `/auth`,`/workspaces`,… proxy forwards Set-Cookie/Cookie — http-proxy does by default; add `cookieDomainRewrite: "localhost"` only if needed), `backend/.env` (`COOKIE_SECURE=false` for dev), restart stack.

- [ ] Set `COOKIE_SECURE=false` in `backend/.env`; `docker compose up -d --build` the backend; ensure the vite dev proxy forwards cookies (test: after login, `document.cookie` is EMPTY — because HttpOnly — but the network shows the `session` cookie sent on subsequent requests). 
- [ ] **Live check:** log in → reload the page → you stay logged in (auth screen NOT shown; workspaces/channels load; status online). Confirm `document.cookie` does NOT contain the token (HttpOnly). Logout (if wired) clears it. Report the result.
- [ ] Full verify `cd client && npx vitest run && npx tsc --noEmit && npx vite build`; `cd backend && go build ./... && go test ./...`. Commit `chore: dev cookie config + verify cookie session end-to-end`.

---

## Self-Review
**Coverage:** cookie set/clear + config (T1); cookie auth for HTTP + WS (T2); frontend cookie usage + boot restore (T3); dev wiring + live verify (T4). ✓ XSS-safe persistence (HttpOnly, no localStorage). MLS group state still not persisted (reload → can't decrypt old messages / re-create groups to send) — documented follow-up (OpenMLS→IndexedDB), separate from auth persistence.
**Placeholders:** concrete cookie attributes + handler/middleware changes; `getenvBool` + `restoreSession()` specified. Dev `Secure=false` (localhost over http via proxy); prod `Secure=true`.
**Type consistency:** `authMW`/`tokenFromRequest` cookie fallback; `NewRouterFull` gains `cookieSecure bool` (wired in cmd/server); `AsClient.session()` ↔ main restore; `session.restore()` ↔ gate (`status()!=="anonymous"`). credentials:"include" on all clients.
**Risk:** the vite proxy must forward Set-Cookie/Cookie (default yes); SameSite=Strict over same-origin proxy is fine; if the cookie isn't stored, check Secure (dev=false) + that the browser sees the cookie scoped to localhost:5173. Verified live in T4.
