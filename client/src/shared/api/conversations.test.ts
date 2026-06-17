import { describe, it, expect, vi } from "vitest";
import { ConversationsClient } from "./conversations";

function mockFetch(status: number, body: unknown) {
  return vi.fn(async (_url: URL | RequestInfo, _init?: RequestInit) => ({ ok: status < 400, status, json: async () => body }) as unknown as Response);
}

function b64(bytes: number[]): string {
  return btoa(String.fromCharCode(...bytes));
}

describe("ConversationsClient", () => {
  it("create posts the right url/body with bearer and returns the conversation", async () => {
    const f = mockFetch(200, { group_id: "g1", type: "dm", visibility: "private", name: "bob" });
    const c = new ConversationsClient("http://api", f);
    const conv = await c.create("TOK", "w1", { type: "dm", emailOrUsername: "bob" });
    expect(conv.group_id).toBe("g1");
    expect(conv.type).toBe("dm");
    const [url, init] = f.mock.calls[0];
    expect(url).toBe("http://api/workspaces/w1/conversations");
    expect((init as RequestInit).method).toBe("POST");
    expect((init as any).headers.Authorization).toBe("Bearer TOK");
    expect(JSON.parse((init as any).body)).toEqual({ type: "dm", email_or_username: "bob" });
  });

  it("create includes visibility and name when given", async () => {
    const f = mockFetch(200, { group_id: "g2", type: "channel", visibility: "public", name: "general" });
    const c = new ConversationsClient("http://api", f);
    const conv = await c.create("TOK", "w1", { type: "channel", visibility: "public", name: "general" });
    expect(conv.name).toBe("general");
    expect(JSON.parse((f.mock.calls[0][1] as any).body)).toEqual({ type: "channel", visibility: "public", name: "general" });
  });

  it("list returns conversations in the workspace", async () => {
    const f = mockFetch(200, [{ group_id: "g1", type: "dm", visibility: "private", name: "bob" }]);
    const c = new ConversationsClient("http://api", f);
    const list = await c.list("TOK", "w1");
    expect(list).toHaveLength(1);
    expect(list[0].group_id).toBe("g1");
    expect(f.mock.calls[0][0]).toBe("http://api/workspaces/w1/conversations");
    expect((f.mock.calls[0][1] as RequestInit).method).toBe("GET");
  });

  it("addUser posts email_or_username to the group", async () => {
    const f = mockFetch(200, { user_id: "u2", username: "bob", email: "bob@c", role: "member" });
    const c = new ConversationsClient("http://api", f);
    const m = await c.addUser("TOK", "g 1", "bob");
    expect(m.user_id).toBe("u2");
    expect(f.mock.calls[0][0]).toBe("http://api/conversations/g%201/users");
    expect((f.mock.calls[0][1] as RequestInit).method).toBe("POST");
    expect(JSON.parse((f.mock.calls[0][1] as any).body)).toEqual({ email_or_username: "bob" });
  });

  it("join posts to the group join endpoint", async () => {
    const f = mockFetch(200, { ok: true });
    const c = new ConversationsClient("http://api", f);
    const r = await c.join("TOK", "g1");
    expect(r.ok).toBe(true);
    expect(f.mock.calls[0][0]).toBe("http://api/conversations/g1/join");
    expect((f.mock.calls[0][1] as RequestInit).method).toBe("POST");
  });

  it("get returns the conversation with members", async () => {
    const f = mockFetch(200, { group_id: "g1", type: "channel", visibility: "public", name: "general", members: [{ user_id: "u1" }] });
    const c = new ConversationsClient("http://api", f);
    const conv = await c.get("TOK", "g1");
    expect(conv.group_id).toBe("g1");
    expect(conv.members).toHaveLength(1);
    expect(f.mock.calls[0][0]).toBe("http://api/conversations/g1");
    expect((f.mock.calls[0][1] as RequestInit).method).toBe("GET");
  });

  it("addDeviceToRoster posts device_id and join_seq", async () => {
    const f = mockFetch(200, { ok: true });
    const c = new ConversationsClient("http://api", f);
    const r = await c.addDeviceToRoster("TOK", "g1", "dev-7", 42);
    expect(r.ok).toBe(true);
    expect(f.mock.calls[0][0]).toBe("http://api/conversations/g1/members");
    expect((f.mock.calls[0][1] as RequestInit).method).toBe("POST");
    expect(JSON.parse((f.mock.calls[0][1] as any).body)).toEqual({ device_id: "dev-7", join_seq: 42 });
  });

  it("members posts device roster entry", async () => {
    const f = mockFetch(200, { ok: true });
    const c = new ConversationsClient("http://api", f);
    const r = await c.members("TOK", "g1", "dev-7", 42);
    expect(r.ok).toBe(true);
    expect(f.mock.calls[0][0]).toBe("http://api/conversations/g1/members");
    expect(JSON.parse((f.mock.calls[0][1] as any).body)).toEqual({ device_id: "dev-7", join_seq: 42 });
  });

  it("keyMaterial gets url-encoded identity and b64-decodes byte fields", async () => {
    const f = mockFetch(200, [
      { device_id: "d1", signing_public_key: b64([1, 2, 3]), key_package: b64([4, 5, 6]) },
    ]);
    const c = new ConversationsClient("http://api", f);
    const km = await c.keyMaterial("TOK", "w1", "bob@corp.com");
    expect(f.mock.calls[0][0]).toBe("http://api/workspaces/w1/users/bob%40corp.com/key-material");
    expect((f.mock.calls[0][1] as RequestInit).method).toBe("GET");
    expect(km).toHaveLength(1);
    expect(km[0].deviceId).toBe("d1");
    expect(Array.from(km[0].signingPublicKey)).toEqual([1, 2, 3]);
    expect(Array.from(km[0].keyPackage)).toEqual([4, 5, 6]);
  });

  it("getGroupInfo returns decoded bytes", async () => {
    const f = mockFetch(200, { group_info: b64([7, 8, 9]) });
    const c = new ConversationsClient("http://api", f);
    const gi = await c.getGroupInfo("TOK", "g1");
    expect(f.mock.calls[0][0]).toBe("http://api/conversations/g1/group-info");
    expect(gi).not.toBeNull();
    expect(Array.from(gi as Uint8Array)).toEqual([7, 8, 9]);
  });

  it("getGroupInfo returns null on 404", async () => {
    const f = mockFetch(404, { error: "not found" });
    const c = new ConversationsClient("http://api", f);
    const gi = await c.getGroupInfo("TOK", "g1");
    expect(gi).toBeNull();
  });

  it("putGroupInfo puts b64-encoded group_info", async () => {
    const f = mockFetch(200, { ok: true });
    const c = new ConversationsClient("http://api", f);
    const r = await c.putGroupInfo("TOK", "g1", new Uint8Array([7, 8, 9]));
    expect(r.ok).toBe(true);
    expect(f.mock.calls[0][0]).toBe("http://api/conversations/g1/group-info");
    expect((f.mock.calls[0][1] as RequestInit).method).toBe("PUT");
    expect(JSON.parse((f.mock.calls[0][1] as any).body)).toEqual({ group_info: b64([7, 8, 9]) });
  });

  it("complianceKeyPackage returns decoded bytes", async () => {
    const f = mockFetch(200, { key_package: b64([10, 11]) });
    const c = new ConversationsClient("http://api", f);
    const kp = await c.complianceKeyPackage("TOK");
    expect(f.mock.calls[0][0]).toBe("http://api/keypackages/compliance");
    expect(kp).not.toBeNull();
    expect(Array.from(kp as Uint8Array)).toEqual([10, 11]);
  });

  it("complianceKeyPackage returns null on 404", async () => {
    const f = mockFetch(404, { error: "not configured" });
    const c = new ConversationsClient("http://api", f);
    const kp = await c.complianceKeyPackage("TOK");
    expect(kp).toBeNull();
  });

  it("throws on other non-2xx", async () => {
    const f = mockFetch(500, { error: "boom" });
    const c = new ConversationsClient("http://api", f);
    await expect(c.list("TOK", "w1")).rejects.toThrow();
    await expect(c.getGroupInfo("TOK", "g1")).rejects.toThrow();
  });
});
