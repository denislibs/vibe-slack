import { sha256 } from "@noble/hashes/sha2.js";

function u32(n: number): Uint8Array {
  const b = new Uint8Array(4); new DataView(b.buffer).setUint32(0, n, false); return b;
}
function u64(n: bigint): Uint8Array {
  const b = new Uint8Array(8); new DataView(b.buffer).setBigUint64(0, n, false); return b;
}
function concat(parts: Uint8Array[]): Uint8Array {
  const len = parts.reduce((a, p) => a + p.length, 0);
  const out = new Uint8Array(len); let o = 0;
  for (const p of parts) { out.set(p, o); o += p.length; }
  return out;
}
function cmp(a: Uint8Array, b: Uint8Array): number {
  const n = Math.min(a.length, b.length);
  for (let i = 0; i < n; i++) if (a[i] !== b[i]) return a[i] - b[i];
  return a.length - b.length;
}

// canonicalLeaf mirrors backend kt.CanonicalLeaf exactly (keys sorted asc by raw bytes).
export function canonicalLeaf(identity: string, version: bigint, deviceSet: Uint8Array[]): Uint8Array {
  const keys = [...deviceSet].sort(cmp);
  const id = new TextEncoder().encode(identity);
  const parts: Uint8Array[] = [u32(id.length), id, u64(version), u32(keys.length)];
  for (const k of keys) { parts.push(u32(k.length), k); }
  return concat(parts);
}

export function parseCanonicalLeaf(buf: Uint8Array): { identity: string; version: bigint; keys: Uint8Array[] } {
  const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
  let o = 0;
  const idLen = dv.getUint32(o, false); o += 4;
  const identity = new TextDecoder().decode(buf.subarray(o, o + idLen)); o += idLen;
  const version = dv.getBigUint64(o, false); o += 8;
  const count = dv.getUint32(o, false); o += 4;
  const keys: Uint8Array[] = [];
  for (let i = 0; i < count; i++) {
    const kl = dv.getUint32(o, false); o += 4;
    keys.push(buf.subarray(o, o + kl)); o += kl;
  }
  return { identity, version, keys };
}

export function leafHash(canonical: Uint8Array): Uint8Array {
  return sha256(concat([new Uint8Array([0x00]), canonical]));
}
export function nodeHash(left: Uint8Array, right: Uint8Array): Uint8Array {
  return sha256(concat([new Uint8Array([0x01]), left, right]));
}
