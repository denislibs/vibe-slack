# UI-3 — Real conversations wired (create / select / send) — fixes `unknown group`

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`).

**Goal:** Make the sidebar show real channels/DMs, let the user create a channel and start a DM (member search), select a conversation (→ real MLS group), and send a message that encrypts to that group with optimistic local echo so it appears immediately. This removes the `unknown group` error (which came from the hardcoded `groupId="g1"`).

**Architecture:** A new app-layer controller `app/conversations.ts` orchestrates the conversation list + active selection + create/send, wiring the `ConversationsClient`, the `createConversation` use-case (UI-3 reuses `features/create-conversation`), the `conversation` store, and `orchestrator.sendText`. `bootstrap.ts` constructs it; `main.tsx`/`App.tsx` feed the Slack shell (UI-2) real `channels`/`dms`/`activeId`/`onSelect`/`onCreateChannel`/`onNewDm` and route the composer to the controller. Modals (`CreateChannelModal`, `NewDmModal`) are presentational. WS live delivery is UI-4; UI-3 uses optimistic echo for the sender's own messages.

**Tech Stack:** SolidJS, vitest. Reuses `features/create-conversation/createConversation.ts`, `shared/api/conversations.ts` (`ConversationsClient`), `shared/api/workspace.ts` (`WorkspaceClient`), `entities/conversation/store.ts` (`addMessage(groupID,{seq,sender,text})`, `messages(groupID)`), `app/orchestrator.ts` (`sendText(groupID,text)`). ui-kit `Modal`/`Input`/`Button`/`Avatar`.

**Current state:** `orchestrator.sendText(groupID, text)` encrypts + sends (no local echo). `App`/`ChatPage` (UI-2) already take `channels`/`dms`/`activeId`/`onSelect`/`onAddChannel`/`onNewDm` props — UI-2 passes empty arrays; UI-3 fills them. `createConversation(deps,args)` does server-create + MLS group(+compliance) + publish GroupInfo, returns the `Conversation`. `ConversationsClient.list(token,wsId)→Conversation[]` (`{group_id,type,visibility,name}`). `WorkspaceClient` has create/list/members/addMember (NO searchMembers yet — UI-3 adds it).

---

### Task 1: App conversations controller

**Files:** Create `client/src/app/conversations.ts` + `conversations.test.ts`.

- [ ] **Step 1: Failing test** — unit test with mocks. The controller:
```ts
export interface ConversationsControllerDeps {
  client: { list(token: string, wsId: string): Promise<{ group_id: string; type: string; visibility: string; name: string }[]> };
  create: (args: { type: "dm" | "channel"; visibility?: "public" | "private"; name?: string; emailOrUsername?: string }) => Promise<{ group_id: string; name: string; type: string }>;
  sendText: (groupId: string, text: string) => Promise<void> | void;
  conversation: { addMessage(groupID: string, m: { seq: number; sender: string; text: string }): void };
  token: () => string;
  wsId: () => string;
  userLabel: () => string;
}
export function createConversationsController(deps): {
  channels: () => {id:string;name:string;visibility:string}[];
  dms: () => {id:string;name:string}[];
  activeId: () => string;
  load(): Promise<void>;
  select(id: string): void;
  createChannel(name: string, visibility: "public"|"private"): Promise<void>;
  startDm(emailOrUsername: string): Promise<void>;
  send(text: string): Promise<void>;
}
```
Assertions:
- `load()` calls `client.list(token,wsId)`, splits by `type` → `channels` (type "channel", map `{id:group_id,name,visibility}`) and `dms` (type "dm", `{id:group_id,name}`).
- `createChannel("general","public")` calls `create({type:"channel",visibility:"public",name:"general"})`, then reloads list, then selects the new group_id (`activeId()` === new id).
- `startDm("bob")` calls `create({type:"dm",emailOrUsername:"bob"})`, reloads, selects.
- `select(id)` sets `activeId`.
- `send("hi")` with an active id: calls `conversation.addMessage(activeId, {seq:<n>, sender:userLabel(), text:"hi"})` (optimistic echo) AND `sendText(activeId,"hi")`. With no active id, it does nothing (no throw).

- [ ] **Step 2-4:** implement `conversations.ts`. Use Solid signals for `channels`/`dms`/`activeId`. Optimistic echo seq: use a local monotonic counter seeded high (e.g. `Date.now()`-based or an incrementing `1e9 + n`) so it won't collide with server seqs; document it's provisional until WS sync (UI-4). Run → PASS, tsc clean.
- [ ] **Step 5:** Commit `feat(client): app conversations controller (list/create/select/send + optimistic echo)`.

---

### Task 2: Wire controller + CreateChannelModal + composer→active group

**Files:** Modify `app/bootstrap.ts`, `main.tsx`, `app/App.tsx`; Create `widgets/create-channel-modal/CreateChannelModal.tsx`. Test: App/wiring smoke.

- [ ] **Step 1:** `bootstrap.ts` — construct `new ConversationsClient(DS_HTTP_URL)`, a `KTVerifier` (from `shared/lib/kt/client.ts`, with the `KTClient` api over DS_HTTP_URL) IF needed by createConversation (createConversation itself doesn't KT-verify — only addMember does; so KT not needed here), and the `createConversation` deps (conversations client + cryptoClient + token). Build `createConversationsController({ client: convClient, create: (args) => createConversation({conversations: convClient, crypto: cryptoClient, token}, {wsId: <current>, ...args}), sendText: orchestrator.sendText, conversation, token: () => session.token() ?? "", wsId: () => workspace.current() ?? "", userLabel: () => <email or username> })`. Return the controller.
- [ ] **Step 2:** `main.tsx` — after `workspaces.load()` on login, also `await conversations.load()` (the controller). Pass to `<App>`: `channels={conversations.channels()}`, `dms={conversations.dms()}`, `activeId={conversations.activeId()}`, `onSelect={(id)=>conversations.select(id)}`, `onCreateChannel={()=> setCreateChannelOpen(true)}` (a signal), `onSend={(text)=>conversations.send(text)}` (replaces the `orchestrator.sendText("g1",...)` stub). Render `<CreateChannelModal open=... onClose=... onCreate={(name,vis)=>{ conversations.createChannel(name,vis); close }}/>`.
- [ ] **Step 3:** `App.tsx` — replace the empty arrays/stub with the passed-through real props (`channels`/`dms`/`activeId`/`onSelect`/`onCreateChannel`/`onSend`); the `groupId` prop is replaced by `activeId` for message rendering: `conversation.messages(props.activeId)`. Keep the 3-way gate.
- [ ] **CreateChannelModal**: presentational — `Modal` with `Input aria-label="Channel name"`, a public/private toggle (radio/segmented), and a `Button` "Create" → `onCreate(name, visibility)`. 
- [ ] **Step 4:** `npx vitest run && npx tsc --noEmit && npx vite build` green; backend `go build ./...`. **Live check (stack up):** log in as alice → create a channel "general" → it appears in the sidebar and is selected → type a message → it appears (optimistic). No `unknown group` error.
- [ ] **Step 5:** Commit `feat(client): wire real conversations into the shell + create-channel modal (fixes unknown group)`.

---

### Task 3: New DM (member search) + join public + add people

**Files:** Modify `shared/api/workspace.ts` (add `searchMembers`), `app/conversations.ts` (startDm already; add `joinPublic`/`addPeople` if included), `main.tsx`/`App.tsx`/`Sidebar`; Create `widgets/new-dm-modal/NewDmModal.tsx`. Tests for searchMembers + modal.

- [ ] **Step 1:** `WorkspaceClient.searchMembers(token, wsId, q) → {user_id,username,email,role}[]` (GET `/workspaces/{wsId}/members/search?q=`), b64 n/a (plain JSON). Test against fetch mock.
- [ ] **Step 2:** `NewDmModal`: `Modal` with an `Input aria-label="Search people"` (debounced), a result list (Avatar+username+email rows from `searchMembers`), click a result → `onPick(emailOrUsername)`. Presentational; results fed via a prop `results` + `onQuery` callback (the controller/main does the search). Test: typing calls `onQuery`; clicking a result calls `onPick`.
- [ ] **Step 3:** Wire `onNewDm` → open NewDmModal; `onQuery` → `workspaceClient.searchMembers` → set results signal; `onPick` → `conversations.startDm(emailOrUsername)` → closes, selects the DM. 
- [ ] **Step 4 (join public, light):** the sidebar already lists channels the user is a member of + public channels (from `list`, which returns public channels even if not a member). For a public channel the user isn't in, `select` should `joinPublic` first. Simplify: add a `joinPublic` to the controller — when selecting a conversation the user isn't a member of (track membership via a flag from the list if available, else attempt), call `joinPublic` use-case then select. If membership info isn't in the list payload, DEFER join-public to a follow-up and just note it (don't block UI-3). Document the choice.
- [ ] **Step 5:** `npx vitest run && npx tsc --noEmit && npx vite build` green. **Live:** start a DM by searching a user (create a 2nd user first via Register in another tab if needed), and create/select channels. Commit `feat(client): new-DM via member search + join public channels`.

---

## Self-Review

**Spec coverage (UI-3 / 6.3c slice):** real sidebar channels/DMs + create channel + select → active group + send-to-real-group (fixes `unknown group`) with optimistic echo → T1/T2; new-DM via member search + join public → T3. ✓ WS live delivery is UI-4; animations/onboarding polish UI-5. Add-people-to-channel and full join-public membership tracking may defer to UI-4/follow-up (flagged in T3).

**Placeholders:** controller API + test contracts given; modals presentational; live verification against the running stack is the visual contract. Optimistic-echo seq is provisional (documented) until WS sync (UI-4). The join-public membership-tracking gap is explicitly flagged, not hidden.

**Type consistency:** controller channels/dms/activeId shape ↔ ChatPage/Sidebar props (UI-2); `createConversation` deps/args ↔ controller.create; `ConversationsClient.list` ↔ controller.load; `WorkspaceClient.searchMembers` ↔ NewDmModal wiring; `conversation.addMessage`/`messages` ↔ echo + render; `orchestrator.sendText` ↔ controller.send. App now renders `messages(activeId)` not `messages("g1")`.

**Verification:** vitest + tsc + vite build green per task; the create→select→send flow verified live in the browser (stack up: :5173 / :18080).
