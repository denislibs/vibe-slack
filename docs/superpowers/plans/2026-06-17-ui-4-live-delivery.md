# UI-4 — Live WebSocket delivery (offline→online) + add-people (real multi-user chat)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`).

**Goal:** Make the live socket actually connect (status "offline" → "online") by deriving the WS URL from the worker's own origin (so it traverses the Vite dev proxy / same-origin ingress), and add an "add people" affordance to the conversation header so two real users can be in a channel and exchange E2E messages over WebSocket.

**Architecture:** Fix the protocol worker's WS URL (uses a wrong hardcoded `ws://localhost:8080/ws` default that isn't proxied). The conversation header gets an "add people" action wired to the existing `addMember` use-case (KT-verify → MLS add → roster → deliver). Inbound delivery already flows through the orchestrator. WS auth uses the device-bound token via `?access_token=` (DS gateway already accepts it — UI-3/6.1 fix).

**Tech Stack:** SolidJS, vitest. Existing: `protocol.worker.ts` (`connect` builds `realSocket(url, token)`), `ProtocolClient.connect(token)`, `orchestrator.connect(token)` (fires after login via authFlow), `features/add-member/addMember.ts` (`addMember(deps,{wsId,group,identity,currentMaxSeq})`, KT-verified), `WorkspaceClient.searchMembers`, `KTVerifier` (`shared/lib/kt/client.ts`).

---

### Task 1: Fix WS URL → online

**Files:** Modify `client/src/shared/lib/transport/protocol.worker.ts`, `client/src/shared/config/env.ts`. Test: `client/src/shared/lib/transport/wsurl.test.ts` (pure helper).

- [ ] **Step 1:** Extract the WS-URL resolution into a pure, testable helper and fix the default to be same-origin (proxied in dev, same-origin in prod). In `protocol.worker.ts`:
```ts
// Resolve the DS WebSocket URL. Prefer an explicit override; else same-origin /ws
// (dev: Vite proxies /ws to the backend; prod: same ingress). The previous hardcoded
// ws://localhost:8080/ws was wrong (unproxied, wrong port) → the socket never connected.
export function resolveWsUrl(override: string | undefined, loc: { protocol: string; host: string }): string {
  if (override) return override;
  const scheme = loc.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${loc.host}/ws`;
}
```
In the `connect` case use `const url = resolveWsUrl((self as any).DS_WS_URL, self.location);`.
- [ ] **Step 2: Test** `wsurl.test.ts`: `resolveWsUrl(undefined, {protocol:"http:",host:"localhost:5173"})` === `"ws://localhost:5173/ws"`; `resolveWsUrl(undefined, {protocol:"https:",host:"app.x"})` === `"wss://app.x/ws"`; `resolveWsUrl("ws://o/ws", anything)` === `"ws://o/ws"`. (Export `resolveWsUrl` from the worker module; if importing the worker module in a test pulls worker globals, move `resolveWsUrl` to a tiny sibling `wsurl.ts` and import it from the worker — prefer that.) RED→GREEN.
- [ ] **Step 3:** `env.ts` — set `DS_WS_URL = (import.meta as any).env?.VITE_DS_WS_URL ?? ""` (empty → worker derives same-origin). (DS_WS_URL isn't currently passed to the worker; that's fine — the worker derives it. Leave the export for future explicit override.)
- [ ] **Step 4:** `npx vitest run && npx tsc --noEmit && npx vite build` green. **Live check:** log in as alice → the conversation header status should flip from "offline" to "online" (the WS connects through the proxy and authenticates with the device-bound token). Report what the status shows.
- [ ] **Step 5:** Commit `fix(client): derive WS URL from origin so the socket connects (offline→online)`.

---

### Task 2: Add people to a channel (real multi-user E2E)

**Files:** Modify `widgets/conversation-view/ConversationView.tsx` (header "add people" button), `app/conversations.ts` (add `addPeople(identity)`), `bootstrap.ts` (wire `addMember` use-case + KTVerifier), `main.tsx` (AddPeopleModal reuse NewDmModal-style search). Create `widgets/add-people-modal/AddPeopleModal.tsx` (or reuse the member-search modal). Tests for the controller method + modal.

- [ ] **Step 1:** `conversations.ts` — add `addPeople(identity: string): Promise<void>` that calls an injected `addMember` dep with `{wsId: wsId(), group: activeId(), identity, currentMaxSeq: <0 or tracked>}`. (currentMaxSeq: use 0 for now — the join_seq semantics mean the new member sees messages after join; document that history-before-join isn't delivered, which is the MLS property.) Unit test: `addPeople("bob")` calls `addMember({wsId,group:active,identity:"bob",currentMaxSeq:0})`.
- [ ] **Step 2:** `bootstrap.ts` — construct a `KTVerifier` over a `KTClient(DS_HTTP_URL)`, and wire the controller's `addMember` dep to call the `features/add-member` use-case with `{conversations: convClient, crypto: cryptoClient, kt: ktVerifier, protocol: {sendCommit, sendWelcome}}`. For `protocol.sendCommit(group, bytes)` / `sendWelcome(group, deviceId, bytes)`: map to `orchestrator`/`protocol.send` with the right `content_type` (commit → `CONTENT_TYPE.commit`, welcome → `CONTENT_TYPE.welcome`) and base64 — add thin `sendCommit`/`sendWelcome` helpers (mirror orchestrator.sendText's encode/send, but the bytes are already ciphertext/handshake, so just base64 + send with the content_type). Put these helpers in the orchestrator or bootstrap.
- [ ] **Step 3:** ConversationView header: an "Add people" `IconButton`/`Button` (only for channels) → opens a search modal (reuse the NewDmModal pattern: search via `wsClient.searchMembers`, pick → `conversations.addPeople(username)`). Wire in main.tsx with an `addPeopleOpen` signal + results.
- [ ] **Step 4:** `npx vitest run && npx tsc --noEmit && npx vite build` green; `cd ../backend && go build ./...`. **Live check (two users):** register a 2nd user (bob) in another browser/profile; as alice, create #general, "Add people" → search bob → add; both send messages and SEE each other's (over WS). Report the result. (If two-profile testing isn't feasible in-session, at least confirm: add-people calls succeed against the backend — bob's device gets added to the roster — and alice→ self optimistic echo still works; note the cross-user verification as a manual step.)
- [ ] **Step 5:** Commit `feat(client): add people to channels (KT-verified MLS add) for real multi-user chat`.

---

## Self-Review

**Spec coverage (UI-4 slice):** WS URL fix → online (T1); add-people for real multi-user E2E delivery (T2). ✓ join-public (deferred from UI-3) — still deferred unless trivial; the `is_member` flag the list lacks is the blocker (note it for a follow-up: add `is_member` to the conversations list payload, then a "Join" affordance). Animations + onboarding polish = UI-5.

**Placeholders:** `resolveWsUrl` is pure + unit-tested; add-member wiring reuses the merged `features/add-member` (KT-verified, fail-closed). `currentMaxSeq=0` documented (MLS delivers from join forward). Cross-user live check may be a documented manual step if two profiles aren't drivable in-session.

**Type consistency:** `resolveWsUrl` signature; controller `addPeople` ↔ `addMember` use-case deps (`conversations`/`crypto`/`kt`/`protocol` ports from CM-6); `KTVerifier.verifyIdentity` ↔ addMember; `CONTENT_TYPE.commit/welcome` ↔ orchestrator inbound (already routes them). 

**Risk:** the WS gateway must accept the device-bound token via `?access_token=` (already implemented, 6.1/CV-7) and validate the session — verified live by the online status. If status stays offline, check the proxy `/ws` (ws:true) and the token.
