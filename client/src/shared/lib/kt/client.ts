import { parseCanonicalLeaf, leafHash } from "./hash.js";
import { verifyInclusion, verifyConsistency } from "./proof.js";
import { verifySTHSignature, type STH } from "./sth.js";
import type { KeyRecord } from "../../api/kt.js";

export class KTError extends Error {} // base
export class KTNotPublished extends KTError {} // 404 from key()
export class KTInvalidSTH extends KTError {}
export class KTForked extends KTError {}
export class KTInvalidInclusion extends KTError {}
export class KTIdentityMismatch extends KTError {}

export interface KTApi {
  pubKey(token: string): Promise<Uint8Array>;
  key(token: string, identity: string): Promise<KeyRecord>;
  consistency(token: string, from: bigint, to: bigint): Promise<Uint8Array[]>;
}

export class KTVerifier {
  private pub: Uint8Array | null = null;
  private trusted: STH | null = null;
  constructor(private api: KTApi) {}

  // verifyIdentity returns the identity's verified device signing keys, or throws.
  async verifyIdentity(token: string, identity: string): Promise<Uint8Array[]> {
    if (!this.pub) this.pub = await this.api.pubKey(token);
    let rec: KeyRecord;
    try {
      rec = await this.api.key(token, identity);
    } catch {
      throw new KTNotPublished(`no KT record for ${identity}`);
    }
    if (!(await verifySTHSignature(this.pub, rec.sth))) throw new KTInvalidSTH("bad STH signature");
    // monotonicity: an append-only log can only grow. Reject a smaller tree
    // (rewind/rollback to a stale, possibly since-rotated key set), require a
    // consistency proof when it grows, and reject a divergent root at equal size.
    if (this.trusted) {
      if (rec.sth.treeSize < this.trusted.treeSize) {
        throw new KTForked("STH tree size went backwards (rewind)");
      } else if (rec.sth.treeSize === this.trusted.treeSize) {
        if (!eqBytes(this.trusted.rootHash, rec.sth.rootHash)) {
          throw new KTForked("same tree size, different root");
        }
      } else {
        const proof = await this.api.consistency(token, this.trusted.treeSize, rec.sth.treeSize);
        if (
          !verifyConsistency(
            this.trusted.rootHash,
            this.trusted.treeSize,
            rec.sth.rootHash,
            rec.sth.treeSize,
            proof,
          )
        ) {
          throw new KTForked("consistency check failed");
        }
      }
    }
    if (!verifyInclusion(leafHash(rec.deviceSet), rec.leafIndex, rec.sth.treeSize, rec.auditPath, rec.sth.rootHash)) {
      throw new KTInvalidInclusion("inclusion proof failed");
    }
    const parsed = parseCanonicalLeaf(rec.deviceSet);
    if (parsed.identity !== identity) throw new KTIdentityMismatch(`leaf identity ${parsed.identity} != ${identity}`);
    // Advance trust only after the STH is fully validated AND this leaf's
    // inclusion/identity check off the same head — never on a partial failure.
    this.trusted = rec.sth;
    return parsed.keys;
  }
}

function eqBytes(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}
