import { createSignal } from "solid-js";

export type Theme = "dark" | "light";
const KEY = "theme";

function apply(t: Theme) {
  if (typeof document !== "undefined") document.documentElement.setAttribute("data-theme", t);
}

// Active UI theme: persisted in localStorage, applied as <html data-theme>. Defaults to dark.
export function createThemeStore() {
  const initial: Theme =
    (typeof localStorage !== "undefined" && localStorage.getItem(KEY)) === "light" ? "light" : "dark";
  const [theme, setTheme] = createSignal<Theme>(initial);
  apply(initial);
  const set = (t: Theme) => {
    setTheme(t);
    apply(t);
    if (typeof localStorage !== "undefined") localStorage.setItem(KEY, t);
  };
  return { theme, set, toggle: () => set(theme() === "dark" ? "light" : "dark") };
}
export type ThemeStore = ReturnType<typeof createThemeStore>;
