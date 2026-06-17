package workspace

import (
	"context"
	"errors"
	"testing"

	"github.com/messenger/backend/internal/store"
)

type fakeRepo struct {
	roles map[string]map[string]string
	slugs map[string]bool
	seq   int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{roles: map[string]map[string]string{}, slugs: map[string]bool{}}
}

func (f *fakeRepo) Create(_ context.Context, name, slug, owner string) (*store.Workspace, error) {
	f.seq++
	id := "ws" + string(rune('0'+f.seq))
	f.roles[id] = map[string]string{owner: store.RoleOwner}
	f.slugs[slug] = true
	return &store.Workspace{ID: id, Name: name, Slug: slug, OwnerUserID: owner}, nil
}
func (f *fakeRepo) SlugExists(_ context.Context, slug string) (bool, error) { return f.slugs[slug], nil }
func (f *fakeRepo) ListForUser(context.Context, string) ([]store.WorkspaceWithRole, error) {
	return nil, nil
}
func (f *fakeRepo) Members(context.Context, string) ([]store.WorkspaceMember, error) { return nil, nil }
func (f *fakeRepo) RoleOf(_ context.Context, ws, u string) (string, error) {
	if r, ok := f.roles[ws][u]; ok {
		return r, nil
	}
	return "", store.ErrNotFound
}
func (f *fakeRepo) AddMember(_ context.Context, ws, u, role string) error {
	if _, ok := f.roles[ws][u]; ok {
		return store.ErrConflict
	}
	f.roles[ws][u] = role
	return nil
}
func (f *fakeRepo) RemoveMember(_ context.Context, ws, u string) error {
	if _, ok := f.roles[ws][u]; !ok {
		return store.ErrNotFound
	}
	delete(f.roles[ws], u)
	return nil
}
func (f *fakeRepo) SetRole(_ context.Context, ws, u, role string) error {
	if _, ok := f.roles[ws][u]; !ok {
		return store.ErrNotFound
	}
	f.roles[ws][u] = role
	return nil
}

type fakeUsers struct{ byKey map[string]*store.User }

func (f *fakeUsers) FindByEmailOrUsername(_ context.Context, q string) (*store.User, error) {
	if u, ok := f.byKey[q]; ok {
		return u, nil
	}
	return nil, store.ErrNotFound
}

func setup() (*Service, *fakeRepo, string) {
	repo := newFakeRepo()
	users := &fakeUsers{byKey: map[string]*store.User{
		"bob@corp": {ID: "bob", Email: "bob@corp", Username: "bob"},
		"bob":      {ID: "bob", Email: "bob@corp", Username: "bob"},
		"carol":    {ID: "carol", Email: "carol@corp", Username: "carol"},
	}}
	svc := NewService(repo, users)
	ws, _ := svc.Create(context.Background(), "owner", "Acme")
	return svc, repo, ws.ID
}

func TestCreateMakesOwnerAndSlug(t *testing.T) {
	svc, _, _ := setup()
	ws, err := svc.Create(context.Background(), "u2", "Acme")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ws.Slug != "acme-2" {
		t.Fatalf("expected deduped slug acme-2, got %q", ws.Slug)
	}
}

func TestAddMemberAuthz(t *testing.T) {
	ctx := context.Background()
	svc, repo, ws := setup()
	repo.roles[ws]["m"] = store.RoleMember
	if _, err := svc.AddMember(ctx, "m", ws, "bob@corp"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member add should be forbidden, got %v", err)
	}
	if _, err := svc.AddMember(ctx, "stranger", ws, "bob@corp"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("stranger → ErrNotMember, got %v", err)
	}
	m, err := svc.AddMember(ctx, "owner", ws, "bob@corp")
	if err != nil || m.Role != store.RoleMember || m.UserID != "bob" {
		t.Fatalf("owner add: %v %+v", err, m)
	}
	if _, err := svc.AddMember(ctx, "owner", ws, "bob"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("dup add → ErrConflict, got %v", err)
	}
	if _, err := svc.AddMember(ctx, "owner", ws, "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user → ErrNotFound, got %v", err)
	}
}

func TestSetRoleAndRemoveAuthz(t *testing.T) {
	ctx := context.Background()
	svc, repo, ws := setup()
	repo.roles[ws]["admin1"] = store.RoleAdmin
	repo.roles[ws]["mem1"] = store.RoleMember

	if err := svc.SetRole(ctx, "admin1", ws, "mem1", store.RoleAdmin); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin set role forbidden, got %v", err)
	}
	if err := svc.SetRole(ctx, "owner", ws, "mem1", store.RoleAdmin); err != nil {
		t.Fatalf("owner set role: %v", err)
	}
	if err := svc.SetRole(ctx, "owner", ws, "owner", store.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("demote owner forbidden, got %v", err)
	}
	repo.roles[ws]["mem2"] = store.RoleMember
	// mem1 was promoted to admin above; admin1 removing an admin → forbidden
	if err := svc.RemoveMember(ctx, "admin1", ws, "mem1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin removing admin forbidden, got %v", err)
	}
	if err := svc.RemoveMember(ctx, "admin1", ws, "mem2"); err != nil {
		t.Fatalf("admin remove member: %v", err)
	}
	if err := svc.RemoveMember(ctx, "owner", ws, "owner"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("remove owner forbidden, got %v", err)
	}
	if err := svc.Leave(ctx, "owner", ws); !errors.Is(err, ErrForbidden) {
		t.Fatalf("owner leave forbidden, got %v", err)
	}
	if err := svc.Leave(ctx, "admin1", ws); err != nil {
		t.Fatalf("admin leave: %v", err)
	}
}
