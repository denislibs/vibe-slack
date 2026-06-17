import type { MessageFrame } from "../shared/api/ds";
import { CONTENT_TYPE } from "../shared/api/ds";
import type { ConversationStore } from "../entities/conversation/store";
import type { ConnectionStore, ConnStatus } from "../entities/connection/store";

// Narrow ports so the orchestrator depends on behavior, not on Worker classes.
export interface ProtocolPort {
  connect(token: string): void;
  send(s: { clientMsgId: string; groupId: string; contentType: string; ciphertext: string }): void;
  ack(groupID: string, upToSeq: number): void;
  onMessage(h: (m: MessageFrame) => void): void;
  onStatus(h: (s: string) => void): void;
}
export interface CryptoPort {
  encrypt(groupID: string, plaintext: Uint8Array): Promise<Uint8Array>;
  decrypt(groupID: string, ciphertext: Uint8Array): Promise<Uint8Array>;
  joinFromWelcome(welcome: Uint8Array): Promise<void>;
}

export interface OrchestratorDeps {
  protocol: ProtocolPort;
  crypto: CryptoPort;
  conversation: ConversationStore;
  connection: ConnectionStore;
}

function b64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}
function bytesToB64(b: Uint8Array): string {
  let s = "";
  for (const x of b) s += String.fromCharCode(x);
  return btoa(s);
}

// The orchestration layer: all cross-thread business logic lives here, NOT in UI.
export function createOrchestrator(deps: OrchestratorDeps) {
  const { protocol, crypto, conversation, connection } = deps;

  protocol.onStatus((s) => connection.setStatus(s as ConnStatus));

  protocol.onMessage((m) => {
    void (async () => {
      try {
        const bytes = b64ToBytes(m.ciphertext);
        if (m.content_type === CONTENT_TYPE.welcome) {
          // A Welcome admits this device to the group; no store write.
          await crypto.joinFromWelcome(bytes);
        } else if (m.content_type === CONTENT_TYPE.commit) {
          // An MLS commit advances the group epoch. decrypt merges it and
          // returns empty bytes for handshake frames; nothing to store.
          await crypto.decrypt(m.group_id, bytes);
        } else {
          // Application message: decrypt and surface to the conversation store.
          const plaintext = await crypto.decrypt(m.group_id, bytes);
          if (plaintext.length > 0) {
            conversation.addMessage(m.group_id, {
              seq: m.seq, sender: m.sender_device, text: new TextDecoder().decode(plaintext),
            });
          }
        }
        protocol.ack(m.group_id, m.seq);
      } catch {
        // Undecryptable (missing epoch/keys): leave for a later sync; do not crash.
      }
    })();
  });

  return {
    connect(token: string) { protocol.connect(token); },
    async sendText(groupID: string, text: string) {
      const ct = await crypto.encrypt(groupID, new TextEncoder().encode(text));
      protocol.send({
        clientMsgId: randomId(), groupId: groupID,
        contentType: CONTENT_TYPE.app, ciphertext: bytesToB64(ct),
      });
    },
  };
}

function randomId(): string {
  const b = new Uint8Array(16);
  globalThis.crypto.getRandomValues(b); // Web Crypto (browser + jsdom)
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}
