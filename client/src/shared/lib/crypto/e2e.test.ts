// @vitest-environment node
import { describe, it, expect } from "vitest";
import { createDispatcher } from "./worker";

describe("crypto e2e via worker dispatch", () => {
  it("alice -> bob message round-trips and bob decrypts", async () => {
    const alice = createDispatcher("alice@corp");
    const bob = createDispatcher("bob@corp");

    const bobKpRes = await bob.handleRequest({ id: "1", kind: "keyPackage" });
    expect(bobKpRes.ok).toBe(true);
    const bobKp = (bobKpRes as { result: Uint8Array }).result;

    const created = await alice.handleRequest({ id: "2", kind: "createGroup", groupId: "team-1" });
    expect(created.ok).toBe(true);

    const addRes = await alice.handleRequest({
      id: "3", kind: "addMember", groupId: "team-1", keyPackage: bobKp,
    });
    expect(addRes.ok).toBe(true);
    const add = (addRes as { result: { commit: Uint8Array; welcome: Uint8Array } }).result;

    const joined = await bob.handleRequest({
      id: "4", kind: "joinFromWelcome", welcome: add.welcome,
    });
    expect(joined.ok).toBe(true);

    const encRes = await alice.handleRequest({
      id: "5", kind: "encrypt", groupId: "team-1", plaintext: new TextEncoder().encode("hi bob"),
    });
    expect(encRes.ok).toBe(true);
    const ct = (encRes as { result: Uint8Array }).result;

    const decRes = await bob.handleRequest({
      id: "6", kind: "decrypt", groupId: "team-1", message: ct,
    });
    expect(decRes.ok).toBe(true);
    const pt = (decRes as { result: Uint8Array }).result;

    expect(new TextDecoder().decode(pt)).toBe("hi bob");
  });

  it("compliance member is added and a member can be removed", async () => {
    const alice = createDispatcher("alice@corp");
    const compliance = createDispatcher("compliance@corp");
    const bob = createDispatcher("bob@corp");

    const compKp = (await compliance.handleRequest({ id: "c1", kind: "keyPackage" }) as { result: Uint8Array }).result;
    const welcomeRes = await alice.handleRequest({ id: "c2", kind: "createGroupWithCompliance", groupId: "g", complianceKeyPackage: compKp });
    expect(welcomeRes.ok).toBe(true);
    const welcome = (welcomeRes as { result: Uint8Array }).result;
    expect((await compliance.handleRequest({ id: "c3", kind: "joinFromWelcome", welcome })).ok).toBe(true);

    // compliance decrypts real traffic
    const ct = (await alice.handleRequest({ id: "c4", kind: "encrypt", groupId: "g", plaintext: new TextEncoder().encode("audited") }) as { result: Uint8Array }).result;
    const pt = (await compliance.handleRequest({ id: "c5", kind: "decrypt", groupId: "g", message: ct }) as { result: Uint8Array }).result;
    expect(new TextDecoder().decode(pt)).toBe("audited");

    // add bob (leaf 2, since compliance is leaf 1), then remove him -> returns a commit
    const bobKp = (await bob.handleRequest({ id: "c6", kind: "keyPackage" }) as { result: Uint8Array }).result;
    expect((await alice.handleRequest({ id: "c7", kind: "addMember", groupId: "g", keyPackage: bobKp })).ok).toBe(true);
    const removeRes = await alice.handleRequest({ id: "c8", kind: "removeMember", groupId: "g", leafIndex: 2 });
    expect(removeRes.ok).toBe(true);
    expect((removeRes as { result: Uint8Array }).result.length).toBeGreaterThan(0);
  });
});
