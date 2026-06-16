CREATE TABLE conversations (
    group_id   TEXT PRIMARY KEY,
    next_seq   BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE messages (
    group_id      TEXT NOT NULL,
    seq           BIGINT NOT NULL,
    sender_device UUID NOT NULL,
    client_msg_id TEXT NOT NULL,
    content_type  TEXT NOT NULL,
    ciphertext    BYTEA NOT NULL,
    server_ts     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, seq)
);
CREATE UNIQUE INDEX idx_messages_idem ON messages (group_id, sender_device, client_msg_id);

CREATE TABLE conversation_members (
    group_id   TEXT NOT NULL,
    device_id  UUID NOT NULL,
    join_seq   BIGINT NOT NULL,
    added_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, device_id)
);

CREATE TABLE device_cursors (
    device_id  UUID NOT NULL,
    group_id   TEXT NOT NULL,
    acked_seq  BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (device_id, group_id)
);
