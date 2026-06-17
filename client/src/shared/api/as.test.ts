import { describe, it, expect, vi } from "vitest";
import { AsClient } from "./as";

function mockFetch(routes: Record<string, { status: number; body: unknown }>) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const key = `${init?.method ?? "GET"} ${new URL(url).pathname}`;
    const r = routes[key];
    if (!r) throw new Error("no route " + key);
    return { status: r.status, ok: r.status < 400, json: async () => r.body } as Response;
  });
}

describe("AsClient", () => {
  it("register start returns the opaque response", async () => {
    const as = new AsClient("http://as.test", mockFetch({
      "POST /auth/register/start": { status: 200, body: { opaque_registration_response: "RESP" } },
      "POST /auth/register/finish": { status: 200, body: { ok: true } },
    }) as any);
    expect(await as.registerStart("a@corp", "REQ")).toBe("RESP");
    await as.registerFinish("a@corp", "alice", "RECORD");
  });

  it("register finish posts email, username and record", async () => {
    const fetchFn = mockFetch({
      "POST /auth/register/finish": { status: 200, body: { ok: true } },
    });
    const as = new AsClient("http://as.test", fetchFn as any);
    await as.registerFinish("a@corp", "alice", "RECORD");
    const [url, init] = fetchFn.mock.calls[0];
    expect(new URL(url as string).pathname).toBe("/auth/register/finish");
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      email: "a@corp",
      username: "alice",
      opaque_registration_record: "RECORD",
    });
  });

  it("login finish returns token + enroll flag", async () => {
    const as = new AsClient("http://as.test", mockFetch({
      "POST /auth/login/start": { status: 200, body: { login_id: "L1", ke2: "KE2" } },
      "POST /auth/login/finish": { status: 200, body: { session_token: "TOK", device_enroll_required: true } },
    }) as any);
    const { loginId, ke2 } = await as.loginStart("a@corp", "KE1");
    expect(loginId).toBe("L1");
    expect(ke2).toBe("KE2");
    const fin = await as.loginFinish(loginId, "KE3");
    expect(fin).toEqual({ sessionToken: "TOK", deviceEnrollRequired: true });
  });

  it("throws on non-2xx", async () => {
    const as = new AsClient("http://as.test", mockFetch({
      "POST /auth/login/start": { status: 401, body: { error: "auth_failed" } },
    }) as any);
    await expect(as.loginStart("a@corp", "KE1")).rejects.toThrow();
  });
});
