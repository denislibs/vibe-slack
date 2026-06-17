import { describe, it, expect, vi } from "vitest";
import { createOrchestrator } from "./orchestrator";
import { createConversationStore } from "../entities/conversation/store";
import { createConnectionStore } from "../entities/connection/store";
import type { MessageFrame } from "../shared/api/ds";

function fakes() {
  let onMsg: (m: MessageFrame) => void = () => {};
  let onStatus: (s: string) => void = () => {};
  const acked: Array<{ g: string; s: number }> = [];
  const sent: any[] = [];
  const protocol = {
    connect: vi.fn(),
    send: (s: any) => sent.push(s),
    ack: (g: string, s: number) => acked.push({ g, s }),
    onMessage: (h: (m: MessageFrame) => void) => (onMsg = h),
    onStatus: (h: (s: string) => void) => (onStatus = h),
  };
  const crypto = {
    decrypt: vi.fn(async (_group: string, _ct: Uint8Array) => new TextEncoder().encode("hello")),
    encrypt: vi.fn(async (_group: string, _pt: Uint8Array) => new Uint8Array([1, 2, 3])),
    joinFromWelcome: vi.fn(async (_welcome: Uint8Array) => {}),
  };
  return { protocol, crypto, sent, acked, fire: (m: MessageFrame) => onMsg(m), fireStatus: (s: string) => onStatus(s) };
}

describe("orchestrator", () => {
  it("inbound message → decrypt → store → ack", async () => {
    const f = fakes();
    const conv = createConversationStore();
    const conn = createConnectionStore();
    createOrchestrator({ protocol: f.protocol as any, crypto: f.crypto as any, conversation: conv, connection: conn });

    f.fire({ type: "message", group_id: "g1", seq: 4, sender_device: "bob", content_type: "application", ciphertext: btoa("ct"), server_ts: 0 });
    await Promise.resolve();
    await Promise.resolve();

    expect(conv.messages("g1").map((m) => m.text)).toEqual(["hello"]);
    expect(f.acked).toContainEqual({ g: "g1", s: 4 });
  });

  it("inbound mls-commit frame → crypto.decrypt (merge) → ack, no store write", async () => {
    const f = fakes();
    const conv = createConversationStore();
    createOrchestrator({ protocol: f.protocol as any, crypto: f.crypto as any, conversation: conv, connection: createConnectionStore() });

    f.fire({ type: "message", group_id: "g1", seq: 7, sender_device: "carol", content_type: "mls-commit", ciphertext: btoa("commitbytes"), server_ts: 0 });
    await Promise.resolve();
    await Promise.resolve();

    expect(f.crypto.decrypt).toHaveBeenCalledWith("g1", expect.any(Uint8Array));
    expect(conv.messages("g1")).toEqual([]);
    expect(f.acked).toContainEqual({ g: "g1", s: 7 });
  });

  it("inbound mls-welcome frame → crypto.joinFromWelcome → ack, no store write, no decrypt", async () => {
    const f = fakes();
    const conv = createConversationStore();
    createOrchestrator({ protocol: f.protocol as any, crypto: f.crypto as any, conversation: conv, connection: createConnectionStore() });

    f.fire({ type: "message", group_id: "g2", seq: 3, sender_device: "alice", content_type: "mls-welcome", ciphertext: btoa("welcomebytes"), server_ts: 0 });
    await Promise.resolve();
    await Promise.resolve();

    expect(f.crypto.joinFromWelcome).toHaveBeenCalledWith(expect.any(Uint8Array));
    expect(f.crypto.decrypt).not.toHaveBeenCalled();
    expect(conv.messages("g2")).toEqual([]);
    expect(f.acked).toContainEqual({ g: "g2", s: 3 });
  });

  it("status events update the connection store", () => {
    const f = fakes();
    const conn = createConnectionStore();
    createOrchestrator({ protocol: f.protocol as any, crypto: f.crypto as any, conversation: createConversationStore(), connection: conn });
    f.fireStatus("online");
    expect(conn.status()).toBe("online");
  });

  it("sendText → encrypt → protocol.send", async () => {
    const f = fakes();
    const orch = createOrchestrator({ protocol: f.protocol as any, crypto: f.crypto as any, conversation: createConversationStore(), connection: createConnectionStore() });
    await orch.sendText("g1", "hi there");
    expect(f.crypto.encrypt).toHaveBeenCalled();
    expect(f.sent.length).toBe(1);
    expect(f.sent[0].groupId).toBe("g1");
  });
});
