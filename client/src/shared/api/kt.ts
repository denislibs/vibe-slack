import type { STH } from "../lib/kt/sth";

type FetchFn = typeof fetch;

export interface KeyRecord {
  leafIndex: bigint;
  version: bigint;
  deviceSet: Uint8Array;
  auditPath: Uint8Array[];
  sth: STH;
}

function b64ToBytes(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}
function toSTH(j: { tree_size: number; root_hash: string; signature: string }): STH {
  return { treeSize: BigInt(j.tree_size), rootHash: b64ToBytes(j.root_hash), signature: b64ToBytes(j.signature) };
}

// Typed client for the KT verifiable-log endpoints (Bearer session token).
export class KTClient {
  // Default wraps fetch in an arrow so it's invoked unbound (calling native fetch as
  // a method, this.fetchFn(...), throws "Illegal invocation").
  constructor(private baseURL: string, private fetchFn: FetchFn = (...args) => fetch(...args)) {}

  private async get<T>(token: string, path: string): Promise<T> {
    const res = await this.fetchFn(this.baseURL + path, {
      method: "GET",
      headers: { Authorization: `Bearer ${token}` },
      credentials: "include",
    });
    if (!res.ok) throw new Error(`kt GET ${path} failed: ${res.status}`);
    return (await res.json()) as T;
  }

  async pubKey(token: string): Promise<Uint8Array> {
    const j = await this.get<{ kt_public_key: string }>(token, "/kt/pubkey");
    return b64ToBytes(j.kt_public_key);
  }
  async sth(token: string): Promise<STH> {
    return toSTH(await this.get(token, "/kt/sth"));
  }
  async key(token: string, identity: string): Promise<KeyRecord> {
    const j = await this.get<{
      leaf_index: number; version: number; device_set: string; audit_path: string[];
      sth: { tree_size: number; root_hash: string; signature: string };
    }>(token, `/kt/key/${encodeURIComponent(identity)}`);
    return {
      leafIndex: BigInt(j.leaf_index),
      version: BigInt(j.version),
      deviceSet: b64ToBytes(j.device_set),
      auditPath: j.audit_path.map(b64ToBytes),
      sth: toSTH(j.sth),
    };
  }
  async consistency(token: string, from: bigint, to: bigint): Promise<Uint8Array[]> {
    const j = await this.get<{ proof: string[] }>(token, `/kt/proof/consistency?from=${from}&to=${to}`);
    return j.proof.map(b64ToBytes);
  }
}
