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

func newFullServer(t *testing.T) http.Handler {
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
	return NewRouterFull(svc, sess, devSvc, kpSvc, rl, roster, ktSvc, ktPub)
}

// usernameFromEmail derives a username valid under as.ValidateUsername
// (lowercase letters, digits, underscore; 3-32 chars) from a test email.
func usernameFromEmail(email string) string {
	var b []rune
	for _, r := range email {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b = append(b, r)
		case r >= 'A' && r <= 'Z':
			b = append(b, r+('a'-'A'))
		case r == '@':
			b = append(b, '_') // stop at domain via padding below; keep local part
		}
		if r == '@' {
			break
		}
	}
	for len(b) < 3 {
		b = append(b, '_')
	}
	if len(b) > 32 {
		b = b[:32]
	}
	return string(b)
}

func registerAndLogin(t *testing.T, h http.Handler, email, password string) string {
	t.Helper()
	cfg := xopaque.DefaultConfiguration()
	client, _ := cfg.Client()
	pw := []byte(password)
	regReq, _ := client.RegistrationInit(pw)
	rec := postJSON(t, h, "/auth/register/start",
		map[string]string{"email": email, "opaque_registration_request": b64(regReq.Serialize())}, "")
	var rs registerStartResp
	json.Unmarshal(rec.Body.Bytes(), &rs)
	respBytes, _ := base64.StdEncoding.DecodeString(rs.OpaqueRegistrationResponse)
	regResp, _ := client.Deserialize.RegistrationResponse(respBytes)
	record, _, _ := client.RegistrationFinalize(regResp, nil, []byte("messenger-as"))
	postJSON(t, h, "/auth/register/finish",
		map[string]string{"email": email, "username": usernameFromEmail(email), "opaque_registration_record": b64(record.Serialize())}, "")

	client2, _ := cfg.Client()
	ke1, _ := client2.GenerateKE1(pw)
	rec = postJSON(t, h, "/auth/login/start",
		map[string]string{"email": email, "ke1": b64(ke1.Serialize())}, "")
	var ls loginStartResp
	json.Unmarshal(rec.Body.Bytes(), &ls)
	ke2Bytes, _ := base64.StdEncoding.DecodeString(ls.KE2)
	ke2, _ := client2.Deserialize.KE2(ke2Bytes)
	ke3, _, _, _ := client2.GenerateKE3(ke2, nil, []byte("messenger-as"))
	rec = postJSON(t, h, "/auth/login/finish",
		map[string]string{"login_id": ls.LoginID, "ke3": b64(ke3.Serialize())}, "")
	var lf loginFinishResp
	json.Unmarshal(rec.Body.Bytes(), &lf)
	if lf.SessionToken == "" {
		t.Fatal("no session token")
	}
	return lf.SessionToken
}

func TestEnrollDeviceUploadAndConsumeKeyPackages(t *testing.T) {
	h := newFullServer(t)
	token := registerAndLogin(t, h, "frank@corp", "device-flow-pass")

	// enroll device with two initial key packages
	rec := postJSON(t, h, "/devices", map[string]any{
		"signing_public_key":   b64([]byte("ed25519-pub")),
		"label":                "frank-laptop",
		"initial_key_packages": []string{b64([]byte("kp-1")), b64([]byte("kp-2"))},
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll: %d %s", rec.Code, rec.Body)
	}
	var dr enrollDeviceResp
	json.Unmarshal(rec.Body.Bytes(), &dr)
	if dr.DeviceID == "" {
		t.Fatal("expected device_id")
	}

	// session is now bound to the device → /auth/session reports the device id
	rec2 := serve(h, httptestNewGet("/auth/session", token))
	var sr sessionResp
	json.Unmarshal(rec2.Body.Bytes(), &sr)
	if sr.DeviceID != dr.DeviceID {
		t.Fatalf("expected session bound to device %q, got %q", dr.DeviceID, sr.DeviceID)
	}

	// count shows 2 available
	rec3 := serve(h, httptestNewGet("/keypackages/count", token))
	var cr countResp
	json.Unmarshal(rec3.Body.Bytes(), &cr)
	if cr.Available != 2 {
		t.Fatalf("expected 2 available key packages, got %d", cr.Available)
	}

	// list devices shows the enrolled device
	rec4 := serve(h, httptestNewGet("/devices", token))
	if rec4.Code != http.StatusOK {
		t.Fatalf("list devices: %d", rec4.Code)
	}
	var list []deviceItem
	json.Unmarshal(rec4.Body.Bytes(), &list)
	if len(list) != 1 || list[0].DeviceID != dr.DeviceID {
		t.Fatalf("expected the enrolled device in the list, got %+v", list)
	}

	// another member consumes one of frank's key packages
	rec5 := serve(h, httptestNewGet("/keypackages/"+dr.DeviceID, token))
	if rec5.Code != http.StatusOK {
		t.Fatalf("consume: %d %s", rec5.Code, rec5.Body)
	}
	var kpr keyPackageResp
	json.Unmarshal(rec5.Body.Bytes(), &kpr)
	if kpr.KeyPackage == "" || kpr.IsLastResort {
		t.Fatalf("expected a one-time key package, got %+v", kpr)
	}
}
