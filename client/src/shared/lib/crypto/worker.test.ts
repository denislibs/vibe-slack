// @vitest-environment node
import { describe, it, expect, vi, beforeEach } from "vitest";

const persisted = new Map<string, Uint8Array>();
vi.mock("./persistence", () => ({
  loadCryptoState: vi.fn(async (name: string) => persisted.get(name) ?? null),
  saveCryptoState: vi.fn(async (name: string, b: Uint8Array) => {
    persisted.set(name, b.slice());
  }),
}));

import { handleRequest, dispatchTo, createDispatcher } from "./worker";
import type { CryptoRequest } from "./protocol";
import type { WasmEngine } from "./wasm-pkg/crypto_core.js";

describe("worker dispatch", () => {
  it("returns a key package for a fresh engine", async () => {
    const req: CryptoRequest = { id: "r1", kind: "keyPackage" };
    const res = await handleRequest("alice@corp", req);
    expect(res.ok).toBe(true);
    if (res.ok) expect((res.result as Uint8Array).length).toBeGreaterThan(0);
  });

  it("dispatches exportGroupInfo to engine.export_group_info", async () => {
    const calls: string[] = [];
    const engine = {
      export_group_info: (g: string) => {
        calls.push(g);
        return new Uint8Array([1, 2]);
      },
    } as unknown as WasmEngine;
    const res = await dispatchTo(engine, {
      id: "1",
      kind: "exportGroupInfo",
      groupId: "g",
    });
    expect(res.ok).toBe(true);
    expect(calls).toEqual(["g"]);
    if (res.ok) expect(Array.from(res.result as Uint8Array)).toEqual([1, 2]);
  });

  it("dispatches joinByExternalCommit forwarding req.groupInfo", async () => {
    const calls: Uint8Array[] = [];
    const engine = {
      join_by_external_commit: (gi: Uint8Array) => {
        calls.push(gi);
        return new Uint8Array([3, 4]);
      },
    } as unknown as WasmEngine;
    const groupInfo = new Uint8Array([9, 9, 9]);
    const res = await dispatchTo(engine, {
      id: "2",
      kind: "joinByExternalCommit",
      groupInfo,
    });
    expect(res.ok).toBe(true);
    expect(calls).toEqual([groupInfo]);
    if (res.ok) expect(Array.from(res.result as Uint8Array)).toEqual([3, 4]);
  });
});

describe("worker persistence", () => {
  beforeEach(() => persisted.clear());

  it("persists engine state and rehydrates a fresh dispatcher", async () => {
    const d1 = createDispatcher("alice@corp");
    const created = await d1.handleRequest({ id: "1", kind: "createGroup", groupId: "team-1" });
    expect(created.ok).toBe(true);
    expect(persisted.has("alice@corp")).toBe(true);

    // Simulate reload: a brand-new dispatcher must restore and encrypt to the
    // existing group instead of throwing "unknown group".
    const d2 = createDispatcher("alice@corp");
    const enc = await d2.handleRequest({ id: "2", kind: "encrypt", groupId: "team-1", plaintext: new Uint8Array([1, 2, 3]) });
    expect(enc.ok).toBe(true);
  });
});
