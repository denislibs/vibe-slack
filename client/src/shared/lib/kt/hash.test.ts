// @vitest-environment node
import { describe, it, expect } from "vitest";
import { sha256 } from "@noble/hashes/sha2.js";
import { canonicalLeaf, parseCanonicalLeaf, leafHash, nodeHash } from "./hash";

const enc = (s: string) => new TextEncoder().encode(s);

describe("kt hash", () => {
  it("canonicalLeaf matches the documented big-endian layout and sorts keys", () => {
    const k1 = new Uint8Array([2, 2]);
    const k2 = new Uint8Array([1, 1]);
    const leaf = canonicalLeaf("ann", 5n, [k1, k2]); // unsorted input
    const expected = new Uint8Array([
      0,0,0,3, ...enc("ann"),
      0,0,0,0,0,0,0,5,
      0,0,0,2,
      0,0,0,2, 1,1,
      0,0,0,2, 2,2,
    ]);
    expect(leaf).toEqual(expected);
  });

  it("parseCanonicalLeaf round-trips identity, version, sorted keys", () => {
    const leaf = canonicalLeaf("bob", 9n, [new Uint8Array([3]), new Uint8Array([1])]);
    const p = parseCanonicalLeaf(leaf);
    expect(p.identity).toBe("bob");
    expect(p.version).toBe(9n);
    expect(p.keys.map((k) => [...k])).toEqual([[1], [3]]);
  });

  it("leafHash = SHA256(0x00 || leaf), nodeHash = SHA256(0x01 || l || r)", () => {
    const leaf = enc("x");
    expect(leafHash(leaf)).toEqual(sha256(new Uint8Array([0, ...leaf])));
    const l = new Uint8Array(32).fill(1), r = new Uint8Array(32).fill(2);
    expect(nodeHash(l, r)).toEqual(sha256(new Uint8Array([1, ...l, ...r])));
  });
});
