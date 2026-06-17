package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/messenger/backend/internal/delivery"
	"github.com/messenger/backend/internal/fanout"
	"github.com/messenger/backend/internal/hub"
	"github.com/messenger/backend/internal/platform/postgres"
	"github.com/messenger/backend/internal/platform/redis"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

type testEnv struct {
	server *httptest.Server
	sess   *session.Manager
	roster *store.RosterRepo
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	pgctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("as"), tcpostgres.WithUsername("as"), tcpostgres.WithPassword("as"),
		tcpostgres.BasicWaitStrategies())
	if err != nil {
		t.Fatalf("pg: %v", err)
	}
	t.Cleanup(func() { _ = pgctr.Terminate(ctx) })
	dsn, _ := pgctr.ConnectionString(ctx, "sslmode=disable")
	pool, _ := postgres.Connect(ctx, dsn)
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rctr, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("redis: %v", err)
	}
	t.Cleanup(func() { _ = rctr.Terminate(ctx) })
	ruri, _ := rctr.ConnectionString(ctx)
	rdb, _ := redis.NewClient(ctx, ruri)

	sess := session.NewManager(rdb, time.Hour)
	roster := store.NewRosterRepo(pool)
	h := hub.New(64)
	f := fanout.New(rdb, func(deviceID string, payload []byte) { h.Deliver(deviceID, payload) })
	t.Cleanup(func() { _ = f.Close() })
	svc := delivery.NewService(store.NewMessageRepo(pool), roster, store.NewCursorRepo(pool), f)

	gw := NewGateway(sess, svc, h, f, "node-test")
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", gw.Handle)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &testEnv{server: srv, sess: sess, roster: roster}
}

func dial(t *testing.T, env *testEnv, token string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(env.server.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

func writeJSON(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	b, _ := json.Marshal(v)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readUntilType(t *testing.T, c *websocket.Conn, typ string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, b, err := c.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m map[string]any
		json.Unmarshal(b, &m)
		if m["type"] == typ {
			return m
		}
	}
	t.Fatalf("did not receive frame of type %q", typ)
	return nil
}

func TestSendThenOtherDeviceReceives(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	alice, _ := env.sess.Issue(ctx, "user-1", "aaaaaaaa-1111-1111-1111-111111111111")
	bob, _ := env.sess.Issue(ctx, "user-2", "bbbbbbbb-1111-1111-1111-111111111111")
	env.roster.AddMember(ctx, "g1", "aaaaaaaa-1111-1111-1111-111111111111", 0)
	env.roster.AddMember(ctx, "g1", "bbbbbbbb-1111-1111-1111-111111111111", 0)

	ca := dial(t, env, alice)
	defer ca.Close(websocket.StatusNormalClosure, "")
	cb := dial(t, env, bob)
	defer cb.Close(websocket.StatusNormalClosure, "")
	time.Sleep(200 * time.Millisecond)

	writeJSON(t, ca, map[string]any{
		"type": "send", "client_msg_id": "c1", "group_id": "g1",
		"content_type": "application", "ciphertext": []byte("hello-bob"),
	})

	if got := readUntilType(t, ca, "sent"); int64(got["seq"].(float64)) != 1 {
		t.Fatalf("expected sent seq 1, got %v", got["seq"])
	}
	msg := readUntilType(t, cb, "message")
	if msg["group_id"] != "g1" || int64(msg["seq"].(float64)) != 1 {
		t.Fatalf("bob got wrong message: %v", msg)
	}
}

func TestReconnectSameDeviceStillReceives(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	dev := "aaaaaaaa-1111-1111-1111-111111111111"
	other := "bbbbbbbb-1111-1111-1111-111111111111"
	tok, _ := env.sess.Issue(ctx, "u1", dev)
	otherTok, _ := env.sess.Issue(ctx, "u2", other)
	env.roster.AddMember(ctx, "g1", dev, 0)
	env.roster.AddMember(ctx, "g1", other, 0)

	// First connection for dev, then a SECOND connection for the same dev (reconnect).
	c1 := dial(t, env, tok)
	time.Sleep(100 * time.Millisecond)
	c2 := dial(t, env, tok)
	defer c2.Close(websocket.StatusNormalClosure, "")
	time.Sleep(150 * time.Millisecond)
	_ = c1 // old conn; its write pump exits when hub replaces its channel

	// `other` sends to the group; the live dev connection (c2) must receive it,
	// proving the reconnect did not unsubscribe dev's channel.
	co := dial(t, env, otherTok)
	defer co.Close(websocket.StatusNormalClosure, "")
	time.Sleep(100 * time.Millisecond)
	writeJSON(t, co, map[string]any{
		"type": "send", "client_msg_id": "rc1", "group_id": "g1",
		"content_type": "application", "ciphertext": []byte("after-reconnect"),
	})
	msg := readUntilType(t, c2, "message")
	if msg["group_id"] != "g1" {
		t.Fatalf("reconnected device did not receive fan-out: %v", msg)
	}
}

func TestUnauthenticatedDialRejected(t *testing.T) {
	env := newEnv(t)
	url := "ws" + strings.TrimPrefix(env.server.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, err := websocket.Dial(ctx, url, nil); err == nil {
		t.Fatal("expected dial to fail without a valid session token")
	}
}
