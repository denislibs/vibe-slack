# UI-5 — Slack visual-fidelity pass + animations

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`). This is a VISUAL pass — original CSS recreating the Slack *look* from reference screenshots; do NOT copy Slack's proprietary stylesheets. Verification is build/tests green + live review.

**Goal:** Raise the UI from "primitive" to a close Slack look: refined dark/light tokens, dense refined sidebar (workspace header, section headers, `#` channel rows, solid active highlight, hover), an icon rail, a slim topbar with centered search, grouped message rows (avatar + bold name + timestamp), a bordered composer with a real placeholder, channel intro empty states, refined scrollbars/typography — then Solid-Transitions animations.

**Architecture:** Mostly CSS Module + token refinement on the UI-2/3 widgets (workspace-rail, sidebar, topbar, conversation-view, message-list, composer) + ui-kit. Structure/props unchanged (tests stay green). Animations via `solid-transition-group`.

**Reference dimensions (functional, original CSS):** rail ~64px; sidebar ~260px; sidebar row height ~28px, font 15px, `#` glyph muted; active row = full-width solid highlight; topbar ~44px slim; message row padding ~8px 20px, name 15px bold, timestamp 12px muted, body 15px/1.46; composer = bordered rounded box, placeholder "Message #channel".

---

### Task 1: Token refresh + global polish + ui-kit

**Files:** `client/src/styles/theme.css`, ui-kit `*.module.css` (Button/Input/Avatar/Modal/Spinner), maybe `IconButton`.

- [ ] Refine `theme.css` tokens to a tighter Slack-like palette (DARK default): e.g. `--rail-bg:#121016; --sidebar-bg:#19171d; --sidebar-text:#cfc8d4; --sidebar-muted:#9a8fa6; --sidebar-active-bg:#4d3a63; --sidebar-active-text:#fff; --sidebar-hover:rgba(255,255,255,.06); --main-bg:#1a1d21; --text:#d1d2d3; --text-muted:#9a9b9e; --border:#35373b; --topbar-bg:#2c1733; --topbar-text:#d9d2de; --composer-bg:#222529; --accent:#7c3aed; --danger:#e01e5a; --hover-row:rgba(255,255,255,.04)`. LIGHT = classic aubergine sidebar (`--sidebar-bg:#3f0e40`, etc.) + white main. Add type tokens (`--fs-13/14/15`, `--lh:1.46`), `--row-h:28px`, refined `--radius`/`--radius-sm`. Add a custom thin scrollbar (`::-webkit-scrollbar` 8px, `--border` thumb). Set body font-size 15px.
- [ ] Polish ui-kit: Button (tighter padding, 14px/600, primary uses `--accent`, crisp hover); Input (subtle bg, 1px border, focus ring); Avatar (rounded-square `--radius-sm`, bold initials); Modal (refined panel + backdrop blur `backdrop-filter: blur(2px)`); Spinner. Keep all props/exports/test anchors.
- [ ] `npx vitest run && npx tsc --noEmit && npx vite build` green. Commit `feat(client): refined Slack-like design tokens + ui-kit polish`.

### Task 2: Sidebar + rail + topbar fidelity (the chrome)

**Files:** `widgets/sidebar/Sidebar.tsx`(+css), `widgets/workspace-rail/WorkspaceRail.tsx`(+css), `widgets/topbar/Topbar.tsx`(+css). Keep props/anchors.

- [ ] **Sidebar:** workspace header row = workspace name (bold, 18px) + a small caret + (decorative) compose icon; a thin divider. Section header rows ("Channels", "Direct messages") = small (13px) muted, with a disclosure caret `▸`/`▾` (decorative, can be static). Channel rows: `#` glyph (muted, monospace-ish) + name, 15px, row height ~28px, padding `0 16px 0 ~10px`, `border-radius:6px` with `margin:0 8px`, `:hover{background:var(--sidebar-hover)}`, active row → `background:var(--sidebar-active-bg); color:#fff` spanning the row. DM rows: small Avatar (20px) + name + presence dot. "+ Add channels"/"+ New message" as muted rows with a circled `+`. Tighten vertical rhythm (no big empty gaps).
- [ ] **Rail:** workspace square (rounded `--radius`, initial, ~40px, subtle ring when active); below it small icon buttons (decorative Home/DMs glyphs ok); `margin-top:auto` then theme-toggle IconButton + user Avatar (32px, rounded). Background `--rail-bg`.
- [ ] **Topbar:** slim (~44px), `--topbar-bg`; left: small back/forward chevrons (decorative); center: a rounded search field (max-width ~640px, `background:rgba(0,0,0,.25)`, placeholder "Search"); right: a help/`?` glyph. Subtle.
- [ ] vitest/tsc/vite green. Commit `feat(client): Slack-fidelity sidebar, rail, and topbar`.

### Task 3: Conversation header + message rows + composer + empty states

**Files:** `widgets/conversation-view/ConversationView.tsx`(+css), `widgets/message-list/MessageList.tsx`(+css), `widgets/composer/Composer.tsx`(+css). Keep `data-testid="status"`, message text rendering, `onSend`.

- [ ] **Header:** `# channelName` bold (18px) + a muted topic/members hint + the "Add people" action (UI-4) on the right; bottom border.
- [ ] **Message rows:** group consecutive messages from the same sender — first in a group shows Avatar(36) + bold name (15px) + muted timestamp (12px); subsequent show only the body indented to align (hide avatar/name), with the time appearing on hover (optional). Body 15px/1.46. Row padding `4px 20px`; `:hover{background:var(--hover-row)}`. Empty state: a channel intro block — a big `#` avatar, "This is the very beginning of the #channel channel." (muted), comfortable padding. (Message shape is `{seq,sender,text}`; group by consecutive equal `sender`.)
- [ ] **Composer:** a bordered rounded box (`1px solid var(--border)`, `--radius`, `--composer-bg`) containing a textarea-like input with placeholder `Message #channel` (or the active name) + a send affordance; Enter sends (Shift+Enter newline if feasible); a thin faux formatting strip optional. Margin around the box (`12px 20px`).
- [ ] vitest/tsc/vite green; **live review**. Commit `feat(client): grouped message rows, channel intro, and a real composer`.

### Task 4: Animations (Solid Transitions)

**Files:** add `solid-transition-group`; App view transitions, MessageList item transition, Modal open/close, theme transition.

- [ ] `cd client && npm i solid-transition-group`. Wrap the App gate views in a `<Transition>` (fade/slide between auth/workspace/chat). Wrap the message list in a `<TransitionGroup>` so new messages slide-up+fade in. Modal: scale+fade + backdrop fade. Theme switch already transitions via CSS. Respect `@media (prefers-reduced-motion: reduce)` (already in theme.css — ensure animations are disabled there). Keep all tests green (transitions shouldn't break queries; if a transition delays unmount, adjust tests minimally).
- [ ] vitest/tsc/vite green; live review. Commit `feat(client): Solid-Transitions animations (views, messages, modals)`.

---

## Self-Review

**Spec coverage:** refined tokens + ui-kit (T1); Slack-fidelity chrome — sidebar/rail/topbar (T2); message grouping + header + composer + empty states (T3); animations (T4). ✓ Original CSS recreating the look (no proprietary copy). Onboarding error-message polish (real errors vs catch-all) is a small follow-up; note it.

**Placeholders:** functional dimensions/colors given; exact pixel-perfection is iterated live (stack at :5173). Structure/props/test anchors unchanged so the 135-test suite stays green; any transition-induced unmount-timing test tweak is flagged in T4.

**Type consistency:** no API changes — purely presentational CSS + (T4) wrapping existing components in transitions. Message grouping derives from the existing `{seq,sender,text}` shape.

**Verification:** vitest + tsc + vite build green per task; visual fidelity judged live in the browser against the reference.
