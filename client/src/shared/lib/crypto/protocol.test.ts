import { describe, it, expect } from "vitest";
import { isCryptoResponse, type CryptoRequest } from "./protocol";

describe("crypto protocol", () => {
  it("builds a typed encrypt request", () => {
    const req: CryptoRequest = {
      id: "r1",
      kind: "encrypt",
      groupId: "team-1",
      plaintext: new Uint8Array([1, 2, 3]),
    };
    expect(req.kind).toBe("encrypt");
  });

  it("recognizes a valid response envelope", () => {
    expect(isCryptoResponse({ id: "r1", ok: true, result: null })).toBe(true);
    expect(isCryptoResponse({ nope: 1 })).toBe(false);
  });
});
