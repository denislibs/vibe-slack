package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
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

// ktEnv exposes the KT relay so tests can advance the log.
type ktEnv struct {
	relay *kt.Relay
}

// newKTServer builds a full server like newFullServer and additionally wires the
// KT service, public key and relay, returning the handler + a ktEnv.
func newKTServer(t *testing.T) (http.Handler, *ktEnv) {
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

	ktPub, ktPriv, _ := ed25519.GenerateKey(rand.Reader)
	ktRepo := store.NewKTRepo(pool)
	ktSvc := kt.NewService(ktRepo)
	relay := kt.NewRelay(pool, ktRepo, store.NewDeviceRepo(pool), kt.NewSTHSigner(ktPriv))

	h := NewRouterFull(svc, sess, devSvc, kpSvc, rl, roster, ktSvc, ktPub, nil, nil, nil, nil, nil, nil, "")
	return h, &ktEnv{relay: relay}
}

// sessionUserID returns the user id bound to the given session token.
func sessionUserID(t *testing.T, h http.Handler, token string) string {
	t.Helper()
	rec := serve(h, httptestNewGet("/auth/session", token))
	if rec.Code != http.StatusOK {
		t.Fatalf("session: %d %s", rec.Code, rec.Body)
	}
	var sr sessionResp
	json.Unmarshal(rec.Body.Bytes(), &sr)
	if sr.UserID == "" {
		t.Fatal("no user_id in session response")
	}
	return sr.UserID
}

func TestKTKeyLookupEndpoint(t *testing.T) {
	h, env := newKTServer(t)
	token := registerAndLogin(t, h, "kt@corp", "kt-pass")
	enrollDevice(t, h, token)
	if err := env.relay.Tick(context.Background()); err != nil {
		t.Fatalf("relay tick: %v", err)
	}
	userID := sessionUserID(t, h, token)

	rec := serve(h, httptestNewGet("/kt/key/"+userID, token))
	if rec.Code != http.StatusOK {
		t.Fatalf("kt key: %d %s", rec.Code, rec.Body)
	}
	var resp ktKeyResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Version < 1 || resp.STH.TreeSize < 1 {
		t.Fatalf("unexpected kt key response: %s", rec.Body)
	}

	rec2 := serve(h, httptestNewGet("/kt/pubkey", token))
	var pk struct {
		KTPublicKey string `json:"kt_public_key"`
	}
	json.Unmarshal(rec2.Body.Bytes(), &pk)
	raw, _ := base64.StdEncoding.DecodeString(pk.KTPublicKey)
	if len(raw) != ed25519.PublicKeySize {
		t.Fatalf("kt pubkey wrong size: %d", len(raw))
	}
}
