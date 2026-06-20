import { describe, it, expect, vi } from "vitest";
import { createConversationsController } from "./conversations";

function deps(list: any[] = []) {
  const added: any[] = [];
  return {
    client: { list: vi.fn(async () => list) },
    create: vi.fn(async (args: any) => ({ group_id: "gNEW", name: args.name ?? "dm", type: args.type })),
    sendText: vi.fn(),
    addMember: vi.fn(async () => {}),
    joinPublic: vi.fn(async () => {}),
    track: vi.fn(),
    conversation: {
      addMessage: vi.fn((g: string, m: any) => added.push([g, m])),
      // Mirror the real store: cursor = max seq currently in the group (0 if empty).
      cursor: vi.fn((g: string) => {
        const seqs = added.filter(([gg]) => gg === g).map(([, m]) => m.seq);
        return seqs.length ? Math.max(...seqs) : 0;
      }),
    },
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

  it("load() does not fetch when no workspace is active (avoids /workspaces//conversations 404)", async () => {
    const d = deps([{ group_id: "c1", type: "channel", visibility: "public", name: "general" }]);
    d.wsId = () => "";
    const c = createConversationsController(d as any);
    await c.load();
    expect(d.client.list).not.toHaveBeenCalled();
    expect(c.channels()).toEqual([]);
    expect(c.dms()).toEqual([]);
  });

  it("load() reselects the first conversation when the active one is gone (workspace switch)", async () => {
    const d = deps([{ group_id: "c9", type: "channel", visibility: "public", name: "newgen" }]);
    const c = createConversationsController(d as any);
    c.select("cOLD"); // stale selection carried over from a previous workspace
    await c.load();
    expect(c.activeId()).toBe("c9");
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
    await c.startDm({ identity: "bob", userId: "bob-uuid" });
    expect(d.create).toHaveBeenCalledWith({ type: "dm", emailOrUsername: "bob" });
    expect(c.activeId()).toBe("gNEW");
    expect(d.addMember).toHaveBeenCalledWith(expect.objectContaining({ group: "gNEW", identity: "bob", userId: "bob-uuid", skipAddUser: true, currentMaxSeq: 0 }));
  });

  it("select() joins a public channel the user hasn't joined (currentMaxSeq=0)", async () => {
    const d = deps([
      { group_id: "c1", type: "channel", visibility: "public", name: "general", member: true },
      { group_id: "c2", type: "channel", visibility: "public", name: "random", member: false },
    ]);
    const c = createConversationsController(d as any);
    await c.load();
    await c.select("c2");
    expect(d.joinPublic).toHaveBeenCalledWith({ group: "c2", currentMaxSeq: 0 });
    expect(c.activeId()).toBe("c2");
  });

  it("select() does not join a channel the user already belongs to", async () => {
    const d = deps([{ group_id: "c1", type: "channel", visibility: "public", name: "general", member: true }]);
    const c = createConversationsController(d as any);
    await c.load();
    await c.select("c1");
    expect(d.joinPublic).not.toHaveBeenCalled();
  });

  it("select() does not re-join after a successful join", async () => {
    const d = deps([
      { group_id: "c1", type: "channel", visibility: "public", name: "general", member: true },
      { group_id: "c2", type: "channel", visibility: "public", name: "random", member: false },
    ]);
    const c = createConversationsController(d as any);
    await c.load();
    await c.select("c2");
    d.joinPublic.mockClear();
    await c.select("c2");
    expect(d.joinPublic).not.toHaveBeenCalled();
  });

  it("select() never tries to join a DM", async () => {
    const d = deps([{ group_id: "d1", type: "dm", visibility: "private", name: "bob", member: true }]);
    const c = createConversationsController(d as any);
    await c.load();
    await c.select("d1");
    expect(d.joinPublic).not.toHaveBeenCalled();
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

  it("optimistic echo seqs are monotonic per group and never collide after reload", async () => {
    const d = deps();
    // Simulate a persisted echo carried over from a previous session at the base.
    d.conversation.addMessage("c1", { seq: 1_000_000_000, sender: "you", text: "old" });
    const c = createConversationsController(d as any);
    await c.select("c1");
    await c.send("new1");
    await c.send("new2");
    const seqs = d._added.filter(([g]) => g === "c1").map(([, m]) => m.seq);
    // Each echo derives from the group's current max, so they strictly increase
    // past the persisted one — no duplicate seq that the store would dedupe away.
    expect(seqs).toEqual([1_000_000_000, 1_000_000_001, 1_000_000_002]);
    expect(new Set(seqs).size).toBe(seqs.length);
  });

  it("first echo in a group with only real (server) messages jumps to the echo base", async () => {
    const d = deps();
    // Real server messages use small seqs; echoes must sort above them.
    d.conversation.addMessage("c1", { seq: 7, sender: "bob", text: "real" });
    const c = createConversationsController(d as any);
    await c.select("c1");
    await c.send("hi");
    const echo = d._added.find(([g, m]) => g === "c1" && m.text === "hi")![1];
    expect(echo.seq).toBe(1_000_000_000);
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
    await c.addPeople({ identity: "bob", userId: "bob-uuid" });
    expect(d.addMember).toHaveBeenCalledWith({ wsId: "w1", group: "c1", identity: "bob", userId: "bob-uuid", currentMaxSeq: 0 });
  });

  it("addPeople with no active conversation is a no-op", async () => {
    const d = deps();
    const c = createConversationsController(d as any);
    await c.addPeople({ identity: "bob", userId: "bob-uuid" });
    expect(d.addMember).not.toHaveBeenCalled();
  });
});
