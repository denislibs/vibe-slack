import type { MessageFrame } from "../../api/ds";

export interface WebSocketLike {
  send(data: string): void;
  close(): void;
  onopen: (() => void) | null;
  onmessage: ((data: string) => void) | null;
  onclose: (() => void) | null;
}

export type OutgoingSend = { clientMsgId: string; groupId: string; contentType: string; ciphertext: string };

// ProtocolConnection is the pure transport logic: queue-until-open, sync-on-connect,
// inbound frame demux. Driven by an injected WebSocketLike so it tests without a real
// socket. The worker shell wires a real WebSocket to it.
export class ProtocolConnection {
  private sock: WebSocketLike | null = null;
  private open = false;
  private outQueue: string[] = [];
  private cursors = new Map<string, number>();
  private msgHandler: ((m: MessageFrame) => void) | null = null;
  private statusHandler: ((s: "online" | "offline") => void) | null = null;

  constructor(private mkSocket: () => WebSocketLike, private token: string) {}

  onMessage(h: (m: MessageFrame) => void) { this.msgHandler = h; }
  onStatus(h: (s: "online" | "offline") => void) { this.statusHandler = h; }
  setCursor(groupID: string, seq: number) { this.cursors.set(groupID, seq); }

  track(groupID: string, sinceSeq: number) {
    this.cursors.set(groupID, sinceSeq);
    if (this.open) this.transmit({ type: "sync", group_id: groupID, since_seq: sinceSeq });
  }

  connect() {
    const sock = this.mkSocket();
    this.sock = sock;
    sock.onopen = () => {
      this.open = true;
      this.statusHandler?.("online");
      for (const [groupID, since] of this.cursors) {
        this.transmit({ type: "sync", group_id: groupID, since_seq: since });
      }
      const q = this.outQueue;
      this.outQueue = [];
      for (const raw of q) sock.send(raw);
    };
    sock.onmessage = (data) => this.handleInbound(data);
    sock.onclose = () => {
      this.open = false;
      this.statusHandler?.("offline");
    };
  }

  send(s: OutgoingSend) {
    const frame = JSON.stringify({
      type: "send", client_msg_id: s.clientMsgId, group_id: s.groupId,
      content_type: s.contentType, ciphertext: s.ciphertext,
    });
    if (this.open && this.sock) this.sock.send(frame);
    else this.outQueue.push(frame);
  }

  ack(groupID: string, upToSeq: number) {
    this.cursors.set(groupID, upToSeq);
    this.transmit({ type: "ack", group_id: groupID, up_to_seq: upToSeq });
  }

  private transmit(frame: object) {
    const raw = JSON.stringify(frame);
    if (this.open && this.sock) this.sock.send(raw);
    else this.outQueue.push(raw);
  }

  private handleInbound(data: string) {
    let frame: any;
    try { frame = JSON.parse(data); } catch { return; }
    if (frame.type === "message" && this.msgHandler) {
      this.msgHandler(frame as MessageFrame);
    }
  }
}
