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
});
