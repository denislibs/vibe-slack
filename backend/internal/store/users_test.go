package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/messenger/backend/internal/platform/postgres"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// newTestPool starts a throwaway Postgres, connects, and migrates.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("as"), tcpostgres.WithUsername("as"), tcpostgres.WithPassword("as"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, _ := ctr.ConnectionString(ctx, "sslmode=disable")
	pool, err := postgres.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func TestUserRepoCreateAndGet(t *testing.T) {
	pool := newTestPool(t)
	repo := NewUserRepo(pool)
	ctx := context.Background()

	u, err := repo.Create(ctx, "alice@corp", []byte("opaque-record"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.ID == "" {
		t.Fatal("expected generated id")
	}

	got, err := repo.GetByEmail(ctx, "alice@corp")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != u.ID || string(got.OpaqueRecord) != "opaque-record" {
		t.Fatal("round-trip mismatch")
	}

	if _, err := repo.GetByEmail(ctx, "nobody@corp"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if _, err := repo.Create(ctx, "alice@corp", []byte("x")); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate email, got %v", err)
	}
}
