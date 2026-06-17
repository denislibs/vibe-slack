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
