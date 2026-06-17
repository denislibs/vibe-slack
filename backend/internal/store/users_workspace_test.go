package store

import (
	"context"
	"errors"
	"testing"
)

func TestUserCreateWithUsernameAndLookup(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	users := NewUserRepo(pool)

	u, err := users.Create(ctx, "alice@corp", "alice", []byte("rec"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.Username != "alice" {
		t.Fatalf("username not set: %+v", u)
	}
	if _, err := users.Create(ctx, "other@corp", "alice", []byte("rec")); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
	if _, err := users.Create(ctx, "alice@corp", "alice2", []byte("rec")); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	byEmail, err := users.FindByEmailOrUsername(ctx, "alice@corp")
	if err != nil || byEmail.ID != u.ID {
		t.Fatalf("find by email: %v %+v", err, byEmail)
	}
	byName, err := users.FindByEmailOrUsername(ctx, "alice")
	if err != nil || byName.ID != u.ID {
		t.Fatalf("find by username: %v %+v", err, byName)
	}
	if _, err := users.FindByEmailOrUsername(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
