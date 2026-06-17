package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CursorRepo struct{ pool *pgxpool.Pool }

func NewCursorRepo(pool *pgxpool.Pool) *CursorRepo { return &CursorRepo{pool: pool} }

// Advance moves the device's cursor for a group to uptoSeq, never backward.
func (r *CursorRepo) Advance(ctx context.Context, deviceID, groupID string, uptoSeq int64) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO device_cursors (device_id, group_id, acked_seq) VALUES ($1,$2,$3)
		 ON CONFLICT (device_id, group_id)
		 DO UPDATE SET acked_seq = GREATEST(device_cursors.acked_seq, EXCLUDED.acked_seq)`,
		deviceID, groupID, uptoSeq)
	return err
}

// Get returns the device's acked_seq for a group (0 if none).
func (r *CursorRepo) Get(ctx context.Context, deviceID, groupID string) (int64, error) {
	var seq int64
	err := r.pool.QueryRow(ctx,
		`SELECT acked_seq FROM device_cursors WHERE device_id=$1 AND group_id=$2`,
		deviceID, groupID).Scan(&seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return seq, nil
}
