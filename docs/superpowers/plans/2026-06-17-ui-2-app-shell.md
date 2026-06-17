# UI-2 — App shell + Slack layout (visible Slack-look)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`).

**Goal:** Make the app visibly Slack-like: restyle the onboarding (auth + workspace pick) as polished centered cards with login/register tabs, and recompose the chat screen into the Slack 3-column shell (workspace rail + sidebar + conversation pane + topbar) with a working theme toggle. Presentational only; real conversation data/CRUD is UI-3.

**Architecture:** New presentational widgets in `client/src/widgets/` (`workspace-rail`, `sidebar`, `topbar`, `conversation-view`) + restyled `pages/auth`, `pages/workspace`, `pages/chat`, `widgets/login-form`, `message-list`, `composer`. Each uses CSS Modules consuming the theme tokens from UI-1. `App` gains a `theme: ThemeStore` prop and a `userEmail` prop; `main.tsx` passes them. UI stays prop-driven; never destructure Solid props.

**Tech Stack:** SolidJS, Vite CSS Modules, vitest + @solidjs/testing-library. ui-kit from UI-1: `Button` (variants), `IconButton`, `Input`, `Avatar` (+`colorForName`), `Modal`, `Spinner` from `shared/ui`. Theme store `entities/theme/store.ts` (`theme()`, `toggle()`).

**Current state (read before editing):** `App.tsx` is a 3-way `<Show>` gate (AuthPage / WorkspacePage / ChatPage). `AuthPage`→`LoginForm` (Email/Username/Password + Log in/Register buttons, `role="alert"` error). `WorkspacePage` (list + create form, `aria-label="Workspace name"`). `ChatPage` (`<header data-testid="status">` + `MessageList` + `Composer`). Keep all existing `aria-label`s, `data-testid="status"`, and callback prop names so current tests keep passing.

---

### Task 1: Styled onboarding (auth + workspace)

**Files:** Modify `pages/auth/AuthPage.tsx`, `widgets/login-form/LoginForm.tsx`, `pages/workspace/WorkspacePage.tsx` (+ `*.module.css`). Tests: keep existing AuthPage/WorkspacePage tests green; add minimal style-presence assertions only if useful.

- [ ] **Step 1:** Restyle as polished centered cards (Slack/Telegram onboarding feel), using ui-kit `Input`/`Button` and theme tokens:
  - **AuthPage**: full-height centered container (`background: var(--main-bg)`), a card (`--shadow`, `--radius`, padding `--space-5`, max-width ~360px) with an app title/logo mark ("Messenger") and `<h1>Sign in</h1>` (KEEP the "Sign in" text — a test asserts it).
  - **LoginForm**: convert the two-button layout into **tabs** — a "Sign in" / "Create account" segmented control (two buttons toggling a `mode` signal). In "Sign in" mode show Email + Password + a primary `Button` "Log in" calling `onLogin(email,password)`; in "Create account" mode show Email + Username + Password + a primary `Button` "Register" calling `onRegister(email,username,password)`. KEEP `aria-label="Email"|"Username"|"Password"` and the `role="alert"` error (`<Show when={props.error}>`). Use ui-kit `Input` (forwards aria-label) and `Button variant="primary"`. Existing AuthPage.test.tsx fills Email/Username/Password and clicks Register — ensure register mode is reachable and the Username field + Register button are present (e.g. default to register tab OR keep both fields rendered; simplest: keep all three inputs always rendered, just style, and provide both "Log in" and "Register" primary buttons — that preserves the existing test exactly while looking clean). Prefer the minimal change that keeps tests green: style the existing structure into a card, optionally add a tab toggle that still renders the asserted fields/buttons.
  - **WorkspacePage**: centered card, `<h1>Workspaces</h1>` (KEEP text), styled list of workspaces as clickable rows (Avatar + name, hover), and a create row (`Input aria-label="Workspace name"` + `Button` "Create"). KEEP the aria-label and "Create" button.
- [ ] **Step 2:** `npx vitest run src/pages/auth src/pages/workspace src/widgets/login-form` + the App tests → all green (existing assertions intact). `npx tsc --noEmit`.
- [ ] **Step 3:** Commit `feat(client): styled onboarding (auth card + login/register, workspace picker)`.

---

### Task 2: Slack 3-column chat shell + theme toggle

**Files:** Create `widgets/workspace-rail/WorkspaceRail.tsx`, `widgets/sidebar/Sidebar.tsx`, `widgets/topbar/Topbar.tsx`, `widgets/conversation-view/ConversationView.tsx` (+ css); Modify `pages/chat/ChatPage.tsx`, `widgets/message-list/MessageList.tsx`, `widgets/composer/Composer.tsx`, `app/App.tsx`, `main.tsx`. Test: `pages/chat/ChatPage.test.tsx` (extend) + a shell render test.

- [ ] **Step 1: Failing/extended test** — extend ChatPage test so it renders the new shell and still exposes `data-testid="status"` and the messages + composer. Add `ChatPage` props: `workspaceName: string`, `userEmail: string`, `theme: "dark"|"light"`, `onToggletheme: () => void`, plus the existing `messages/status/onSend`, and (placeholder for UI-3) `channels: {id:string;name:string;visibility:string}[]` and `dms: {id:string;name:string}[]` and `activeId: string` + `onSelect: (id)=>void` (UI-2 can pass empty arrays from App; UI-3 fills them). Assert: workspace name renders; a theme-toggle button (`aria-label="Toggle theme"`) calls `onToggleTheme`; messages + composer present; `data-testid="status"` present.

- [ ] **Step 2:** run → FAIL.

- [ ] **Step 3: Implement** the Slack shell (CSS grid `grid-template-columns: 64px 260px 1fr`, full height; topbar spans top):
  - **WorkspaceRail** (`64px`, `background: var(--rail-bg)`): vertical stack — workspace `Avatar` (initials from workspace name, `--radius`), a `+` `IconButton` (label "Create workspace", noop/prop), spacer, a theme-toggle `IconButton` (label "Toggle theme", glyph ☀/☾ by `props.theme`, calls `onToggleTheme`), and a user `Avatar` (initials from `userEmail`) at the bottom.
  - **Topbar** (`background: var(--topbar-bg)`, `color: var(--topbar-text)`, full width, ~40px): workspace name on the left, a disabled search input placeholder in the middle.
  - **Sidebar** (`260px`, `background: var(--sidebar-bg)`, `color: var(--sidebar-text)`): workspace name header; a "Channels" section listing `props.channels` (each row `# name`, `🔒` for private, hover `--sidebar-hover`, active row `--sidebar-active-bg`/`--sidebar-active-text` when `id===activeId`, click → `onSelect(id)`) + a "+ Add channels" row (noop prop for now); a "Direct messages" section listing `props.dms` (Avatar + name). Empty sections render a muted placeholder.
  - **ConversationView** (`background: var(--main-bg)`): header (`# channelName` or dm name + `status` via `data-testid="status"`), the existing `MessageList` (restyle: each message = Avatar + sender + time + text, grouped feel, scrollable, empty state "No messages yet"), and the existing `Composer` (restyle: rounded input `--composer-bg`/`--border`, send button; Enter sends).
  - **ChatPage** composes Topbar + (Rail + Sidebar + ConversationView) in the grid. Resolve the active conversation's display name from `channels`/`dms` by `activeId` (fallback "Messenger").
  - Restyle **MessageList** and **Composer** with CSS modules + tokens. Composer: keep `onSend`; add Enter-to-send (Shift+Enter newline optional).
  - **App.tsx**: add props `theme: "dark"|"light"`, `onToggleTheme: () => void`, `userEmail: string`; derive `workspaceName` from `props.workspace.list().find(w => w.id === props.workspace.current())?.name ?? "Workspace"`; pass shell props to ChatPage (channels/dms empty arrays + activeId=props.groupId + onSelect noop for now — UI-3 wires real data). Keep the 3-way gate.
  - **main.tsx**: construct/keep the theme store, pass `theme={theme.theme()}`, `onToggleTheme={() => theme.toggle()}`, and `userEmail` (capture the email entered at login — store it in a signal set in `onLogin`, default ""). Keep all existing wiring.

- [ ] **Step 4:** `npx vitest run` (all green — existing `data-testid="status"`, message render, composer, auth, workspace tests intact), `npx tsc --noEmit`, `npx vite build`.
- [ ] **Step 5:** Commit `feat(client): Slack 3-column chat shell (rail/sidebar/topbar/conversation) + theme toggle`.

---

## Self-Review

**Spec coverage (UI-2 slice):** styled onboarding + login/register → T1; Slack 3-column shell (rail/sidebar/topbar/conversation) + theme toggle + dark/light applied → T2. ✓ Real conversation list/create/select + send-to-real-group (UI-3), WS live delivery (UI-4), animations+onboarding polish (UI-5) are later plans. Sidebar shows placeholder/empty sections until UI-3 feeds real `channels`/`dms`.

**Placeholders:** layout + CSS direction + widget APIs given; visual polish reviewed live (stack up at :5173). Channels/DMs are explicit empty-array props in UI-2, filled in UI-3 — not a hidden TBD. Existing test anchors (`Sign in`, `Workspaces`, `Create`, aria-labels, `data-testid="status"`) preserved so the suite stays green.

**Type consistency:** `ThemeStore` (UI-1) → App `theme`/`onToggleTheme`; ui-kit (UI-1) used throughout; ChatPage shell props (channels/dms/activeId/onSelect) are the contract UI-3 fills. Props accessed via `props.x`.

**Verification:** vitest + tsc + vite build green per task; the Slack-like result is reviewed live in the running app after merge (frontend :5173, backend :18080).
