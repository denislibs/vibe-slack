package redis

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// Client is the shared Redis client type used across the service.
type Client = redis.Client

// NewClient parses a redis:// URL, connects, and verifies with PING.
func NewClient(ctx context.Context, url string) (*Client, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	c := redis.NewClient(opt)
	if err := c.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return c, nil
}
