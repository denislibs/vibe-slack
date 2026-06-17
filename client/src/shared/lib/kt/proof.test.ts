// @vitest-environment node
import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { leafHash, nodeHash } from "./hash.js";
import { verifyInclusion, verifyConsistency } from "./proof.js";

const here = dirname(fileURLToPath(import.meta.url));
const b64 = (s: string): Uint8Array => Uint8Array.from(Buffer.from(s, "base64"));

describe("verifyInclusion (hand-built 4-leaf tree)", () => {
  // Build a balanced 4-leaf tree:
  //        root
  //       /    \
  //     h01    h23
  //    /  \    /  \
  //   l0  l1  l2  l3
  const leaves = [0, 1, 2, 3].map((i) => leafHash(new Uint8Array([i])));
  const h01 = nodeHash(leaves[0], leaves[1]);
  const h23 = nodeHash(leaves[2], leaves[3]);
  const root = nodeHash(h01, h23);

  it("verifies a valid audit path for index 2", () => {
    // For leaf index 2: sibling l3 (right), then h01 (left).
    const path = [leaves[3], h01];
    expect(verifyInclusion(leaves[2], 2n, 4n, path, root)).toBe(true);
  });

  it("rejects a tampered audit path", () => {
    const path = [leaves[3], h01];
    const bad = [leaves[3], leaves[0]]; // wrong second element
    expect(verifyInclusion(leaves[2], 2n, 4n, bad, root)).toBe(false);
    void path;
  });

  it("rejects a tampered root", () => {
    const path = [leaves[3], h01];
    const badRoot = Uint8Array.from(root);
    badRoot[0] ^= 0xff;
    expect(verifyInclusion(leaves[2], 2n, 4n, path, badRoot)).toBe(false);
  });
});

describe("fixture interop (ground truth from Go KT service)", () => {
  const vectors = JSON.parse(
    readFileSync(join(here, "testdata", "kt_vectors.json"), "utf8"),
  );

  it("verifies the real inclusion proof", () => {
    const lk = vectors.lookup;
    const lh = leafHash(b64(lk.device_set));
    const path = (lk.audit_path as string[]).map(b64);
    const root = b64(lk.sth.root_hash);
    expect(
      verifyInclusion(lh, BigInt(lk.leaf_index), BigInt(lk.sth.tree_size), path, root),
    ).toBe(true);
  });

  it("rejects inclusion when the root byte is flipped", () => {
    const lk = vectors.lookup;
    const lh = leafHash(b64(lk.device_set));
    const path = (lk.audit_path as string[]).map(b64);
    const root = b64(lk.sth.root_hash);
    root[0] ^= 0xff;
    expect(
      verifyInclusion(lh, BigInt(lk.leaf_index), BigInt(lk.sth.tree_size), path, root),
    ).toBe(false);
  });

  it("verifies the real consistency proof", () => {
    const c = vectors.consistency;
    const proof = (c.proof as string[]).map(b64);
    const oldRoot = b64(c.sth_from.root_hash);
    const newRoot = b64(c.sth_to.root_hash);
    expect(
      verifyConsistency(oldRoot, BigInt(c.from), newRoot, BigInt(c.to), proof),
    ).toBe(true);
  });

  it("rejects consistency when the new root byte is flipped", () => {
    const c = vectors.consistency;
    const proof = (c.proof as string[]).map(b64);
    const oldRoot = b64(c.sth_from.root_hash);
    const newRoot = b64(c.sth_to.root_hash);
    newRoot[0] ^= 0xff;
    expect(
      verifyConsistency(oldRoot, BigInt(c.from), newRoot, BigInt(c.to), proof),
    ).toBe(false);
  });
});
