package session

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiterBlocksAfterLimit(t *testing.T) {
	ctx := context.Background()
	rl := NewRateLimiter(newTestRedis(t), 3, time.Minute)

	for i := 0; i < 3; i++ {
		ok, err := rl.Allow(ctx, "ip:1.2.3.4")
		if err != nil || !ok {
			t.Fatalf("request %d should be allowed (ok=%v err=%v)", i, ok, err)
		}
	}
	ok, err := rl.Allow(ctx, "ip:1.2.3.4")
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if ok {
		t.Fatal("4th request should be blocked")
	}
	ok, _ = rl.Allow(ctx, "ip:5.6.7.8")
	if !ok {
		t.Fatal("different key should be allowed")
	}
}
