package store

import (
	"context"
	"errors"
	"testing"
)

func TestKeyPackageUploadConsumeExhaustLastResort(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	users := NewUserRepo(pool)
	devices := NewDeviceRepo(pool)
	kp := NewKeyPackageRepo(pool)

	u, _ := users.Create(ctx, "erin@corp", "erin", []byte("rec"))
	dev, _ := devices.Enroll(ctx, u.ID, []byte("pub"), "phone")

	if err := kp.Upload(ctx, dev.ID, [][]byte{[]byte("otk-1"), []byte("otk-2")}, false); err != nil {
		t.Fatalf("upload one-time: %v", err)
	}
	if err := kp.Upload(ctx, dev.ID, [][]byte{[]byte("last-resort")}, true); err != nil {
		t.Fatalf("upload last-resort: %v", err)
	}

	if n, _ := kp.CountAvailable(ctx, dev.ID); n != 2 {
		t.Fatalf("expected 2 available one-time, got %d", n)
	}

	first, lr1, _ := kp.Consume(ctx, dev.ID)
	second, lr2, _ := kp.Consume(ctx, dev.ID)
	if lr1 || lr2 {
		t.Fatal("one-time consumes should not be last-resort while pool is non-empty")
	}
	if string(first) == string(second) {
		t.Fatal("must not hand out the same one-time package twice")
	}

	if n, _ := kp.CountAvailable(ctx, dev.ID); n != 0 {
		t.Fatalf("expected 0 available after consuming both, got %d", n)
	}

	got, isLast, err := kp.Consume(ctx, dev.ID)
	if err != nil {
		t.Fatalf("consume last-resort: %v", err)
	}
	if !isLast || string(got) != "last-resort" {
		t.Fatalf("expected last-resort package, got %q last=%v", got, isLast)
	}
}

func TestConsumeRevokedDeviceReturnsNothing(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	users := NewUserRepo(pool)
	devices := NewDeviceRepo(pool)
	kp := NewKeyPackageRepo(pool)
	u, _ := users.Create(ctx, "gail@corp", "gail", []byte("r"))
	dev, _ := devices.Enroll(ctx, u.ID, []byte("pub"), "phone")
	kp.Upload(ctx, dev.ID, [][]byte{[]byte("otk-1")}, false)
	kp.Upload(ctx, dev.ID, [][]byte{[]byte("lr")}, true)

	if err := devices.Revoke(ctx, u.ID, dev.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if n, _ := kp.CountAvailable(ctx, dev.ID); n != 0 {
		t.Fatalf("revoked device must report 0 available, got %d", n)
	}
	if _, _, err := kp.Consume(ctx, dev.ID); !errors.Is(err, ErrNoKeyPackage) {
		t.Fatalf("revoked device must serve no key package, got err %v", err)
	}
}
