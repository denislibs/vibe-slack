// @vitest-environment node
//
// Live end-to-end slice for subproject 6.3b: real OpenMLS engines (one per
// device, via the crypto dispatcher used by transport-demo.test.ts) prove both
// membership flows of 6.3b work through the actual WASM crypto:
//   1. DM/add path        — addMember + Welcome (the CM-6/CM-7 producer flow).
//   2. external-commit path — exportGroupInfo + joinByExternalCommit (joinPublic).
import { describe, it, expect } from "vitest";
import { createDispatcher } from "../shared/lib/crypto/worker";

type Engine = ReturnType<typeof createDispatcher>;

async function call<T>(d: Engine, req: any): Promise<T> {
  const r = (await d.handleRequest(req)) as { ok: boolean; result?: unknown; error?: string };
  if (!r.ok) throw new Error(r.error);
  return r.result as T;
}

const enc = (s: string) => new TextEncoder().encode(s);
const dec = (b: Uint8Array) => new TextDecoder().decode(b);

describe("conversation slice (real MLS, both 6.3b flows)", () => {
  it("DM/add path: alice adds bob via Welcome, then alice→bob message round-trips", async () => {
    const alice = createDispatcher("alice@corp");
    const bob = createDispatcher("bob@corp");

    const bobKp = await call<Uint8Array>(bob, { id: "1", kind: "keyPackage" });
    await call(alice, { id: "2", kind: "createGroup", groupId: "c1" });
    const add = await call<{ commit: Uint8Array; welcome: Uint8Array }>(alice, {
      id: "3", kind: "addMember", groupId: "c1", keyPackage: bobKp,
    });
    // Deliver the Welcome to bob (commit is n/a for the adder in a 2-party add).
    await call(bob, { id: "4", kind: "joinFromWelcome", welcome: add.welcome });

    const ct = await call<Uint8Array>(alice, { id: "5", kind: "encrypt", groupId: "c1", plaintext: enc("hi") });
    const pt = await call<Uint8Array>(bob, { id: "6", kind: "decrypt", groupId: "c1", message: ct });

    expect(dec(pt)).toBe("hi");
  });

  it("public/external-commit path: carol joins p1 by external commit, alice→carol message round-trips", async () => {
    const alice = createDispatcher("alice2@corp");
    const carol = createDispatcher("carol@corp");

    await call(alice, { id: "1", kind: "createGroup", groupId: "p1" });
    const gi = await call<Uint8Array>(alice, { id: "2", kind: "exportGroupInfo", groupId: "p1" });

    // Carol joins by external commit; the resulting commit must reach alice.
    const commit = await call<Uint8Array>(carol, { id: "3", kind: "joinByExternalCommit", groupInfo: gi });
    // Alice merges carol's external commit (orchestrator path: decrypt on a commit frame).
    await call(alice, { id: "4", kind: "decrypt", groupId: "p1", message: commit });

    const ct = await call<Uint8Array>(alice, { id: "5", kind: "encrypt", groupId: "p1", plaintext: enc("yo") });
    const pt = await call<Uint8Array>(carol, { id: "6", kind: "decrypt", groupId: "p1", message: ct });

    expect(dec(pt)).toBe("yo");
  });
});
