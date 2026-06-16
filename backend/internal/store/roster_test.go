package store

import (
	"context"
	"testing"
)

func TestRosterAddListRemove(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := NewRosterRepo(pool)
	devA := "aaaaaaaa-1111-1111-1111-111111111111"
	devB := "bbbbbbbb-1111-1111-1111-111111111111"

	if err := repo.AddMember(ctx, "g1", devA, 0); err != nil {
		t.Fatalf("add A: %v", err)
	}
	if err := repo.AddMember(ctx, "g1", devB, 5); err != nil {
		t.Fatalf("add B: %v", err)
	}
	if err := repo.AddMember(ctx, "g1", devA, 0); err != nil {
		t.Fatalf("re-add A: %v", err)
	}

	members, err := repo.Members(ctx, "g1")
	if err != nil || len(members) != 2 {
		t.Fatalf("members: %d %v", len(members), err)
	}
	if ok, _ := repo.IsMember(ctx, "g1", devA); !ok {
		t.Fatal("A should be a member")
	}

	if err := repo.RemoveMember(ctx, "g1", devB); err != nil {
		t.Fatalf("remove B: %v", err)
	}
	if ok, _ := repo.IsMember(ctx, "g1", devB); ok {
		t.Fatal("B should no longer be a member")
	}
}
