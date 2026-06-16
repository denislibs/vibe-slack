import { describe, it, expect } from "vitest";
import { handleRequest } from "./worker";
import type { CryptoRequest } from "./protocol";

describe("worker dispatch", () => {
  it("returns a key package for a fresh engine", async () => {
    const req: CryptoRequest = { id: "r1", kind: "keyPackage" };
    const res = await handleRequest("alice@corp", req);
    expect(res.ok).toBe(true);
    if (res.ok) expect((res.result as Uint8Array).length).toBeGreaterThan(0);
  });
});
