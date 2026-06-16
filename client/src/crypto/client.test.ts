import { describe, it, expect } from "vitest";
import { CryptoClient } from "./client";
import type { CryptoResponse } from "./protocol";

// Fake Worker that echoes deterministic responses.
class FakeWorker {
  onmessage: ((ev: MessageEvent) => void) | null = null;
  postMessage(req: { id: string; kind: string }) {
    const ok = (result: unknown): CryptoResponse => ({ id: req.id, ok: true, result });
    let res: CryptoResponse;
    switch (req.kind) {
      case "keyPackage":
        res = ok(new Uint8Array([9, 9]));
        break;
      case "createGroupWithCompliance":
        res = ok(new Uint8Array([1, 2, 3]));
        break;
      case "removeMember":
        res = ok(new Uint8Array([4, 5, 6]));
        break;
      default:
        res = { id: req.id, ok: false, error: "unsupported" };
    }
    queueMicrotask(() => this.onmessage?.({ data: res } as MessageEvent));
  }
  terminate() {}
}

describe("CryptoClient", () => {
  it("resolves a request with the matching response by id", async () => {
    const client = new CryptoClient(new FakeWorker() as unknown as Worker, "alice@corp");
    const kp = await client.keyPackage();
    expect(Array.from(kp)).toEqual([9, 9]);
  });

  it("createGroupWithCompliance forwards the welcome from the worker", async () => {
    const client = new CryptoClient(new FakeWorker() as unknown as Worker, "alice@corp");
    const welcome = await client.createGroupWithCompliance("g", new Uint8Array([1]));
    expect(Array.from(welcome)).toEqual([1, 2, 3]);
  });

  it("removeMember forwards the commit from the worker", async () => {
    const client = new CryptoClient(new FakeWorker() as unknown as Worker, "alice@corp");
    const commit = await client.removeMember("g", 2);
    expect(Array.from(commit)).toEqual([4, 5, 6]);
  });

  it("rejects when the worker reports an error", async () => {
    const client = new CryptoClient(new FakeWorker() as unknown as Worker, "alice@corp");
    await expect(client.createGroup("team-1")).rejects.toThrow("unsupported");
  });
});
