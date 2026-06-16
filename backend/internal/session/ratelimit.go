package session

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// RateLimiter is a fixed-window counter in Redis.
type RateLimiter struct {
	rdb    *goredis.Client
	limit  int64
	window time.Duration
}

func NewRateLimiter(rdb *goredis.Client, limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{rdb: rdb, limit: int64(limit), window: window}
}

// Allow increments the counter for key and returns false once it exceeds limit
// within the window.
func (rl *RateLimiter) Allow(ctx context.Context, key string) (bool, error) {
	k := "ratelimit:" + key
	n, err := rl.rdb.Incr(ctx, k).Result()
	if err != nil {
		return false, err
	}
	if n == 1 {
		rl.rdb.Expire(ctx, k, rl.window)
	}
	return n <= rl.limit, nil
}
