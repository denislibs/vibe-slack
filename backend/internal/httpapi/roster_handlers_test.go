package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	xopaque "github.com/bytemare/opaque"
	"github.com/messenger/backend/internal/as"
	"github.com/messenger/backend/internal/devices"
	"github.com/messenger/backend/internal/keypackages"
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
	return NewRouterFull(svc, sess, devSvc, kpSvc, rl, roster), roster
}

func TestRosterAddAndListMembers(t *testing.T) {
	h, roster := newRosterServer(t)
	token := registerAndLogin(t, h, "grace@corp", "roster-pass")
	deviceID := enrollDevice(t, h, token)

	rec := postJSON(t, h, "/conversations/g1/members", map[string]any{
		"device_id": deviceID, "join_seq": 0,
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("add member: %d %s", rec.Code, rec.Body)
	}

	members, _ := roster.Members(context.Background(), "g1")
	if len(members) != 1 || members[0].DeviceID != deviceID {
		t.Fatalf("expected device in roster, got %+v", members)
	}

	// Remove it.
	req, _ := http.NewRequest(http.MethodDelete, "/conversations/g1/members/"+deviceID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec2 := serve(h, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("remove member: %d %s", rec2.Code, rec2.Body)
	}
	members2, _ := roster.Members(context.Background(), "g1")
	if len(members2) != 0 {
		t.Fatalf("expected empty roster after remove, got %+v", members2)
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
