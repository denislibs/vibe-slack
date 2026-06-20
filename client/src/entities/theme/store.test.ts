import { describe, it, expect, beforeEach } from "vitest";
import { createThemeStore } from "./store";

describe("theme store", () => {
  beforeEach(() => { localStorage.clear(); document.documentElement.removeAttribute("data-theme"); });

  it("defaults to light (Slack look) and applies data-theme to <html>", () => {
    const t = createThemeStore();
    expect(t.theme()).toBe("light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });
  it("toggle flips and persists to localStorage", () => {
    const t = createThemeStore();
    t.toggle();
    expect(t.theme()).toBe("dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    expect(localStorage.getItem("theme")).toBe("dark");
  });
  it("reads a persisted theme on init", () => {
    localStorage.setItem("theme", "dark");
    const t = createThemeStore();
    expect(t.theme()).toBe("dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  });
});
