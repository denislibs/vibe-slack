import type { Postable } from "../rpc/workerRpc";
import type { MessageFrame } from "../../api/ds";

// Main-thread proxy over the protocol worker. Commands {cmd,...} out; events
// {event, payload} in.
export class ProtocolClient {
  private msgHandler: ((m: MessageFrame) => void) | null = null;
  private statusHandler: ((s: string) => void) | null = null;

  constructor(private worker: Postable) {
    this.worker.onmessage = (ev: MessageEvent) => {
      const d = ev.data as { event?: string; payload?: unknown };
      if (d.event === "message") this.msgHandler?.(d.payload as MessageFrame);
      else if (d.event === "status") this.statusHandler?.(d.payload as string);
    };
  }

  connect(token: string) { this.worker.postMessage({ cmd: "connect", token }); }
  send(s: { clientMsgId: string; groupId: string; contentType: string; ciphertext: string }) {
    this.worker.postMessage({ cmd: "send", ...s });
  }
  ack(groupID: string, upToSeq: number) { this.worker.postMessage({ cmd: "ack", groupID, upToSeq }); }
  onMessage(h: (m: MessageFrame) => void) { this.msgHandler = h; }
  onStatus(h: (s: string) => void) { this.statusHandler = h; }
}
