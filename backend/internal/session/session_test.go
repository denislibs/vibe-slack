package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) *goredis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
}

func TestSessionIssueValidateRevoke(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(newTestRedis(t), time.Hour)

	token, err := mgr.Issue(ctx, "user-1", "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if len(token) < 32 {
		t.Fatalf("token too short: %d", len(token))
	}

	sess, err := mgr.Validate(ctx, token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if sess.UserID != "user-1" {
		t.Fatalf("got user %q", sess.UserID)
	}

	if err := mgr.Revoke(ctx, token); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := mgr.Validate(ctx, token); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expected ErrInvalidSession after revoke, got %v", err)
	}
}

func TestRevokeAll(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(newTestRedis(t), time.Hour)
	t1, _ := mgr.Issue(ctx, "user-1", "dev-a")
	t2, _ := mgr.Issue(ctx, "user-1", "dev-b")

	if err := mgr.RevokeAll(ctx, "user-1"); err != nil {
		t.Fatalf("revoke all: %v", err)
	}
	for _, tok := range []string{t1, t2} {
		if _, err := mgr.Validate(ctx, tok); !errors.Is(err, ErrInvalidSession) {
			t.Fatalf("expected all sessions invalid, %q still valid", tok)
		}
	}
}

func TestBindDevice(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(newTestRedis(t), time.Hour)
	token, _ := mgr.Issue(ctx, "user-1", "")
	if err := mgr.BindDevice(ctx, token, "dev-x"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	sess, _ := mgr.Validate(ctx, token)
	if sess.DeviceID != "dev-x" {
		t.Fatalf("expected device bound, got %q", sess.DeviceID)
	}
}
