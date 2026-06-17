import { CryptoClient } from "../shared/lib/crypto/client";
import { ProtocolClient } from "../shared/lib/transport/protocolClient";
import { createConversationStore } from "../entities/conversation/store";
import { createConnectionStore } from "../entities/connection/store";
import { createOrchestrator, type CryptoPort } from "./orchestrator";

export function bootstrap(deviceName: string) {
  const cryptoWorker = new Worker(new URL("../shared/lib/crypto/worker.ts", import.meta.url), { type: "module" });
  const protocolWorker = new Worker(new URL("../shared/lib/transport/protocol.worker.ts", import.meta.url), { type: "module" });

  const cryptoClient = new CryptoClient(cryptoWorker, deviceName);
  const protocol = new ProtocolClient(protocolWorker);
  const conversation = createConversationStore();
  const connection = createConnectionStore();

  // CryptoClient → CryptoPort. CryptoClient's encrypt/decrypt already match the
  // CryptoPort signature; this adapter bridges them without editing CryptoClient.
  const crypto: CryptoPort = {
    encrypt: (g, pt) => cryptoClient.encrypt(g, pt),
    decrypt: (g, msg) => cryptoClient.decrypt(g, msg),
  };

  const orchestrator = createOrchestrator({ protocol, crypto, conversation, connection });
  return { orchestrator, conversation, connection, protocol };
}
