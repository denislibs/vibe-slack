import * as ed from "@noble/ed25519";

export interface STH {
  treeSize: bigint;
  rootHash: Uint8Array;
  signature: Uint8Array;
}

// sthMessage mirrors backend kt.sthMessage exactly:
// "KTSTHv1" ‖ tree_size(u64 big-endian) ‖ root_hash
export function sthMessage(treeSize: bigint, rootHash: Uint8Array): Uint8Array {
  const prefix = new TextEncoder().encode("KTSTHv1");
  const out = new Uint8Array(prefix.length + 8 + rootHash.length);
  out.set(prefix, 0);
  new DataView(out.buffer).setBigUint64(prefix.length, treeSize, false);
  out.set(rootHash, prefix.length + 8);
  return out;
}

export async function verifySTHSignature(pubKey: Uint8Array, sth: STH): Promise<boolean> {
  try {
    return await ed.verifyAsync(sth.signature, sthMessage(sth.treeSize, sth.rootHash), pubKey);
  } catch {
    return false;
  }
}
