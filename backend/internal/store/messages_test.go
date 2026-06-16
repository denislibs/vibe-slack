package store

import (
	"context"
	"testing"
)

func TestMessageAppendAssignsMonotonicSeqAndIsIdempotent(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := NewMessageRepo(pool)
	dev := "11111111-1111-1111-1111-111111111111"

	s1, dup1, err := repo.Append(ctx, "g1", dev, "cm-1", "application", []byte("ct-1"))
	if err != nil || dup1 {
		t.Fatalf("append 1: seq=%d dup=%v err=%v", s1, dup1, err)
	}
	s2, _, _ := repo.Append(ctx, "g1", dev, "cm-2", "application", []byte("ct-2"))
	if s1 != 1 || s2 != 2 {
		t.Fatalf("expected monotonic seq 1,2 got %d,%d", s1, s2)
	}
	s1again, dup, err := repo.Append(ctx, "g1", dev, "cm-1", "application", []byte("ct-1"))
	if err != nil || !dup || s1again != s1 {
		t.Fatalf("idempotent replay: seq=%d dup=%v err=%v", s1again, dup, err)
	}
	sg2, _, _ := repo.Append(ctx, "g2", dev, "cm-1", "application", []byte("x"))
	if sg2 != 1 {
		t.Fatalf("group g2 should start at seq 1, got %d", sg2)
	}
}

func TestListSinceRespectsJoinSeq(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := NewMessageRepo(pool)
	dev := "11111111-1111-1111-1111-111111111111"
	for i := 0; i < 5; i++ {
		repo.Append(ctx, "g1", dev, "cm-"+string(rune('a'+i)), "application", []byte("ct"))
	}
	msgs, err := repo.ListSince(ctx, "g1", 2, 0, 100)
	if err != nil {
		t.Fatalf("listsince: %v", err)
	}
	if len(msgs) != 3 || msgs[0].Seq != 3 || msgs[2].Seq != 5 {
		t.Fatalf("expected seqs 3..5, got %+v", msgs)
	}
	msgs2, _ := repo.ListSince(ctx, "g1", 0, 4, 100)
	if len(msgs2) != 2 || msgs2[0].Seq != 4 {
		t.Fatalf("join_seq floor not applied, got %+v", msgs2)
	}
}
