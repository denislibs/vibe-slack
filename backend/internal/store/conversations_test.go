package store

import (
	"context"
	"errors"
	"testing"
)

func TestConvRepo(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	users := NewUserRepo(pool)
	wsRepo := NewWorkspaceRepo(pool)
	repo := NewConvRepo(pool)

	owner, _ := users.Create(ctx, "o@c", "owner", []byte("r"))
	bob, _ := users.Create(ctx, "b@c", "bob", []byte("r"))
	carol, _ := users.Create(ctx, "c@c", "carol", []byte("r"))
	ws, _ := wsRepo.Create(ctx, "Acme", "acme", owner.ID)
	// make bob & carol workspace members so listing is realistic
	wsRepo.AddMember(ctx, ws.ID, bob.ID, RoleMember)
	wsRepo.AddMember(ctx, ws.ID, carol.ID, RoleMember)

	// public channel created by owner
	pub, err := repo.CreateChannel(ctx, "pub1", ws.ID, "public", "general", owner.ID)
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if pub.Type != "channel" || pub.Visibility != "public" {
		t.Fatalf("bad channel: %+v", pub)
	}
	if ok, _ := repo.IsMember(ctx, "pub1", owner.ID); !ok {
		t.Fatal("creator should be a member")
	}

	// private channel created by owner (bob NOT a member)
	repo.CreateChannel(ctx, "priv1", ws.ID, "private", "secret", owner.ID)

	// DM owner<->bob, idempotent
	dm, created, err := repo.GetOrCreateDM(ctx, "dm1", ws.ID, owner.ID, bob.ID)
	if err != nil || !created || dm.Type != "dm" {
		t.Fatalf("create dm: %v created=%v %+v", err, created, dm)
	}
	dm2, created2, _ := repo.GetOrCreateDM(ctx, "dmX", ws.ID, bob.ID, owner.ID) // reversed order, same pair
	if created2 || dm2.GroupID != "dm1" {
		t.Fatalf("dm should be idempotent per pair: created=%v id=%s", created2, dm2.GroupID)
	}

	// AddUser/RemoveUser
	if err := repo.AddUser(ctx, "priv1", carol.ID); err != nil {
		t.Fatalf("add carol: %v", err)
	}
	if err := repo.AddUser(ctx, "priv1", carol.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("dup add → ErrConflict, got %v", err)
	}
	if err := repo.RemoveUser(ctx, "priv1", carol.ID); err != nil {
		t.Fatalf("remove carol: %v", err)
	}
	if err := repo.RemoveUser(ctx, "priv1", carol.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove absent → ErrNotFound, got %v", err)
	}

	// ListForUser(bob): sees pub1 (public) + dm1 (member), NOT priv1 (private, not a member)
	list, _ := repo.ListForUser(ctx, ws.ID, bob.ID)
	ids := map[string]bool{}
	for _, c := range list {
		ids[c.GroupID] = true
	}
	if !ids["pub1"] || !ids["dm1"] || ids["priv1"] {
		t.Fatalf("ListForUser(bob) wrong: %v", ids)
	}

	// Get + MemberUserIDs
	got, err := repo.Get(ctx, "dm1")
	if err != nil || got.WorkspaceID != ws.ID {
		t.Fatalf("get: %v %+v", err, got)
	}
	m, _ := repo.MemberUserIDs(ctx, "dm1")
	if len(m) != 2 {
		t.Fatalf("dm members = %d, want 2", len(m))
	}
	if _, err := repo.Get(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get absent → ErrNotFound, got %v", err)
	}
}
