package fanout

import (
	"context"
	"testing"
	"time"

	"github.com/messenger/backend/internal/platform/redis"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func startRedis(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("redis container: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	uri, err := ctr.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("conn string: %v", err)
	}
	return uri
}

func TestPublishReachesSubscribedDevice(t *testing.T) {
	ctx := context.Background()
	uri := startRedis(t)
	rdb, err := redis.NewClient(ctx, uri)
	if err != nil {
		t.Fatalf("redis client: %v", err)
	}

	got := make(chan []byte, 1)
	f := New(rdb, func(deviceID string, payload []byte) {
		if deviceID == "devX" {
			got <- payload
		}
	})
	defer f.Close()

	if err := f.Subscribe(ctx, "devX"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if err := f.Publish(ctx, "devX", []byte("hello")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case p := <-got:
		if string(p) != "hello" {
			t.Fatalf("got %q", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for published payload")
	}

	f.Unsubscribe(ctx, "devX")
	time.Sleep(100 * time.Millisecond)
	f.Publish(ctx, "devX", []byte("after"))
	select {
	case <-got:
		t.Fatal("should not receive after unsubscribe")
	case <-time.After(500 * time.Millisecond):
	}
}

func TestRefcountedSubscribeKeepsChannelUntilLastUnsub(t *testing.T) {
	ctx := context.Background()
	rdb, err := redis.NewClient(ctx, startRedis(t))
	if err != nil {
		t.Fatalf("redis: %v", err)
	}
	got := make(chan []byte, 4)
	f := New(rdb, func(deviceID string, payload []byte) {
		if deviceID == "devR" {
			got <- payload
		}
	})
	defer f.Close()

	f.Subscribe(ctx, "devR")
	f.Subscribe(ctx, "devR") // two local conns for same device
	time.Sleep(100 * time.Millisecond)
	f.Unsubscribe(ctx, "devR") // one leaves; channel must remain
	time.Sleep(100 * time.Millisecond)

	f.Publish(ctx, "devR", []byte("still-here"))
	select {
	case p := <-got:
		if string(p) != "still-here" {
			t.Fatalf("got %q", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel was unsubscribed too early (refcount bug)")
	}

	f.Unsubscribe(ctx, "devR") // last leaves; now unsubscribed
	time.Sleep(100 * time.Millisecond)
	f.Publish(ctx, "devR", []byte("gone"))
	select {
	case <-got:
		t.Fatal("should not receive after last unsubscribe")
	case <-time.After(500 * time.Millisecond):
	}
}
