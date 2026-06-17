// DS WebSocket frame contract (mirrors backend/internal/ws/frames.go). Ciphertext
// fields are base64 strings on the wire.
export type SendFrame = { type: "send"; client_msg_id: string; group_id: string; content_type: string; ciphertext: string };
export type AckFrame = { type: "ack"; group_id: string; up_to_seq: number };
export type SyncFrame = { type: "sync"; group_id: string; since_seq: number };
export type SentFrame = { type: "sent"; client_msg_id: string; group_id: string; seq: number; server_ts: number };
export type MessageFrame = { type: "message"; group_id: string; seq: number; sender_device: string; content_type: string; ciphertext: string; server_ts: number };
export type ErrorFrame = { type: "error"; code: string; message: string };

export type InboundFrame = SentFrame | MessageFrame | ErrorFrame;
