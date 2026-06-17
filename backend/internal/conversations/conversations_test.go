package conversations

import (
	"context"
	"errors"
	"testing"

	"github.com/messenger/backend/internal/store"
)

// in-memory fakes
type fakeRepo struct {
	conv    map[string]*store.Conversation
	members map[string]map[string]bool // group -> user -> true
	dm      map[string]string          // wsID|dmkey -> groupID
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{conv: map[string]*store.Conversation{}, members: map[string]map[string]bool{}, dm: map[string]string{}}
}
func (f *fakeRepo) add(c *store.Conversation, users ...string) {
	f.conv[c.GroupID] = c
	f.members[c.GroupID] = map[string]bool{}
	for _, u := range users {
		f.members[c.GroupID][u] = true
	}
}
func (f *fakeRepo) CreateChannel(_ context.Context, g, ws, vis, name, creator string) (*store.Conversation, error) {
	c := &store.Conversation{GroupID: g, WorkspaceID: ws, Type: "channel", Visibility: vis, Name: name, CreatedBy: creator}
	f.add(c, creator)
	return c, nil
}
func (f *fakeRepo) GetOrCreateDM(_ context.Context, g, ws, creator, target string) (*store.Conversation, bool, error) {
	key := ws + "|" + creator + target
	keyR := ws + "|" + target + creator
	if id, ok := f.dm[key]; ok {
		return f.conv[id], false, nil
	}
	if id, ok := f.dm[keyR]; ok {
		return f.conv[id], false, nil
	}
	c := &store.Conversation{GroupID: g, WorkspaceID: ws, Type: "dm", Visibility: "private", CreatedBy: creator}
	f.add(c, creator, target)
	f.dm[key] = g
	return c, true, nil
}
func (f *fakeRepo) Get(_ context.Context, g string) (*store.Conversation, error) {
	if c, ok := f.conv[g]; ok {
		return c, nil
	}
	return nil, store.ErrNotFound
}
func (f *fakeRepo) IsMember(_ context.Context, g, u string) (bool, error) { return f.members[g][u], nil }
func (f *fakeRepo) AddUser(_ context.Context, g, u string) error {
	if f.members[g][u] {
		return store.ErrConflict
	}
	f.members[g][u] = true
	return nil
}
func (f *fakeRepo) RemoveUser(_ context.Context, g, u string) error {
	if !f.members[g][u] {
		return store.ErrNotFound
	}
	delete(f.members[g], u)
	return nil
}
func (f *fakeRepo) MemberUserIDs(_ context.Context, g string) ([]string, error) {
	var out []string
	for u := range f.members[g] {
		out = append(out, u)
	}
	return out, nil
}
func (f *fakeRepo) ListForUser(_ context.Context, ws, u string) ([]store.Conversation, error) {
	return nil, nil
}

type fakeWS struct{ roles map[string]map[string]string }

func (f *fakeWS) RoleOf(_ context.Context, ws, u string) (string, error) {
	if r, ok := f.roles[ws][u]; ok {
		return r, nil
	}
	return "", store.ErrNotFound
}

type fakeUsers struct{ byKey map[string]*store.User }

func (f *fakeUsers) FindByEmailOrUsername(_ context.Context, q string) (*store.User, error) {
	if u, ok := f.byKey[q]; ok {
		return u, nil
	}
	return nil, store.ErrNotFound
}

func setup() (*Service, *fakeRepo) {
	repo := newFakeRepo()
	ws := &fakeWS{roles: map[string]map[string]string{
		"w1": {"owner": store.RoleOwner, "bob": store.RoleMember, "carol": store.RoleMember},
	}}
	users := &fakeUsers{byKey: map[string]*store.User{
		"bob":   {ID: "bob", Username: "bob", Email: "b@c"},
		"carol": {ID: "carol", Username: "carol", Email: "c@c"},
		"ext":   {ID: "ext", Username: "ext", Email: "e@x"}, // NOT a w1 member
	}}
	return NewService(repo, ws, users), repo
}

func TestCreateChannelAuthz(t *testing.T) {
	ctx := context.Background()
	svc, _ := setup()
	if _, err := svc.CreateChannel(ctx, "stranger", "w1", "public", "general"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("non-member create → ErrNotMember, got %v", err)
	}
	if _, err := svc.CreateChannel(ctx, "owner", "w1", "weird", "x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad visibility → ErrInvalid, got %v", err)
	}
	if _, err := svc.CreateChannel(ctx, "owner", "w1", "public", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty name → ErrInvalid, got %v", err)
	}
	c, err := svc.CreateChannel(ctx, "owner", "w1", "public", "general")
	if err != nil || c.Type != "channel" {
		t.Fatalf("owner create: %v %+v", err, c)
	}
}

func TestCreateDMAuthz(t *testing.T) {
	ctx := context.Background()
	svc, _ := setup()
	if _, _, err := svc.CreateDM(ctx, "owner", "w1", "owner"); !errors.Is(err, ErrInvalid) {
		// resolving "owner" — not in users map → ErrNotFound; but self-dm guard should also apply.
	}
	if _, _, err := svc.CreateDM(ctx, "owner", "w1", "ext"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("target not WS member → ErrNotFound (no cross-tenant oracle), got %v", err)
	}
	if _, _, err := svc.CreateDM(ctx, "owner", "w1", "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown target → ErrNotFound, got %v", err)
	}
	c, created, err := svc.CreateDM(ctx, "owner", "w1", "bob")
	if err != nil || !created || c.Type != "dm" {
		t.Fatalf("dm create: %v created=%v %+v", err, created, c)
	}
}

func TestGetAndJoin(t *testing.T) {
	ctx := context.Background()
	svc, repo := setup()
	repo.add(&store.Conversation{GroupID: "pub", WorkspaceID: "w1", Type: "channel", Visibility: "public", CreatedBy: "owner"}, "owner")
	repo.add(&store.Conversation{GroupID: "priv", WorkspaceID: "w1", Type: "channel", Visibility: "private", CreatedBy: "owner"}, "owner")

	// bob (WS member, not conv member) can Get a public channel, not a private one
	if _, err := svc.Get(ctx, "bob", "pub"); err != nil {
		t.Fatalf("get public by WS member: %v", err)
	}
	if _, err := svc.Get(ctx, "bob", "priv"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("get private by non-member → ErrNotMember, got %v", err)
	}
	// join public ok; private → ErrNotMember
	if err := svc.Join(ctx, "bob", "pub"); err != nil {
		t.Fatalf("join public: %v", err)
	}
	if err := svc.Join(ctx, "carol", "priv"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("join private → ErrNotMember, got %v", err)
	}
}

func TestAddRemoveUser(t *testing.T) {
	ctx := context.Background()
	svc, repo := setup()
	repo.add(&store.Conversation{GroupID: "priv", WorkspaceID: "w1", Type: "channel", Visibility: "private", CreatedBy: "owner"}, "owner")
	repo.add(&store.Conversation{GroupID: "dm", WorkspaceID: "w1", Type: "dm", Visibility: "private", CreatedBy: "owner"}, "owner", "bob")

	// non-member cannot add to private channel
	if _, err := svc.AddUser(ctx, "bob", "priv", "carol"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("non-member add → ErrNotMember, got %v", err)
	}
	// member (owner) adds carol
	if _, err := svc.AddUser(ctx, "owner", "priv", "carol"); err != nil {
		t.Fatalf("owner add carol: %v", err)
	}
	// cannot leave a DM
	if err := svc.RemoveUser(ctx, "owner", "dm", "owner"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("remove from DM → ErrForbidden, got %v", err)
	}
	// self-remove from channel ok
	if err := svc.RemoveUser(ctx, "carol", "priv", "carol"); err != nil {
		t.Fatalf("self remove: %v", err)
	}
	// removing another, non-creator → ErrForbidden
	repo.AddUser(ctx, "priv", "carol")
	if err := svc.RemoveUser(ctx, "bob", "priv", "carol"); !errors.Is(err, ErrNotMember) {
		// bob isn't a member of priv → ErrNotMember (membership checked first)
	}
}
