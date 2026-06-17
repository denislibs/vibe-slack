// @vitest-environment node
import { describe, it, expect } from "vitest";
import * as ed from "@noble/ed25519";
import { sha512 } from "@noble/hashes/sha2.js";
import { sthMessage, verifySTHSignature } from "./sth";

// @noble/ed25519 v3 needs the sha512 hook for keygen/signing in node.
ed.hashes.sha512 = sha512;

describe("kt sth", () => {
  it("sthMessage = 'KTSTHv1' || u64 BE tree_size || root", () => {
    const root = new Uint8Array(32).fill(7);
    const msg = sthMessage(5n, root);
    const prefix = new TextEncoder().encode("KTSTHv1");
    const expected = new Uint8Array([...prefix, 0, 0, 0, 0, 0, 0, 0, 5, ...root]);
    expect(msg).toEqual(expected);
  });

  it("verifies a valid Ed25519 STH signature and rejects tampering", async () => {
    const priv = ed.utils.randomSecretKey();
    const pub = await ed.getPublicKeyAsync(priv);
    const root = new Uint8Array(32).fill(3);
    const msg = sthMessage(9n, root);
    const sig = await ed.signAsync(msg, priv);
    expect(await verifySTHSignature(pub, { treeSize: 9n, rootHash: root, signature: sig })).toBe(true);
    // tampered root
    const badRoot = new Uint8Array(32).fill(4);
    expect(await verifySTHSignature(pub, { treeSize: 9n, rootHash: badRoot, signature: sig })).toBe(false);
    // tampered size
    expect(await verifySTHSignature(pub, { treeSize: 10n, rootHash: root, signature: sig })).toBe(false);
  });
});
