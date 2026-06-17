package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	xopaque "github.com/bytemare/opaque"
	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/messenger/backend/internal/as"
	"github.com/messenger/backend/internal/opaque"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
)

type memUsers struct{ m map[string]*store.User }

func (u *memUsers) Create(_ context.Context, e, username string, r []byte) (*store.User, error) {
	if _, ok := u.m[e]; ok {
		return nil, store.ErrConflict
	}
	usr := &store.User{ID: "u-" + e, Email: e, Username: username, OpaqueRecord: r}
	u.m[e] = usr
	return usr, nil
}
func (u *memUsers) GetByEmail(_ context.Context, e string) (*store.User, error) {
	if usr, ok := u.m[e]; ok {
		return usr, nil
	}
	return nil, store.ErrNotFound
}

func newServer(t *testing.T) (http.Handler, *session.Manager) {
	t.Helper()
	cfg := xopaque.DefaultConfiguration()
	sk, pk := cfg.KeyGen()
	osrv, _ := opaque.NewServer(sk.Encode(), pk.Encode(), cfg.GenerateOPRFSeed(), []byte("messenger-as"))
	mr, _ := miniredis.Run()
	t.Cleanup(mr.Close)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	sess := session.NewManager(rdb, time.Hour)
	svc := as.NewService(osrv, &memUsers{m: map[string]*store.User{}}, sess, rdb)
	return NewRouter(svc, sess), sess
}

func postJSON(t *testing.T, h http.Handler, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// httptestNewGet builds a GET request with an optional bearer token.
func httptestNewGet(path, token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

// serve runs a request through the handler and returns the recorder.
func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestFullOpaqueFlowOverHTTP(t *testing.T) {
	h, _ := newServer(t)
	cfg := xopaque.DefaultConfiguration()
	client, _ := cfg.Client()
	pw := []byte("s3cret-passphrase")

	regReq, _ := client.RegistrationInit(pw)
	rec := postJSON(t, h, "/auth/register/start",
		map[string]string{"email": "carol@corp", "opaque_registration_request": b64(regReq.Serialize())}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("register/start: %d %s", rec.Code, rec.Body)
	}
	var rs registerStartResp
	json.Unmarshal(rec.Body.Bytes(), &rs)
	respBytes, _ := base64.StdEncoding.DecodeString(rs.OpaqueRegistrationResponse)
	regResp, _ := client.Deserialize.RegistrationResponse(respBytes)
	record, _, _ := client.RegistrationFinalize(regResp, nil, []byte("messenger-as"))

	rec = postJSON(t, h, "/auth/register/finish",
		map[string]string{"email": "carol@corp", "username": "carol", "opaque_registration_record": b64(record.Serialize())}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("register/finish: %d %s", rec.Code, rec.Body)
	}

	client2, _ := cfg.Client()
	ke1, _ := client2.GenerateKE1(pw)
	rec = postJSON(t, h, "/auth/login/start",
		map[string]string{"email": "carol@corp", "ke1": b64(ke1.Serialize())}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login/start: %d %s", rec.Code, rec.Body)
	}
	var ls loginStartResp
	json.Unmarshal(rec.Body.Bytes(), &ls)
	ke2Bytes, _ := base64.StdEncoding.DecodeString(ls.KE2)
	ke2, _ := client2.Deserialize.KE2(ke2Bytes)
	ke3, _, _, _ := client2.GenerateKE3(ke2, nil, []byte("messenger-as"))

	rec = postJSON(t, h, "/auth/login/finish",
		map[string]string{"login_id": ls.LoginID, "ke3": b64(ke3.Serialize())}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login/finish: %d %s", rec.Code, rec.Body)
	}
	var lf loginFinishResp
	json.Unmarshal(rec.Body.Bytes(), &lf)
	if lf.SessionToken == "" || !lf.DeviceEnrollRequired {
		t.Fatalf("unexpected login/finish body: %s", rec.Body)
	}

	// authenticated session check works with the issued token
	if serve(h, httptestNewGet("/auth/session", lf.SessionToken)).Code != http.StatusOK {
		t.Fatal("expected 200 from /auth/session with valid token")
	}
}

func TestSessionEndpointRequiresAuth(t *testing.T) {
	h, _ := newServer(t)
	if serve(h, httptestNewGet("/auth/session", "")).Code != http.StatusUnauthorized {
		t.Fatal("expected 401 without token")
	}
}
