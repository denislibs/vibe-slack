package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Member is a device in a conversation, with the seq at which it joined.
type Member struct {
	DeviceID string
	JoinSeq  int64
}

type RosterRepo struct{ pool *pgxpool.Pool }

func NewRosterRepo(pool *pgxpool.Pool) *RosterRepo { return &RosterRepo{pool: pool} }

// AddMember adds (or re-adds, updating join_seq) a device to a conversation.
func (r *RosterRepo) AddMember(ctx context.Context, groupID, deviceID string, joinSeq int64) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO conversation_members (group_id, device_id, join_seq) VALUES ($1,$2,$3)
		 ON CONFLICT (group_id, device_id) DO UPDATE SET join_seq = EXCLUDED.join_seq`,
		groupID, deviceID, joinSeq)
	return err
}

func (r *RosterRepo) RemoveMember(ctx context.Context, groupID, deviceID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM conversation_members WHERE group_id=$1 AND device_id=$2`, groupID, deviceID)
	return err
}

func (r *RosterRepo) Members(ctx context.Context, groupID string) ([]Member, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT device_id, join_seq FROM conversation_members WHERE group_id=$1`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.DeviceID, &m.JoinSeq); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *RosterRepo) IsMember(ctx context.Context, groupID, deviceID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT FROM conversation_members WHERE group_id=$1 AND device_id=$2)`,
		groupID, deviceID).Scan(&exists)
	return exists, err
}
