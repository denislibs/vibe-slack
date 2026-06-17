// @vitest-environment node
import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { parseCanonicalLeaf } from "./hash.js";
import type { STH } from "./sth.js";
import type { KeyRecord } from "../../api/kt.js";
import {
  KTVerifier,
  KTInvalidSTH,
  KTInvalidInclusion,
  KTNotPublished,
  KTForked,
  type KTApi,
} from "./client.js";

const here = dirname(fileURLToPath(import.meta.url));
const b64 = (s: string): Uint8Array => Uint8Array.from(Buffer.from(s, "base64"));
const vectors = JSON.parse(
  readFileSync(join(here, "testdata", "kt_vectors.json"), "utf8"),
);

interface JsonSTH {
  tree_size: number;
  root_hash: string;
  signature: string;
}
interface JsonLookup {
  identity: string;
  leaf_index: number;
  version: number;
  device_set: string;
  audit_path: string[];
  sth: JsonSTH;
}

function toSTH(j: JsonSTH): STH {
  return {
    treeSize: BigInt(j.tree_size),
    rootHash: b64(j.root_hash),
    signature: b64(j.signature),
  };
}
function toRecord(lk: JsonLookup): KeyRecord {
  return {
    leafIndex: BigInt(lk.leaf_index),
    version: BigInt(lk.version),
    deviceSet: b64(lk.device_set),
    auditPath: lk.audit_path.map(b64),
    sth: toSTH(lk.sth),
  };
}

const pub = b64(vectors.kt_public_key);

// stubApi builds a KTApi whose key() returns successive records from a queue and
// whose consistency() returns the fixture's real from->to proof.
function stubApi(records: KeyRecord[], opts: { keyThrows?: boolean } = {}): KTApi {
  let i = 0;
  return {
    async pubKey() {
      return pub;
    },
    async key() {
      if (opts.keyThrows) throw new Error("404");
      const r = records[Math.min(i, records.length - 1)];
      i++;
      return r;
    },
    async consistency() {
      return (vectors.consistency.proof as string[]).map(b64);
    },
  };
}

describe("KTVerifier.verifyIdentity (interop fixture)", () => {
  it("returns the device signing keys parsed from the fixture lookup", async () => {
    const rec = toRecord(vectors.lookup);
    const v = new KTVerifier(stubApi([rec]));
    const keys = await v.verifyIdentity("tok", vectors.lookup.identity);
    const expected = parseCanonicalLeaf(rec.deviceSet).keys;
    expect(keys.length).toBe(expected.length);
    expect(keys.length).toBeGreaterThan(0);
    for (let k = 0; k < keys.length; k++) {
      expect(Array.from(keys[k])).toEqual(Array.from(expected[k]));
    }
  });

  it("rejects a tampered STH signature with KTInvalidSTH", async () => {
    const rec = toRecord(vectors.lookup);
    rec.sth.signature = Uint8Array.from(rec.sth.signature);
    rec.sth.signature[0] ^= 0xff;
    const v = new KTVerifier(stubApi([rec]));
    await expect(v.verifyIdentity("tok", vectors.lookup.identity)).rejects.toBeInstanceOf(
      KTInvalidSTH,
    );
  });

  it("rejects a tampered device_set with KTInvalidInclusion", async () => {
    const rec = toRecord(vectors.lookup);
    // Flip a byte inside the trailing device key so the leaf still parses but its
    // hash no longer matches the audit path.
    rec.deviceSet = Uint8Array.from(rec.deviceSet);
    rec.deviceSet[rec.deviceSet.length - 1] ^= 0xff;
    const v = new KTVerifier(stubApi([rec]));
    await expect(v.verifyIdentity("tok", vectors.lookup.identity)).rejects.toBeInstanceOf(
      KTInvalidInclusion,
    );
  });

  it("maps a failing key() lookup to KTNotPublished", async () => {
    const v = new KTVerifier(stubApi([], { keyThrows: true }));
    await expect(v.verifyIdentity("tok", "ghost@corp")).rejects.toBeInstanceOf(
      KTNotPublished,
    );
  });

  it("accepts a real from->to consistency proof across observations", async () => {
    const recFrom = toRecord(vectors.lookup_from); // tree_size = from = 3
    const recTo = toRecord(vectors.lookup); // tree_size = to = 5
    const v = new KTVerifier(stubApi([recFrom, recTo]));

    // First observation seeds trusted = sth_from with a verified inclusion.
    await v.verifyIdentity("tok", vectors.lookup_from.identity);
    // Second observation at the larger size must verify via the consistency proof.
    const keys = await v.verifyIdentity("tok", vectors.lookup.identity);
    expect(keys.length).toBeGreaterThan(0);
  });

  it("rejects a fork: validly-signed STH, same tree size, different root => KTForked", async () => {
    const recTo = toRecord(vectors.lookup); // seeds trusted at size 5
    // Second observation: same tree size, a DIFFERENT root, with a real signature
    // over that forked root (from the fixture) so signature verification passes
    // and we reach the equal-size fork-detection branch.
    const forked = toRecord(vectors.lookup);
    forked.sth = toSTH(vectors.fork);
    const v = new KTVerifier(stubApi([recTo, forked]));
    await v.verifyIdentity("tok", vectors.lookup.identity); // trusted = size 5 root
    await expect(v.verifyIdentity("tok", vectors.lookup.identity)).rejects.toBeInstanceOf(
      KTForked,
    );
  });
});
