import { describe, it, expect } from "vitest";
import { createWorkerRpc, type Postable } from "./workerRpc";

class FakeWorker implements Postable {
  onmessage: ((ev: MessageEvent) => void) | null = null;
  postMessage(msg: any) {
    const res = msg.kind === "ping"
      ? { id: msg.id, ok: true, result: "pong" }
      : { id: msg.id, ok: false, error: "unsupported" };
    queueMicrotask(() => this.onmessage?.({ data: res } as MessageEvent));
  }
}

describe("workerRpc", () => {
  it("resolves a call with the matching response", async () => {
    const rpc = createWorkerRpc(new FakeWorker());
    await expect(rpc.call("ping", {})).resolves.toBe("pong");
  });
  it("rejects on error response", async () => {
    const rpc = createWorkerRpc(new FakeWorker());
    await expect(rpc.call("nope", {})).rejects.toThrow("unsupported");
  });
});
