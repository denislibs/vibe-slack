package ws

import (
	"context"
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

func TestCrossNodeDelivery(t *testing.T) {
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

	// Each node has its OWN hub + fanout (separate Redis subscriptions),
	// sharing the same Postgres pool and Redis server.
	mkNode := func(nodeID string) *httptest.Server {
		h := hub.New(64)
		f := fanout.New(rdb, func(deviceID string, payload []byte) { h.Deliver(deviceID, payload) })
		t.Cleanup(func() { _ = f.Close() })
		svc := delivery.NewService(store.NewMessageRepo(pool), roster, store.NewCursorRepo(pool), f)
		gw := NewGateway(sess, svc, h, f, nodeID)
		mux := http.NewServeMux()
		mux.HandleFunc("/ws", gw.Handle)
		s := httptest.NewServer(mux)
		t.Cleanup(s.Close)
		return s
	}
	node1 := mkNode("n1")
	node2 := mkNode("n2")

	alice := "aaaaaaaa-1111-1111-1111-111111111111"
	bob := "bbbbbbbb-1111-1111-1111-111111111111"
	aliceTok, _ := sess.Issue(ctx, "u1", alice)
	bobTok, _ := sess.Issue(ctx, "u2", bob)
	roster.AddMember(ctx, "g1", alice, 0)
	roster.AddMember(ctx, "g1", bob, 0)

	dialNode := func(s *httptest.Server, token string) *websocket.Conn {
		url := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"
		dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		c, _, err := websocket.Dial(dctx, url, &websocket.DialOptions{
			HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
		})
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		return c
	}

	ca := dialNode(node1, aliceTok) // Alice on node1
	defer ca.Close(websocket.StatusNormalClosure, "")
	cb := dialNode(node2, bobTok) // Bob on node2
	defer cb.Close(websocket.StatusNormalClosure, "")
	time.Sleep(300 * time.Millisecond) // let subscriptions register on both nodes

	writeJSON(t, ca, map[string]any{
		"type": "send", "client_msg_id": "c1", "group_id": "g1",
		"content_type": "application", "ciphertext": []byte("cross-node"),
	})

	// Bob, on a DIFFERENT node, must receive via Redis pub/sub.
	msg := readUntilType(t, cb, "message")
	if msg["group_id"] != "g1" {
		t.Fatalf("cross-node delivery failed: %v", msg)
	}
}
