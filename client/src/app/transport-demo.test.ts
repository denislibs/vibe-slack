// @vitest-environment node
import { describe, it, expect } from "vitest";
import { createOrchestrator, type ProtocolPort } from "./orchestrator";
import { createConversationStore } from "../entities/conversation/store";
import { createConnectionStore } from "../entities/connection/store";
import { createDispatcher } from "../shared/lib/crypto/worker";
import type { MessageFrame } from "../shared/api/ds";

function mockProtocol() {
  let onMsg: (m: MessageFrame) => void = () => {};
  const acks: number[] = [];
  const port: ProtocolPort = {
    connect() {},
    send() {},
    ack: (_g, s) => acks.push(s),
    onMessage: (h) => (onMsg = h),
    onStatus() {},
  };
  return { port, acks, fire: (m: MessageFrame) => onMsg(m) };
}

function cryptoPort(name: string) {
  const d = createDispatcher(name);
  return {
    dispatcher: d,
    port: {
      async encrypt(group: string, pt: Uint8Array): Promise<Uint8Array> {
        const r = await d.handleRequest({ id: "e", kind: "encrypt", groupId: group, plaintext: pt });
        return (r as any).result as Uint8Array;
      },
      async decrypt(group: string, ct: Uint8Array): Promise<Uint8Array> {
        const r = await d.handleRequest({ id: "d", kind: "decrypt", groupId: group, message: ct });
        return (r as any).result as Uint8Array;
      },
    },
  };
}

describe("transport demo (real MLS through the orchestrator)", () => {
  it("DS message frame → orchestrator → real decrypt → store, then ack", async () => {
    const alice = cryptoPort("alice@corp");
    const bob = cryptoPort("bob@corp");

    const bobKp = (await bob.dispatcher.handleRequest({ id: "1", kind: "keyPackage" }) as { result: Uint8Array }).result;
    await alice.dispatcher.handleRequest({ id: "2", kind: "createGroup", groupId: "g1" });
    const add = (await alice.dispatcher.handleRequest({ id: "3", kind: "addMember", groupId: "g1", keyPackage: bobKp }) as { result: { commit: Uint8Array; welcome: Uint8Array } }).result;
    await bob.dispatcher.handleRequest({ id: "4", kind: "joinFromWelcome", welcome: add.welcome });

    const ct = (await alice.dispatcher.handleRequest({ id: "5", kind: "encrypt", groupId: "g1", plaintext: new TextEncoder().encode("hi bob") }) as { result: Uint8Array }).result;

    const proto = mockProtocol();
    const conv = createConversationStore();
    createOrchestrator({ protocol: proto.port, crypto: bob.port, conversation: conv, connection: createConnectionStore() });

    const toB64 = (b: Uint8Array) => {
      let s = "";
      for (const x of b) s += String.fromCharCode(x);
      return btoa(s);
    };
    proto.fire({ type: "message", group_id: "g1", seq: 1, sender_device: "alice", content_type: "application", ciphertext: toB64(ct), server_ts: 0 });

    await new Promise((r) => setTimeout(r, 50));

    expect(conv.messages("g1").map((m) => m.text)).toEqual(["hi bob"]);
    expect(proto.acks).toContain(1);
  });
});
