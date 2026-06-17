CREATE TABLE kt_leaves (
    leaf_index BIGINT PRIMARY KEY,
    identity   UUID NOT NULL,
    version    BIGINT NOT NULL,
    device_set BYTEA NOT NULL,
    leaf_hash  BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_kt_leaves_identity ON kt_leaves (identity, version);

CREATE TABLE kt_sths (
    tree_size  BIGINT PRIMARY KEY,
    root_hash  BYTEA NOT NULL,
    signature  BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
