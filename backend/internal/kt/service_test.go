package kt

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/messenger/backend/internal/store"
	"github.com/transparency-dev/merkle/proof"
	"github.com/transparency-dev/merkle/rfc6962"
)

func TestServiceLookupReturnsVerifiableProof(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t) // defined in relay_test.go (same package)
	users := store.NewUserRepo(pool)
	devices := store.NewDeviceRepo(pool)
	ktRepo := store.NewKTRepo(pool)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	u, _ := users.Create(ctx, "svc@corp", []byte("rec"))
	devices.Enroll(ctx, u.ID, []byte("keyA"), "laptop")
	relay := NewRelay(pool, ktRepo, store.NewDeviceRepo(pool), NewSTHSigner(priv))
	relay.Tick(ctx)

	svc := NewService(ktRepo)

	res, err := svc.Lookup(ctx, u.ID)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if err := proof.VerifyInclusion(rfc6962.DefaultHasher,
		uint64(res.LeafIndex), uint64(res.STH.TreeSize), res.LeafHash, res.AuditPath, res.STH.RootHash); err != nil {
		t.Fatalf("returned proof must verify: %v", err)
	}
	if !VerifySTH(pub, res.STH.TreeSize, res.STH.RootHash, res.STH.Signature) {
		t.Fatal("returned STH signature must verify")
	}
	if _, err := svc.Lookup(ctx, "99999999-9999-9999-9999-999999999999"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLookupNeverReturnsLeafBeyondSTH(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	users := store.NewUserRepo(pool)
	devices := store.NewDeviceRepo(pool)
	ktRepo := store.NewKTRepo(pool)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	relay := NewRelay(pool, ktRepo, store.NewDeviceRepo(pool), NewSTHSigner(priv))
	svc := NewService(ktRepo)

	u, _ := users.Create(ctx, "snap@corp", []byte("rec"))
	devices.Enroll(ctx, u.ID, []byte("k1"), "a")
	relay.Tick(ctx) // STH now covers u's leaf

	// Append a leaf for a SECOND identity directly WITHOUT issuing a new STH,
	// simulating the window between leaf-commit and STH-issue.
	u2, _ := users.Create(ctx, "snap2@corp", []byte("rec"))
	canonical := CanonicalLeaf(u2.ID, 1, [][]byte{[]byte("k2")})
	ktRepo.AppendLeaf(ctx, u2.ID, 1, canonical, LeafHash(canonical))

	// u2's leaf exists but is beyond the current STH → Lookup must return ErrNotFound,
	// NOT a broken/out-of-range proof.
	if _, err := svc.Lookup(ctx, u2.ID); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound for leaf beyond STH, got %v", err)
	}
	// u (covered by the STH) still looks up fine with a verifiable proof.
	res, err := svc.Lookup(ctx, u.ID)
	if err != nil {
		t.Fatalf("lookup u: %v", err)
	}
	if res.STH.TreeSize != 1 || res.LeafIndex != 0 {
		t.Fatalf("unexpected: treeSize=%d leafIndex=%d", res.STH.TreeSize, res.LeafIndex)
	}
}
