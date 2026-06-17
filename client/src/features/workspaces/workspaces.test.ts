import { describe, it, expect, vi } from "vitest";
import { createWorkspaces } from "./workspaces";
import { createWorkspaceStore } from "../../entities/workspace/store";

function deps(listResult: any[] = []) {
  return {
    client: {
      create: vi.fn(async () => ({ id: "w1", name: "Acme", slug: "acme", role: "owner" })),
      list: vi.fn(async () => listResult),
    },
    store: createWorkspaceStore(),
    token: () => "TOK",
  };
}

describe("workspaces feature", () => {
  it("load fills the store and auto-selects when exactly one exists", async () => {
    const d = deps([{ id: "w1", name: "Acme", slug: "acme", role: "member" }]);
    await createWorkspaces(d as any).load();
    expect(d.client.list).toHaveBeenCalledWith("TOK");
    expect(d.store.list()).toHaveLength(1);
    expect(d.store.current()).toBe("w1");
  });
  it("load does not auto-select when there are zero or many", async () => {
    const d = deps([]);
    await createWorkspaces(d as any).load();
    expect(d.store.current()).toBeNull();
  });
  it("create adds the workspace and selects it", async () => {
    const d = deps([]);
    await createWorkspaces(d as any).create("Acme");
    expect(d.client.create).toHaveBeenCalledWith("TOK", "Acme");
    expect(d.store.current()).toBe("w1");
  });
});
