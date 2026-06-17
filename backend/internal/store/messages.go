package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Message is one stored (encrypted) message in a conversation log.
type Message struct {
	GroupID      string
	Seq          int64
	SenderDevice string
	ContentType  string
	Ciphertext   []byte
	ServerTS     time.Time
}

type MessageRepo struct{ pool *pgxpool.Pool }

func NewMessageRepo(pool *pgxpool.Pool) *MessageRepo { return &MessageRepo{pool: pool} }

// Append stores a message, assigning the next per-group seq in one transaction.
// Idempotent on (groupID, senderDevice, clientMsgID): a replay returns the existing
// seq with dup=true and writes nothing.
func (r *MessageRepo) Append(ctx context.Context, groupID, senderDevice, clientMsgID, contentType string, ciphertext []byte) (seq int64, dup bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx,
		`SELECT seq FROM messages WHERE group_id=$1 AND sender_device=$2 AND client_msg_id=$3`,
		groupID, senderDevice, clientMsgID).Scan(&seq)
	if err == nil {
		return seq, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, err
	}

	if err := tx.QueryRow(ctx,
		`INSERT INTO conversations (group_id, next_seq) VALUES ($1, 1)
		 ON CONFLICT (group_id) DO UPDATE SET next_seq = conversations.next_seq + 1
		 RETURNING next_seq`, groupID).Scan(&seq); err != nil {
		return 0, false, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO messages (group_id, seq, sender_device, client_msg_id, content_type, ciphertext)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		groupID, seq, senderDevice, clientMsgID, contentType, ciphertext); err != nil {
		return 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return seq, false, nil
}

// ListSince returns messages with seq > max(sinceSeq, joinSeq-1), ordered by seq, up to limit.
func (r *MessageRepo) ListSince(ctx context.Context, groupID string, sinceSeq, joinSeq int64, limit int) ([]Message, error) {
	floor := sinceSeq
	if joinSeq-1 > floor {
		floor = joinSeq - 1
	}
	rows, err := r.pool.Query(ctx,
		`SELECT group_id, seq, sender_device, content_type, ciphertext, server_ts
		 FROM messages WHERE group_id=$1 AND seq > $2 ORDER BY seq LIMIT $3`,
		groupID, floor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.GroupID, &m.Seq, &m.SenderDevice, &m.ContentType, &m.Ciphertext, &m.ServerTS); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
