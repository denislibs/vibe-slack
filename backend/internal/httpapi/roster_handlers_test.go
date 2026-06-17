package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	xopaque "github.com/bytemare/opaque"
	"github.com/messenger/backend/internal/as"
	"github.com/messenger/backend/internal/devices"
	"github.com/messenger/backend/internal/keypackages"
	"github.com/messenger/backend/internal/kt"
	"github.com/messenger/backend/internal/opaque"
	"github.com/messenger/backend/internal/platform/postgres"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
	goredis "github.com/redis/go-redis/v9"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// newRosterServer builds a full server (like newFullServer) but also constructs a
// *store.RosterRepo from the same pool, passes it to NewRouterFull, and returns
// both the handler and the roster repo so the test can inspect roster state.
func newRosterServer(t *testing.T) (http.Handler, *store.RosterRepo) {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("as"), tcpostgres.WithUsername("as"), tcpostgres.WithPassword("as"),
		tcpostgres.BasicWaitStrategies())
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, _ := ctr.ConnectionString(ctx, "sslmode=disable")
	pool, _ := postgres.Connect(ctx, dsn)
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := xopaque.DefaultConfiguration()
	sk, pk := cfg.KeyGen()
	osrv, _ := opaque.NewServer(sk.Encode(), pk.Encode(), cfg.GenerateOPRFSeed(), []byte("messenger-as"))
	mr, _ := miniredis.Run()
	t.Cleanup(mr.Close)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	sess := session.NewManager(rdb, time.Hour)

	svc := as.NewService(osrv, store.NewUserRepo(pool), sess, rdb)
	devSvc := devices.NewService(store.NewDeviceRepo(pool))
	kpSvc := keypackages.NewService(store.NewKeyPackageRepo(pool))
	rl := session.NewRateLimiter(rdb, 1000, time.Minute)
	roster := store.NewRosterRepo(pool)
	ktPub, _, _ := ed25519.GenerateKey(rand.Reader)
	ktSvc := kt.NewService(store.NewKTRepo(pool))
	return NewRouterFull(svc, sess, devSvc, kpSvc, rl, roster, ktSvc, ktPub, nil, nil, nil, nil, nil, nil, ""), roster
}

// fakeRosterStore records calls so tests can assert the store was (or was not)
// invoked.
type fakeRosterStore struct {
	addErr      error
	removeErr   error
	addCalls    []rosterCall
	removeCalls []rosterCall
}

type rosterCall struct {
	groupID  string
	deviceID string
	joinSeq  int64
}

func (f *fakeRosterStore) AddMember(_ context.Context, groupID, deviceID string, joinSeq int64) error {
	f.addCalls = append(f.addCalls, rosterCall{groupID, deviceID, joinSeq})
	return f.addErr
}
func (f *fakeRosterStore) RemoveMember(_ context.Context, groupID, deviceID string) error {
	f.removeCalls = append(f.removeCalls, rosterCall{groupID: groupID, deviceID: deviceID})
	return f.removeErr
}

// fakeMembership returns a configurable membership result.
type fakeMembership struct {
	member bool
	err    error
}

func (f *fakeMembership) IsMember(context.Context, string, string) (bool, error) {
	return f.member, f.err
}

func TestRosterAddMember_NonMemberForbidden(t *testing.T) {
	store := &fakeRosterStore{}
	h := &rosterHandlers{roster: store, members: &fakeMembership{member: false}}

	req := withSession(httptest.NewRequest(http.MethodPost, "/conversations/g1/members",
		strings.NewReader(`{"device_id":"d1","join_seq":0}`)))
	req.SetPathValue("group", "g1")
	rec := httptest.NewRecorder()
	h.addMember(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-member, got %d %s", rec.Code, rec.Body)
	}
	if len(store.addCalls) != 0 {
		t.Fatalf("roster store must not be called for non-member, got %+v", store.addCalls)
	}
}

func TestRosterAddMember_MemberAllowed(t *testing.T) {
	store := &fakeRosterStore{}
	h := &rosterHandlers{roster: store, members: &fakeMembership{member: true}}

	req := withSession(httptest.NewRequest(http.MethodPost, "/conversations/g1/members",
		strings.NewReader(`{"device_id":"d1","join_seq":7}`)))
	req.SetPathValue("group", "g1")
	rec := httptest.NewRecorder()
	h.addMember(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for member, got %d %s", rec.Code, rec.Body)
	}
	if len(store.addCalls) != 1 || store.addCalls[0] != (rosterCall{"g1", "d1", 7}) {
		t.Fatalf("expected one AddMember call (g1,d1,7), got %+v", store.addCalls)
	}
}

func TestRosterRemoveMember_NonMemberForbidden(t *testing.T) {
	store := &fakeRosterStore{}
	h := &rosterHandlers{roster: store, members: &fakeMembership{member: false}}

	req := withSession(httptest.NewRequest(http.MethodDelete, "/conversations/g1/members/d1", nil))
	req.SetPathValue("group", "g1")
	req.SetPathValue("device", "d1")
	rec := httptest.NewRecorder()
	h.removeMember(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-member, got %d %s", rec.Code, rec.Body)
	}
	if len(store.removeCalls) != 0 {
		t.Fatalf("roster store must not be called for non-member, got %+v", store.removeCalls)
	}
}

func TestRosterRemoveMember_MemberAllowed(t *testing.T) {
	store := &fakeRosterStore{}
	h := &rosterHandlers{roster: store, members: &fakeMembership{member: true}}

	req := withSession(httptest.NewRequest(http.MethodDelete, "/conversations/g1/members/d1", nil))
	req.SetPathValue("group", "g1")
	req.SetPathValue("device", "d1")
	rec := httptest.NewRecorder()
	h.removeMember(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for member, got %d %s", rec.Code, rec.Body)
	}
	if len(store.removeCalls) != 1 || store.removeCalls[0] != (rosterCall{groupID: "g1", deviceID: "d1"}) {
		t.Fatalf("expected one RemoveMember call (g1,d1), got %+v", store.removeCalls)
	}
}

// enrollDevice POSTs /devices and returns the new device_id.
func enrollDevice(t *testing.T, h http.Handler, token string) string {
	t.Helper()
	rec := postJSON(t, h, "/devices", map[string]any{
		"signing_public_key":   b64([]byte("ed25519-pub")),
		"label":                "dev",
		"initial_key_packages": []string{},
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll device: %d %s", rec.Code, rec.Body)
	}
	var dr struct {
		DeviceID string `json:"device_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &dr)
	if dr.DeviceID == "" {
		t.Fatal("no device_id")
	}
	return dr.DeviceID
}
