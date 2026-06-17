package kt

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/messenger/backend/internal/platform/postgres"
	"github.com/messenger/backend/internal/store"
	"github.com/transparency-dev/merkle/proof"
	"github.com/transparency-dev/merkle/rfc6962"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("as"), tcpostgres.WithUsername("as"), tcpostgres.WithPassword("as"),
		tcpostgres.BasicWaitStrategies())
	if err != nil {
		t.Fatalf("pg: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, _ := ctr.ConnectionString(ctx, "sslmode=disable")
	pool, _ := postgres.Connect(ctx, dsn)
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func TestRelayBuildsLeavesAndSTH(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	users := store.NewUserRepo(pool)
	devices := store.NewDeviceRepo(pool)
	kt := store.NewKTRepo(pool)

	u, _ := users.Create(ctx, "rl@corp", []byte("rec"))
	d1, _ := devices.Enroll(ctx, u.ID, []byte("keyA"), "laptop")
	devices.Enroll(ctx, u.ID, []byte("keyB"), "phone")

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	relay := NewRelay(pool, kt, store.NewDeviceRepo(pool), NewSTHSigner(priv))

	if err := relay.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	leaf, err := kt.LatestLeafForIdentity(ctx, u.ID)
	if err != nil {
		t.Fatalf("latest leaf: %v", err)
	}
	if leaf.Version != 2 {
		t.Fatalf("expected version 2 after two events, got %d", leaf.Version)
	}

	sth, err := kt.LatestSTH(ctx)
	if err != nil {
		t.Fatalf("sth: %v", err)
	}
	hashes, _ := kt.LeafHashes(ctx, sth.TreeSize)
	pf, _ := InclusionProof(hashes, uint64(leaf.LeafIndex))
	if err := proof.VerifyInclusion(rfc6962.DefaultHasher, uint64(leaf.LeafIndex), uint64(sth.TreeSize), leaf.LeafHash, pf, sth.RootHash); err != nil {
		t.Fatalf("inclusion against STH failed: %v", err)
	}

	// Idempotency: a second tick with no new events appends nothing.
	if err := relay.Tick(ctx); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	sth2, _ := kt.LatestSTH(ctx)
	if sth2.TreeSize != sth.TreeSize {
		t.Fatalf("idempotent tick must not grow the tree: %d -> %d", sth.TreeSize, sth2.TreeSize)
	}

	// Revoking d1 produces a new version whose device set no longer contains keyA.
	devices.Revoke(ctx, u.ID, d1.ID)
	relay.Tick(ctx)
	leaf3, _ := kt.LatestLeafForIdentity(ctx, u.ID)
	if leaf3.Version != 3 {
		t.Fatalf("expected version 3 after revoke, got %d", leaf3.Version)
	}
}
