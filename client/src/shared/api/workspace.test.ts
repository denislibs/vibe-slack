import { describe, it, expect, vi } from "vitest";
import { WorkspaceClient } from "./workspace";

function mockFetch(status: number, body: unknown) {
  return vi.fn(async (_url: URL | RequestInfo, _init?: RequestInit) => ({ ok: status < 400, status, json: async () => body }) as unknown as Response);
}

describe("WorkspaceClient", () => {
  it("create posts name with bearer token and returns the workspace", async () => {
    const f = mockFetch(200, { id: "w1", name: "Acme", slug: "acme", role: "owner" });
    const c = new WorkspaceClient("http://api", f);
    const ws = await c.create("TOK", "Acme");
    expect(ws.id).toBe("w1");
    const [url, init] = f.mock.calls[0];
    expect(url).toBe("http://api/workspaces");
    expect((init as RequestInit).method).toBe("POST");
    expect((init as any).headers.Authorization).toBe("Bearer TOK");
    expect(JSON.parse((init as any).body)).toEqual({ name: "Acme" });
  });

  it("list returns my workspaces", async () => {
    const f = mockFetch(200, [{ id: "w1", name: "Acme", slug: "acme", role: "member" }]);
    const c = new WorkspaceClient("http://api", f);
    const list = await c.list("TOK");
    expect(list).toHaveLength(1);
    expect(list[0].role).toBe("member");
  });

  it("addMember posts email_or_username", async () => {
    const f = mockFetch(200, { user_id: "u2", username: "bob", email: "bob@c", role: "member" });
    const c = new WorkspaceClient("http://api", f);
    const m = await c.addMember("TOK", "w1", "bob");
    expect(m.user_id).toBe("u2");
    expect(JSON.parse((f.mock.calls[0][1] as any).body)).toEqual({ email_or_username: "bob" });
  });

  it("throws on non-2xx", async () => {
    const f = mockFetch(403, { error: "forbidden", message: "nope" });
    const c = new WorkspaceClient("http://api", f);
    await expect(c.create("TOK", "X")).rejects.toThrow();
  });
});
