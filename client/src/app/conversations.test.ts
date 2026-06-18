import { describe, it, expect, vi } from "vitest";
import { createConversationsController } from "./conversations";

function deps(list: any[] = []) {
  const added: any[] = [];
  return {
    client: { list: vi.fn(async () => list) },
    create: vi.fn(async (args: any) => ({ group_id: "gNEW", name: args.name ?? "dm", type: args.type })),
    sendText: vi.fn(),
    addMember: vi.fn(async () => {}),
    track: vi.fn(),
    conversation: { addMessage: vi.fn((g: string, m: any) => added.push([g, m])) },
    token: () => "TOK",
    wsId: () => "w1",
    userLabel: () => "alice@corp",
    _added: added,
  };
}

describe("conversations controller", () => {
  it("load() splits server list into channels and dms", async () => {
    const d = deps([
      { group_id: "c1", type: "channel", visibility: "public", name: "general" },
      { group_id: "d1", type: "dm", visibility: "private", name: "bob" },
    ]);
    const c = createConversationsController(d as any);
    await c.load();
    expect(d.client.list).toHaveBeenCalledWith("TOK", "w1");
    expect(c.channels()).toEqual([{ id: "c1", name: "general", visibility: "public" }]);
    expect(c.dms()).toEqual([{ id: "d1", name: "bob" }]);
    // auto-selects the first channel so the composer isn't a silent no-op
    expect(c.activeId()).toBe("c1");
  });

  it("load() auto-selects a DM when there are no channels", async () => {
    const d = deps([{ group_id: "d1", type: "dm", visibility: "private", name: "bob" }]);
    const c = createConversationsController(d as any);
    await c.load();
    expect(c.activeId()).toBe("d1");
  });

  it("load() leaves an already-selected conversation untouched", async () => {
    const d = deps([
      { group_id: "c1", type: "channel", visibility: "public", name: "general" },
      { group_id: "c2", type: "channel", visibility: "public", name: "random" },
    ]);
    const c = createConversationsController(d as any);
    c.select("c2");
    await c.load();
    expect(c.activeId()).toBe("c2");
  });

  it("load() tracks every conversation so history backfills", async () => {
    const d = deps([
      { group_id: "c1", type: "channel", visibility: "public", name: "general" },
      { group_id: "d1", type: "dm", visibility: "private", name: "bob" },
    ]);
    const c = createConversationsController(d as any);
    await c.load();
    expect(d.track).toHaveBeenCalledWith("c1", 0);
    expect(d.track).toHaveBeenCalledWith("d1", 0);
  });

  it("createChannel creates, reloads, and selects the new group", async () => {
    const d = deps();
    d.client.list = vi.fn(async () => [{ group_id: "gNEW", type: "channel", visibility: "private", name: "secret" }]);
    const c = createConversationsController(d as any);
    await c.createChannel("secret", "private");
    expect(d.create).toHaveBeenCalledWith({ type: "channel", visibility: "private", name: "secret" });
    expect(c.activeId()).toBe("gNEW");
    expect(c.channels()).toEqual([{ id: "gNEW", name: "secret", visibility: "private" }]);
  });

  it("startDm creates a dm, reloads, selects", async () => {
    const d = deps();
    d.client.list = vi.fn(async () => [{ group_id: "gNEW", type: "dm", visibility: "private", name: "bob" }]);
    const c = createConversationsController(d as any);
    await c.startDm("bob");
    expect(d.create).toHaveBeenCalledWith({ type: "dm", emailOrUsername: "bob" });
    expect(c.activeId()).toBe("gNEW");
  });

  it("send does optimistic echo + sendText to the active group", async () => {
    const d = deps();
    const c = createConversationsController(d as any);
    c.select("c1");
    await c.send("hi");
    expect(d.conversation.addMessage).toHaveBeenCalled();
    const [g, m] = d._added[0];
    expect(g).toBe("c1");
    expect(m.sender).toBe("alice@corp");
    expect(m.text).toBe("hi");
    expect(typeof m.seq).toBe("number");
    expect(d.sendText).toHaveBeenCalledWith("c1", "hi");
  });

  it("send with no active conversation is a no-op", async () => {
    const d = deps();
    const c = createConversationsController(d as any);
    await c.send("hi");
    expect(d.sendText).not.toHaveBeenCalled();
    expect(d.conversation.addMessage).not.toHaveBeenCalled();
  });

  it("addPeople calls addMember dep for the active channel (currentMaxSeq=0)", async () => {
    const d = deps();
    const c = createConversationsController(d as any);
    c.select("c1");
    await c.addPeople("bob");
    expect(d.addMember).toHaveBeenCalledWith({ wsId: "w1", group: "c1", identity: "bob", currentMaxSeq: 0 });
  });

  it("addPeople with no active conversation is a no-op", async () => {
    const d = deps();
    const c = createConversationsController(d as any);
    await c.addPeople("bob");
    expect(d.addMember).not.toHaveBeenCalled();
  });
});
