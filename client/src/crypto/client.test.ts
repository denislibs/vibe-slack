import { describe, it, expect } from "vitest";
import { CryptoClient } from "./client";
import type { CryptoResponse } from "./protocol";

// Fake Worker that echoes deterministic responses.
class FakeWorker {
  onmessage: ((ev: MessageEvent) => void) | null = null;
  postMessage(req: { id: string; kind: string }) {
    const res: CryptoResponse =
      req.kind === "keyPackage"
        ? { id: req.id, ok: true, result: new Uint8Array([9, 9]) }
        : { id: req.id, ok: false, error: "unsupported" };
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

  it("rejects when the worker reports an error", async () => {
    const client = new CryptoClient(new FakeWorker() as unknown as Worker, "alice@corp");
    await expect(client.createGroup("team-1")).rejects.toThrow("unsupported");
  });
});
