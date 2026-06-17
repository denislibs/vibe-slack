// RFC 6962 inclusion + consistency proof verification, ported faithfully from
// github.com/transparency-dev/merkle (proof.RootFromInclusionProof and
// proof.VerifyConsistency). All node/leaf hashing goes through hash.ts so the
// SHA-256 0x00/0x01 domain separation matches the Go rfc6962 hasher exactly.
import { nodeHash } from "./hash.js";

// bitLen returns the number of bits needed to represent x (bitLen(0) == 0).
function bitLen(x: bigint): number {
  let n = 0;
  while (x > 0n) {
    x >>= 1n;
    n++;
  }
  return n;
}

// popcount returns the number of set bits in x.
function popcount(x: bigint): number {
  let n = 0;
  while (x > 0n) {
    n += Number(x & 1n);
    x >>= 1n;
  }
  return n;
}

// trailingZeros returns the number of trailing zero bits in x (x must be > 0).
function trailingZeros(x: bigint): number {
  let n = 0;
  while ((x & 1n) === 0n) {
    n++;
    x >>= 1n;
  }
  return n;
}

function equal(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a[i] ^ b[i];
  return diff === 0;
}

// rootFromInclusion recomputes the Merkle root from a leaf hash and its audit path.
// Mirrors transparency-dev proof.RootFromInclusionProof.
function decompInclProof(index: bigint, size: bigint): [number, number] {
  const inner = bitLen(index ^ (size - 1n));
  const border = popcount(index >> BigInt(inner));
  return [inner, border];
}

// chainInner folds proof hashes from lower to upper levels around seed, choosing
// left/right placement from the bits of index. Mirrors proof.chainInner.
function chainInner(seed: Uint8Array, proof: Uint8Array[], index: bigint): Uint8Array {
  let res = seed;
  for (let i = 0; i < proof.length; i++) {
    if (((index >> BigInt(i)) & 1n) === 0n) {
      res = nodeHash(res, proof[i]);
    } else {
      res = nodeHash(proof[i], res);
    }
  }
  return res;
}

// chainInnerRight is like chainInner but only folds left-side siblings (bit==1),
// yielding the hash of the earlier version of the subtree. Mirrors
// proof.chainInnerRight.
function chainInnerRight(seed: Uint8Array, proof: Uint8Array[], index: bigint): Uint8Array {
  let res = seed;
  for (let i = 0; i < proof.length; i++) {
    if (((index >> BigInt(i)) & 1n) === 1n) {
      res = nodeHash(proof[i], res);
    }
  }
  return res;
}

// chainBorderRight folds left-side border siblings onto seed. Mirrors
// proof.chainBorderRight.
function chainBorderRight(seed: Uint8Array, proof: Uint8Array[]): Uint8Array {
  let res = seed;
  for (const h of proof) res = nodeHash(h, res);
  return res;
}

function rootFromInclusion(
  leafHash: Uint8Array,
  index: bigint,
  size: bigint,
  path: Uint8Array[],
): Uint8Array {
  if (index >= size) throw new Error("index beyond tree size");
  const [inner, border] = decompInclProof(index, size);
  if (path.length !== inner + border) throw new Error("wrong proof size");
  let res = chainInner(leafHash, path.slice(0, inner), index);
  res = chainBorderRight(res, path.slice(inner));
  return res;
}

// verifyInclusion returns true iff the audit path proves leafHash sits at leafIndex
// in a tree of treeSize whose root is expectedRoot.
export function verifyInclusion(
  leafHash: Uint8Array,
  leafIndex: bigint,
  treeSize: bigint,
  auditPath: Uint8Array[],
  expectedRoot: Uint8Array,
): boolean {
  let root: Uint8Array;
  try {
    root = rootFromInclusion(leafHash, leafIndex, treeSize, auditPath);
  } catch {
    return false;
  }
  return equal(root, expectedRoot);
}

// verifyConsistency returns true iff proof shows the tree of newSize (root newRoot)
// is an append-only extension of the tree of oldSize (root oldRoot).
// Ported from transparency-dev proof.VerifyConsistency.
export function verifyConsistency(
  oldRoot: Uint8Array,
  oldSize: bigint,
  newRoot: Uint8Array,
  newSize: bigint,
  proof: Uint8Array[],
): boolean {
  try {
    if (oldSize > newSize) throw new Error("oldSize > newSize");
    if (oldSize === newSize) {
      if (proof.length !== 0) throw new Error("nonempty proof for equal sizes");
      return equal(oldRoot, newRoot);
    }
    if (oldSize === 0n) {
      // Any size greater than 0 is consistent with size 0; proof must be empty.
      if (proof.length !== 0) throw new Error("nonempty proof for empty old tree");
      return true;
    }
    if (proof.length === 0) throw new Error("empty proof");

    // size1 != 0 && size1 < size2.
    const [innerFull, border] = decompInclProof(oldSize - 1n, newSize);
    const shift = trailingZeros(oldSize);
    const inner = innerFull - shift; // shift < inner since size1 < size2.

    // The proof includes the root hash for the sub-tree of size 2^shift, unless
    // size1 IS that 2^shift (a power of two), in which case the seed is root1.
    let seed: Uint8Array;
    let start: number;
    if (oldSize === 1n << BigInt(shift)) {
      seed = oldRoot;
      start = 0;
    } else {
      seed = proof[0];
      start = 1;
    }

    if (proof.length !== start + inner + border) {
      throw new Error("wrong consistency proof size");
    }
    const rest = proof.slice(start); // now length == inner + border.
    const mask = (oldSize - 1n) >> BigInt(shift); // chain from level |shift|.

    // First (older) root.
    let hash1 = chainInnerRight(seed, rest.slice(0, inner), mask);
    hash1 = chainBorderRight(hash1, rest.slice(inner));
    if (!equal(hash1, oldRoot)) return false;

    // Second (newer) root.
    let hash2 = chainInner(seed, rest.slice(0, inner), mask);
    hash2 = chainBorderRight(hash2, rest.slice(inner));
    return equal(hash2, newRoot);
  } catch {
    return false;
  }
}
