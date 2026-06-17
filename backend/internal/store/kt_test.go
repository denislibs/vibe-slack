package store

import (
	"context"
	"testing"
)

func TestKTRepoAppendAndQuery(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := NewKTRepo(pool)
	id := "11111111-1111-1111-1111-111111111111"

	if v, _ := repo.MaxVersion(ctx, id); v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}

	idx0, err := repo.AppendLeaf(ctx, id, 1, []byte("set-v1"), []byte("hash0"))
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	idx1, _ := repo.AppendLeaf(ctx, id, 2, []byte("set-v2"), []byte("hash1"))
	if idx0 != 0 || idx1 != 1 {
		t.Fatalf("leaf_index must be monotonic from 0, got %d,%d", idx0, idx1)
	}
	if v, _ := repo.MaxVersion(ctx, id); v != 2 {
		t.Fatalf("expected max version 2, got %d", v)
	}

	hashes, _ := repo.LeafHashes(ctx, 2)
	if len(hashes) != 2 || string(hashes[0]) != "hash0" || string(hashes[1]) != "hash1" {
		t.Fatalf("leaf hashes wrong: %v", hashes)
	}

	leaf, err := repo.LatestLeafForIdentity(ctx, id)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if leaf.LeafIndex != 1 || leaf.Version != 2 || string(leaf.DeviceSet) != "set-v2" {
		t.Fatalf("latest leaf wrong: %+v", leaf)
	}
	if _, err := repo.LatestLeafForIdentity(ctx, "22222222-2222-2222-2222-222222222222"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for unknown identity, got %v", err)
	}

	if err := repo.PutSTH(ctx, 2, []byte("root"), []byte("sig")); err != nil {
		t.Fatalf("put sth: %v", err)
	}
	sth, err := repo.LatestSTH(ctx)
	if err != nil || sth.TreeSize != 2 || string(sth.RootHash) != "root" {
		t.Fatalf("latest sth wrong: %+v err=%v", sth, err)
	}
}
