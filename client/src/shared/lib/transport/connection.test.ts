import { describe, it, expect } from "vitest";
import { ProtocolConnection, type WebSocketLike } from "./connection";

class MockSocket implements WebSocketLike {
  onopen: (() => void) | null = null;
  onmessage: ((data: string) => void) | null = null;
  onclose: (() => void) | null = null;
  sent: string[] = [];
  send(data: string) { this.sent.push(data); }
  close() { this.onclose?.(); }
  open() { this.onopen?.(); }
  receive(obj: unknown) { this.onmessage?.(JSON.stringify(obj)); }
}

function setup() {
  const sock = new MockSocket();
  const messages: any[] = [];
  const conn = new ProtocolConnection(() => sock, "token-abc");
  conn.onMessage((m) => messages.push(m));
  return { sock, conn, messages };
}

describe("ProtocolConnection", () => {
  it("queues sends until open, then flushes", () => {
    const { sock, conn } = setup();
    conn.connect();
    conn.send({ clientMsgId: "c1", groupId: "g1", contentType: "application", ciphertext: "AQID" });
    expect(sock.sent.length).toBe(0);
    sock.open();
    expect(sock.sent.length).toBe(1);
    expect(JSON.parse(sock.sent[0]).type).toBe("send");
  });

  it("emits inbound message frames", () => {
    const { sock, conn, messages } = setup();
    conn.connect();
    sock.open();
    sock.receive({ type: "message", group_id: "g1", seq: 5, sender_device: "d", content_type: "application", ciphertext: "AQID" });
    expect(messages.length).toBe(1);
    expect(messages[0].seq).toBe(5);
  });

  it("on reconnect, re-sends a sync for the known cursor", () => {
    const { sock, conn } = setup();
    conn.setCursor("g1", 7);
    conn.connect();
    sock.open();
    const syncs = sock.sent.map((s) => JSON.parse(s)).filter((f) => f.type === "sync");
    expect(syncs.some((f) => f.group_id === "g1" && f.since_seq === 7)).toBe(true);
  });

  it("track() before connect seeds a sync sent on open", () => {
    const { sock, conn } = setup();
    conn.track("g1", 0);
    conn.connect();
    sock.open();
    const syncs = sock.sent.map((s) => JSON.parse(s)).filter((f) => f.type === "sync");
    expect(syncs.some((f) => f.group_id === "g1" && f.since_seq === 0)).toBe(true);
  });

  it("track() after open syncs immediately", () => {
    const { sock, conn } = setup();
    conn.connect();
    sock.open();
    sock.sent.length = 0;
    conn.track("g2", 5);
    const f = sock.sent.map((s) => JSON.parse(s)).find((x) => x.type === "sync");
    expect(f).toMatchObject({ type: "sync", group_id: "g2", since_seq: 5 });
  });
});
