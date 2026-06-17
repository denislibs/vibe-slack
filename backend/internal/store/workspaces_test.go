package store

import (
	"context"
	"errors"
	"testing"
)

func TestWorkspaceRepoLifecycle(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	users := NewUserRepo(pool)
	repo := NewWorkspaceRepo(pool)

	owner, _ := users.Create(ctx, "owner@corp", "owner", []byte("r"))
	bob, _ := users.Create(ctx, "bob@corp", "bob", []byte("r"))

	ws, err := repo.Create(ctx, "Acme", "acme", owner.ID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	role, err := repo.RoleOf(ctx, ws.ID, owner.ID)
	if err != nil || role != RoleOwner {
		t.Fatalf("owner role: %v %q", err, role)
	}
	if _, err := repo.RoleOf(ctx, ws.ID, bob.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	exists, _ := repo.SlugExists(ctx, "acme")
	if !exists {
		t.Fatal("slug should exist")
	}
	if err := repo.AddMember(ctx, ws.ID, bob.ID, RoleMember); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := repo.AddMember(ctx, ws.ID, bob.ID, RoleMember); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict on dup add, got %v", err)
	}
	ms, _ := repo.Members(ctx, ws.ID)
	if len(ms) != 2 {
		t.Fatalf("expected 2 members, got %d", len(ms))
	}
	list, _ := repo.ListForUser(ctx, bob.ID)
	if len(list) != 1 || list[0].Role != RoleMember || list[0].Slug != "acme" {
		t.Fatalf("ListForUser: %+v", list)
	}
	if err := repo.SetRole(ctx, ws.ID, bob.ID, RoleAdmin); err != nil {
		t.Fatalf("setrole: %v", err)
	}
	if r2, _ := repo.RoleOf(ctx, ws.ID, bob.ID); r2 != RoleAdmin {
		t.Fatalf("role not updated: %q", r2)
	}
	if err := repo.RemoveMember(ctx, ws.ID, bob.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := repo.RemoveMember(ctx, ws.ID, bob.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound removing absent member, got %v", err)
	}
}

func TestSearchMembers(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	users := NewUserRepo(pool)
	repo := NewWorkspaceRepo(pool)

	owner, _ := users.Create(ctx, "owner@corp", "owner", []byte("r"))
	alice, _ := users.Create(ctx, "alice@corp", "alice", []byte("r"))
	abel, _ := users.Create(ctx, "abel@corp", "abel", []byte("r"))
	other, _ := users.Create(ctx, "alvin@elsewhere", "alvin", []byte("r"))

	ws, err := repo.Create(ctx, "Acme", "acme", owner.ID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.AddMember(ctx, ws.ID, alice.ID, RoleMember); err != nil {
		t.Fatalf("add alice: %v", err)
	}
	if err := repo.AddMember(ctx, ws.ID, abel.ID, RoleMember); err != nil {
		t.Fatalf("add abel: %v", err)
	}
	// other belongs to a DIFFERENT workspace; must never appear in results.
	otherWS, err := repo.Create(ctx, "Other", "other", other.ID)
	if err != nil {
		t.Fatalf("create other ws: %v", err)
	}
	_ = otherWS

	// Prefix "al" matches alice (username/email) but not abel and not the
	// other-workspace user even though "alvin" shares the prefix.
	got, err := repo.SearchMembers(ctx, ws.ID, "al")
	if err != nil {
		t.Fatalf("search al: %v", err)
	}
	if len(got) != 1 || got[0].Username != "alice" {
		t.Fatalf("search al → %+v, want only alice", got)
	}

	// Case-insensitive prefix.
	got, err = repo.SearchMembers(ctx, ws.ID, "AL")
	if err != nil || len(got) != 1 || got[0].Username != "alice" {
		t.Fatalf("case-insensitive search → %+v err %v", got, err)
	}

	// "ab" matches abel only.
	got, err = repo.SearchMembers(ctx, ws.ID, "ab")
	if err != nil || len(got) != 1 || got[0].Username != "abel" {
		t.Fatalf("search ab → %+v err %v", got, err)
	}

	// Search by email prefix.
	got, err = repo.SearchMembers(ctx, ws.ID, "abel@")
	if err != nil || len(got) != 1 || got[0].Username != "abel" {
		t.Fatalf("search by email → %+v err %v", got, err)
	}

	// Empty q returns no rows.
	got, err = repo.SearchMembers(ctx, ws.ID, "")
	if err != nil || len(got) != 0 {
		t.Fatalf("empty q → %+v err %v, want empty", got, err)
	}
}
