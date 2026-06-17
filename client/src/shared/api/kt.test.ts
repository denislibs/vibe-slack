import { describe, it, expect, vi } from "vitest";
import { KTClient } from "./kt";

function mockFetch(status: number, body: unknown) {
  return vi.fn(async (_url: RequestInfo | URL, _init?: RequestInit) =>
    ({ ok: status < 400, status, json: async () => body }) as unknown as Response);
}
const b64 = (bytes: number[]) => btoa(String.fromCharCode(...bytes));

describe("KTClient", () => {
  it("pubKey decodes base64 to bytes with bearer", async () => {
    const f = mockFetch(200, { kt_public_key: b64([1, 2, 3]) });
    const c = new KTClient("http://api", f);
    const pk = await c.pubKey("TOK");
    expect([...pk]).toEqual([1, 2, 3]);
    const [url, init] = f.mock.calls[0];
    expect(url).toBe("http://api/kt/pubkey");
    expect((init as any).headers.Authorization).toBe("Bearer TOK");
  });

  it("key decodes fields, numbers→bigint, audit_path→bytes[]", async () => {
    const f = mockFetch(200, {
      leaf_index: 2, version: 3, device_set: b64([9]),
      audit_path: [b64([1]), b64([2])],
      sth: { tree_size: 5, root_hash: b64([7]), signature: b64([8]) },
    });
    const c = new KTClient("http://api", f);
    const rec = await c.key("TOK", "carol@corp");
    expect(f.mock.calls[0][0]).toBe("http://api/kt/key/carol%40corp");
    expect(rec.leafIndex).toBe(2n);
    expect(rec.version).toBe(3n);
    expect([...rec.deviceSet]).toEqual([9]);
    expect(rec.auditPath.map((p) => [...p])).toEqual([[1], [2]]);
    expect(rec.sth.treeSize).toBe(5n);
    expect([...rec.sth.rootHash]).toEqual([7]);
  });

  it("consistency builds query and decodes proof", async () => {
    const f = mockFetch(200, { from: 3, to: 5, proof: [b64([1]), b64([2])] });
    const c = new KTClient("http://api", f);
    const proof = await c.consistency("TOK", 3n, 5n);
    expect(f.mock.calls[0][0]).toBe("http://api/kt/proof/consistency?from=3&to=5");
    expect(proof.map((p) => [...p])).toEqual([[1], [2]]);
  });

  it("throws on non-2xx", async () => {
    const f = mockFetch(404, { error: "not_found" });
    const c = new KTClient("http://api", f);
    await expect(c.key("TOK", "ghost")).rejects.toThrow();
  });
});
