import { ProtocolConnection, type WebSocketLike } from "./connection";

function realSocket(url: string, token: string): WebSocketLike {
  // Browsers can't set Authorization on the WS handshake, so the token goes as a
  // query param (DS must accept ?access_token=; reconciling with the bearer-header
  // gateway is a backend follow-up).
  const ws = new WebSocket(`${url}?access_token=${encodeURIComponent(token)}`);
  const like: WebSocketLike = {
    send: (d) => ws.send(d),
    close: () => ws.close(),
    onopen: null, onmessage: null, onclose: null,
  };
  ws.onopen = () => like.onopen?.();
  ws.onmessage = (e) => like.onmessage?.(typeof e.data === "string" ? e.data : "");
  ws.onclose = () => like.onclose?.();
  return like;
}

let conn: ProtocolConnection | null = null;

self.onmessage = (ev: MessageEvent) => {
  const d = ev.data as any;
  switch (d.cmd) {
    case "connect": {
      const url = (self as any).DS_WS_URL ?? "ws://localhost:8080/ws";
      conn = new ProtocolConnection(() => realSocket(url, d.token), d.token);
      conn.onMessage((m) => (self as unknown as Worker).postMessage({ event: "message", payload: m }));
      conn.onStatus((s) => (self as unknown as Worker).postMessage({ event: "status", payload: s }));
      conn.connect();
      break;
    }
    case "send": conn?.send({ clientMsgId: d.clientMsgId, groupId: d.groupId, contentType: d.contentType, ciphertext: d.ciphertext }); break;
    case "ack": conn?.ack(d.groupID, d.upToSeq); break;
  }
};
