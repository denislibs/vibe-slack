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
