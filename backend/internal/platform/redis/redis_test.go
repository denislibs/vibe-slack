package redis

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestNewClientPingAndSetGet(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	ctx := context.Background()
	client, err := NewClient(ctx, "redis://"+mr.Addr()+"/0")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Set(ctx, "k", "v", time.Minute).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := client.Get(ctx, "k").Result()
	if err != nil || got != "v" {
		t.Fatalf("get: got %q err %v", got, err)
	}
}
