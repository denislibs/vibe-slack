CREATE TABLE conversation_meta (
    group_id     TEXT PRIMARY KEY REFERENCES conversations(group_id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    type         TEXT NOT NULL CHECK (type IN ('dm','channel')),
    visibility   TEXT NOT NULL CHECK (visibility IN ('public','private')),
    name         TEXT NOT NULL DEFAULT '',
    created_by   UUID NOT NULL REFERENCES users(id),
    dm_key       TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX conversation_meta_ws_idx ON conversation_meta(workspace_id);
CREATE UNIQUE INDEX conversation_meta_dm_uniq ON conversation_meta(workspace_id, dm_key) WHERE dm_key IS NOT NULL;

CREATE TABLE conversation_user_members (
    group_id  TEXT NOT NULL REFERENCES conversation_meta(group_id) ON DELETE CASCADE,
    user_id   UUID NOT NULL REFERENCES users(id),
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX conversation_user_members_user_idx ON conversation_user_members(user_id);
