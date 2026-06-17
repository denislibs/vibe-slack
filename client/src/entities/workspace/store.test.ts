import { describe, it, expect } from "vitest";
import { createWorkspaceStore } from "./store";

describe("workspace store", () => {
  it("tracks my workspaces and the current selection", () => {
    const s = createWorkspaceStore();
    expect(s.current()).toBeNull();
    s.setList([{ id: "w1", name: "Acme", slug: "acme", role: "owner" }]);
    expect(s.list()).toHaveLength(1);
    s.select("w1");
    expect(s.current()).toBe("w1");
    s.clear();
    expect(s.current()).toBeNull();
    expect(s.list()).toHaveLength(0);
  });
});
