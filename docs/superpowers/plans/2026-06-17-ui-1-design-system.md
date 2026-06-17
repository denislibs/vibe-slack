# UI-1 — Design system + themes + styled ui-kit

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`).

**Goal:** Establish the visual foundation — dark/light design tokens (CSS variables on `data-theme`), a theme store (localStorage, default dark, toggle), global base styles, and styled ui-kit components (Button variants, IconButton, Input, Avatar, Modal, Spinner) — so subsequent UI plans (shell, conversations, animations) build on a Slack-like design system.

**Architecture:** A global `src/styles/theme.css` defines tokens for `[data-theme="dark"]` / `[data-theme="light"]` + base reset/typography. `src/entities/theme/store.ts` holds the active theme, persists to localStorage, applies `data-theme` to `document.documentElement`. ui-kit components in `shared/ui` get per-component CSS Modules consuming the tokens. UI stays presentational; never destructure Solid props.

**Tech Stack:** SolidJS, Vite (CSS Modules built-in), vitest + @solidjs/testing-library.

**Conventions:** existing `shared/ui` = `Button.tsx`, `Spinner.tsx`, `index.ts`. `Button` is `Component<{children, onClick?, disabled?, type?}>`. main.tsx renders App. Vite CSS Modules: `import s from "./X.module.css"; <div class={s.foo}>`. Global CSS: `import "./styles/theme.css"` in main.tsx.

---

### Task 1: Theme tokens + theme store + global base

**Files:** Create `client/src/styles/theme.css`, `client/src/entities/theme/store.ts`, `store.test.ts`; Modify `client/src/main.tsx`.

- [ ] **Step 1: Failing test** `client/src/entities/theme/store.test.ts`:
```ts
import { describe, it, expect, beforeEach } from "vitest";
import { createThemeStore } from "./store";

describe("theme store", () => {
  beforeEach(() => { localStorage.clear(); document.documentElement.removeAttribute("data-theme"); });

  it("defaults to dark and applies data-theme to <html>", () => {
    const t = createThemeStore();
    expect(t.theme()).toBe("dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  });

  it("toggle flips and persists to localStorage", () => {
    const t = createThemeStore();
    t.toggle();
    expect(t.theme()).toBe("light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
    expect(localStorage.getItem("theme")).toBe("light");
  });

  it("reads a persisted theme on init", () => {
    localStorage.setItem("theme", "light");
    const t = createThemeStore();
    expect(t.theme()).toBe("light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });
});
```

- [ ] **Step 2: Run → FAIL.** `cd client && npx vitest run src/entities/theme/store.test.ts`

- [ ] **Step 3: Implement**

`client/src/entities/theme/store.ts`:
```ts
import { createSignal } from "solid-js";

export type Theme = "dark" | "light";
const KEY = "theme";

function apply(t: Theme) {
  if (typeof document !== "undefined") document.documentElement.setAttribute("data-theme", t);
}

// Active UI theme: persisted in localStorage, applied as <html data-theme>. Defaults to dark.
export function createThemeStore() {
  const initial: Theme = (typeof localStorage !== "undefined" && localStorage.getItem(KEY)) === "light" ? "light" : "dark";
  const [theme, setTheme] = createSignal<Theme>(initial);
  apply(initial);
  const set = (t: Theme) => { setTheme(t); apply(t); if (typeof localStorage !== "undefined") localStorage.setItem(KEY, t); };
  return {
    theme,
    set,
    toggle: () => set(theme() === "dark" ? "light" : "dark"),
  };
}
export type ThemeStore = ReturnType<typeof createThemeStore>;
```

`client/src/styles/theme.css` (tokens + base; values per the spec — tunable):
```css
:root {
  --radius: 8px; --radius-sm: 6px;
  --space-1: 4px; --space-2: 8px; --space-3: 12px; --space-4: 16px; --space-5: 24px;
  --font: -apple-system, "Segoe UI", system-ui, sans-serif;
  --shadow: 0 4px 24px rgba(0,0,0,.4);
  --transition: 140ms ease;
}
:root, [data-theme="dark"] {
  --rail-bg:#15101a; --sidebar-bg:#1a1320; --sidebar-text:#cfc3d6; --sidebar-muted:#9b8fa6;
  --sidebar-active-bg:#5b2e91; --sidebar-active-text:#fff; --sidebar-hover:rgba(255,255,255,.06);
  --main-bg:#1a1d21; --text:#e8e8e9; --text-muted:#9a9b9e; --border:#2c2d30;
  --accent:#7c3aed; --accent-text:#fff; --accent-hover:#6d28d9; --composer-bg:#222529;
  --topbar-bg:#3a1d4d; --topbar-text:#efeaf3; --danger:#e01e5a; --input-bg:#222529;
}
[data-theme="light"] {
  --rail-bg:#3f0e40; --sidebar-bg:#4a154b; --sidebar-text:#e8d9ea; --sidebar-muted:#bda9bf;
  --sidebar-active-bg:#1164a3; --sidebar-active-text:#fff; --sidebar-hover:rgba(255,255,255,.10);
  --main-bg:#fff; --text:#1d1c1d; --text-muted:#616061; --border:#e2e2e2;
  --accent:#007a5a; --accent-text:#fff; --accent-hover:#148567; --composer-bg:#fff;
  --topbar-bg:#350d36; --topbar-text:#f4ecf4; --danger:#e01e5a; --input-bg:#fff;
}
* { box-sizing: border-box; }
html, body, #root { height: 100%; margin: 0; }
body {
  font-family: var(--font); background: var(--main-bg); color: var(--text);
  transition: background var(--transition), color var(--transition);
}
button { font-family: inherit; }
@media (prefers-reduced-motion: reduce) { * { transition: none !important; animation: none !important; } }
```

In `client/src/main.tsx`: add `import "./styles/theme.css";` at top, and construct the theme store early (so `data-theme` is set before render). Pass the theme store down to App later (UI-2 wires the toggle); for now constructing it in main.tsx (or bootstrap) is enough to apply the default. Keep existing wiring intact.

- [ ] **Step 4: Run → PASS;** `npx tsc --noEmit`; `npx vite build`.
- [ ] **Step 5: Commit** `feat(client): theme tokens (dark/light) + theme store + global base styles`.

---

### Task 2: Styled ui-kit components

**Files:** Modify `client/src/shared/ui/Button.tsx`, `Spinner.tsx`, `index.ts`; Create `IconButton.tsx`, `Input.tsx`, `Avatar.tsx`, `Modal.tsx` + matching `*.module.css`; Tests `client/src/shared/ui/uikit.test.tsx`.

- [ ] **Step 1: Failing tests** `client/src/shared/ui/uikit.test.tsx`:
```tsx
// @vitest-environment jsdom
import { render } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { Button, IconButton, Input, Avatar, Modal } from "./index";

describe("ui-kit", () => {
  it("Button renders variant + handles click", () => {
    const onClick = vi.fn();
    const { getByRole } = render(() => <Button variant="primary" onClick={onClick}>Go</Button>);
    const b = getByRole("button");
    b.click();
    expect(onClick).toHaveBeenCalled();
    expect(b.className).toBeTruthy();
  });
  it("Avatar shows initials derived from name", () => {
    const { getByText } = render(() => <Avatar name="alice" />);
    expect(getByText("A")).toBeTruthy(); // first letter, uppercased
  });
  it("Input forwards aria-label + value/onInput", () => {
    const onInput = vi.fn();
    const { getByLabelText } = render(() => <Input aria-label="Email" value="x" onInput={onInput} />);
    const el = getByLabelText("Email") as HTMLInputElement;
    expect(el.value).toBe("x");
  });
  it("Modal renders children only when open and fires onClose on backdrop click", () => {
    const onClose = vi.fn();
    const { getByText, getByTestId } = render(() => <Modal open onClose={onClose}><div>body</div></Modal>);
    expect(getByText("body")).toBeTruthy();
    getByTestId("modal-backdrop").click();
    expect(onClose).toHaveBeenCalled();
  });
  it("Modal renders nothing when closed", () => {
    const { queryByText } = render(() => <Modal open={false} onClose={() => {}}><div>hidden</div></Modal>);
    expect(queryByText("hidden")).toBeNull();
  });
});
```

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement** (each component a presentational `Component` consuming tokens via a `*.module.css`; do NOT destructure props):
  - **Button** (`Button.tsx` + `Button.module.css`): props `{children, onClick?, disabled?, type?, variant?: "primary"|"secondary"|"ghost"|"danger"}` (default "secondary"). Classes use `--accent`/`--danger`/transparent; hover/disabled states; `border-radius: var(--radius-sm)`, padding via space tokens. Keep the existing `onClick?.()` call style.
  - **IconButton** (`IconButton.tsx`): props `{children (icon/glyph), onClick?, label: string, active?}`; square, rounded, hover bg `--sidebar-hover`; `aria-label={props.label}`.
  - **Input** (`Input.tsx`): props `{value, onInput, "aria-label"?, type?, placeholder?, onKeyDown?}`; styled with `--input-bg`/`--border`/`--text`, focus ring `--accent`. Forward `aria-label` and `type`. (Use `props["aria-label"]`.)
  - **Avatar** (`Avatar.tsx`): props `{name: string, size?: number}`; renders a rounded square/circle with the uppercased first code point of `name` as initials; background color deterministically derived from `name` (hash → hue, `hsl`); text white. Export a `colorForName(name)` helper.
  - **Modal** (`Modal.tsx` + css): props `{open: boolean, onClose: () => void, children}`; returns `null` when `!open` (use `<Show when={props.open}>`); renders a backdrop `div` with `data-testid="modal-backdrop"` (click → `onClose`) + a centered panel (`--main-bg`, `--shadow`, `--radius`); stop propagation on panel click. (Animations added in UI-5.)
  - **Spinner**: restyle to use `--accent` (CSS spinner). 
  - Update `shared/ui/index.ts` to export Button, IconButton, Input, Avatar, Modal, Spinner (+ types, `colorForName`).
  Keep all components free of business logic (ui-kit only).

- [ ] **Step 4: Run → PASS;** `npx tsc --noEmit`; full `npx vitest run` (existing Button.test.tsx must still pass — adapt it if the Button API changed, e.g. it now accepts `variant`; the old props remain valid so it should pass); `npx vite build`.
- [ ] **Step 5: Commit** `feat(client): styled ui-kit — Button variants, IconButton, Input, Avatar, Modal`.

---

## Self-Review

**Spec coverage (UI-1 slice):** tokens dark/light on data-theme + theme store (default dark, toggle, persist) → T1; styled ui-kit (Button/IconButton/Input/Avatar/Modal/Spinner) → T2. ✓ App shell (UI-2), conversation wiring (UI-3), WS (UI-4), animations+onboarding (UI-5) are later plans.

**Placeholders:** concrete token values + component APIs + tests given. CSS visual polish is inherently reviewed live (stack is up: :5173). No TBDs.

**Type consistency:** `ThemeStore` (T1) consumed by UI-2's toggle; ui-kit exports (T2) consumed by UI-2/3/5. `Button` gains optional `variant` (back-compatible). `Avatar.colorForName` reused later. Props accessed via `props.x` (never destructured) to preserve Solid reactivity.

**Verification:** each task ends green on vitest + tsc + vite build; the visual result is reviewed in the running app after merge.
