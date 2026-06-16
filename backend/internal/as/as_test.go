package as

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	xopaque "github.com/bytemare/opaque"
	"github.com/messenger/backend/internal/opaque"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
	goredis "github.com/redis/go-redis/v9"
)

type fakeUserStore struct {
	byEmail map[string]*store.User
	nextID  int
}

func newFakeUsers() *fakeUserStore { return &fakeUserStore{byEmail: map[string]*store.User{}} }

func (f *fakeUserStore) Create(_ context.Context, email string, rec []byte) (*store.User, error) {
	if _, ok := f.byEmail[email]; ok {
		return nil, store.ErrConflict
	}
	f.nextID++
	u := &store.User{ID: string(rune('a' + f.nextID)), Email: email, OpaqueRecord: rec}
	f.byEmail[email] = u
	return u, nil
}
func (f *fakeUserStore) GetByEmail(_ context.Context, email string) (*store.User, error) {
	u, ok := f.byEmail[email]
	if !ok {
		return nil, store.ErrNotFound
	}
	return u, nil
}

func newAS(t *testing.T) *Service {
	t.Helper()
	cfg := xopaque.DefaultConfiguration()
	sk, pk := cfg.KeyGen()
	srv, err := opaque.NewServer(sk.Encode(), pk.Encode(), cfg.GenerateOPRFSeed(), []byte("messenger-as"))
	if err != nil {
		t.Fatalf("opaque server: %v", err)
	}
	mr, _ := miniredis.Run()
	t.Cleanup(mr.Close)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	sess := session.NewManager(rdb, time.Hour)
	return NewService(srv, newFakeUsers(), sess, rdb)
}

func TestRegisterAndLoginRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := newAS(t)
	cfg := xopaque.DefaultConfiguration()
	client, _ := cfg.Client()
	pw := []byte("hunter2hunter2")

	regReq, _ := client.RegistrationInit(pw)
	respBytes, err := svc.RegisterStart(ctx, "bob@corp", regReq.Serialize())
	if err != nil {
		t.Fatalf("RegisterStart: %v", err)
	}
	regResp, _ := client.Deserialize.RegistrationResponse(respBytes)
	record, _, _ := client.RegistrationFinalize(regResp, nil, []byte("messenger-as"))
	if err := svc.RegisterFinish(ctx, "bob@corp", record.Serialize()); err != nil {
		t.Fatalf("RegisterFinish: %v", err)
	}

	client2, _ := cfg.Client()
	ke1, _ := client2.GenerateKE1(pw)
	loginID, ke2Bytes, err := svc.LoginStart(ctx, "bob@corp", ke1.Serialize())
	if err != nil {
		t.Fatalf("LoginStart: %v", err)
	}
	ke2, _ := client2.Deserialize.KE2(ke2Bytes)
	ke3, _, _, _ := client2.GenerateKE3(ke2, nil, []byte("messenger-as"))
	token, enrollRequired, err := svc.LoginFinish(ctx, loginID, ke3.Serialize())
	if err != nil {
		t.Fatalf("LoginFinish: %v", err)
	}
	if token == "" {
		t.Fatal("expected a session token")
	}
	if !enrollRequired {
		t.Fatal("first login should require device enrollment")
	}
}

func TestLoginUnknownUserDoesNotRevealAbsence(t *testing.T) {
	ctx := context.Background()
	svc := newAS(t)
	cfg := xopaque.DefaultConfiguration()
	client, _ := cfg.Client()
	ke1, _ := client.GenerateKE1([]byte("whatever"))

	loginID, ke2Bytes, err := svc.LoginStart(ctx, "ghost@corp", ke1.Serialize())
	if err != nil {
		t.Fatalf("LoginStart for unknown user should not error: %v", err)
	}
	if loginID == "" || len(ke2Bytes) == 0 {
		t.Fatal("expected a KE2 indistinguishable from a real user")
	}
	ke2, _ := client.Deserialize.KE2(ke2Bytes)
	ke3, _, _, kerr := client.GenerateKE3(ke2, nil, []byte("messenger-as"))
	if kerr == nil {
		if _, _, err := svc.LoginFinish(ctx, loginID, ke3.Serialize()); !errors.Is(err, ErrAuthFailed) {
			t.Fatalf("expected ErrAuthFailed, got %v", err)
		}
	}
}
