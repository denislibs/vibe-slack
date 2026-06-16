package store

import (
	"context"
	"testing"
)

func TestDeviceEnrollWritesOutboxInSameTx(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	users := NewUserRepo(pool)
	devices := NewDeviceRepo(pool)

	u, _ := users.Create(ctx, "dave@corp", []byte("rec"))

	dev, err := devices.Enroll(ctx, u.ID, []byte("signing-pub-key"), "laptop")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if dev.ID == "" || dev.Status != "active" {
		t.Fatalf("unexpected device: %+v", dev)
	}

	var outboxCount int
	pool.QueryRow(ctx,
		`SELECT count(*) FROM kt_outbox WHERE user_id=$1 AND event_type='device_added'`, u.ID).
		Scan(&outboxCount)
	if outboxCount != 1 {
		t.Fatalf("expected 1 kt_outbox row, got %d", outboxCount)
	}

	list, _ := devices.ListByUser(ctx, u.ID)
	if len(list) != 1 {
		t.Fatalf("expected 1 device, got %d", len(list))
	}

	if err := devices.Revoke(ctx, dev.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	pool.QueryRow(ctx,
		`SELECT count(*) FROM kt_outbox WHERE user_id=$1 AND event_type='device_revoked'`, u.ID).
		Scan(&outboxCount)
	if outboxCount != 1 {
		t.Fatalf("expected 1 device_revoked outbox row, got %d", outboxCount)
	}
}
