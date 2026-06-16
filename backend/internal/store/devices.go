package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DeviceRepo struct{ pool *pgxpool.Pool }

func NewDeviceRepo(pool *pgxpool.Pool) *DeviceRepo { return &DeviceRepo{pool: pool} }

// Enroll inserts a device and, in the SAME transaction, writes a kt_outbox event.
func (r *DeviceRepo) Enroll(ctx context.Context, userID string, signingPubKey []byte, label string) (*Device, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var id string
	if err := tx.QueryRow(ctx,
		`INSERT INTO devices (user_id, signing_public_key, label) VALUES ($1,$2,$3) RETURNING id`,
		userID, signingPubKey, label).Scan(&id); err != nil {
		return nil, err
	}
	if err := emitKTEvent(ctx, tx, userID, "device_added", map[string]string{"device_id": id}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &Device{ID: id, UserID: userID, Label: label, Status: "active"}, nil
}

func (r *DeviceRepo) ListByUser(ctx context.Context, userID string) ([]Device, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, label, status FROM devices WHERE user_id=$1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.UserID, &d.Label, &d.Status); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Revoke marks a device revoked and emits a kt_outbox event in the same tx.
// It is scoped to userID so a caller can only revoke their own devices.
func (r *DeviceRepo) Revoke(ctx context.Context, userID, deviceID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var uid string
	err = tx.QueryRow(ctx,
		`UPDATE devices SET status='revoked', revoked_at=now() WHERE id=$1 AND user_id=$2 RETURNING user_id`,
		deviceID, userID).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := emitKTEvent(ctx, tx, uid, "device_revoked", map[string]string{"device_id": deviceID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func emitKTEvent(ctx context.Context, tx pgx.Tx, userID, eventType string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO kt_outbox (user_id, event_type, payload) VALUES ($1,$2,$3)`,
		userID, eventType, data)
	return err
}
