package fanout

import (
	"context"
	"strings"
	"sync"

	goredis "github.com/redis/go-redis/v9"
)

const channelPrefix = "dev:"

// Fanout bridges Redis pub/sub and the local node: Publish sends frames to a device's
// channel; Subscribe registers this node for a locally-connected device's channel;
// deliver(deviceID, payload) is invoked for every message on a subscribed channel.
type Fanout struct {
	rdb     *goredis.Client
	ps      *goredis.PubSub
	deliver func(deviceID string, payload []byte)

	mu     sync.Mutex
	subs   map[string]int // deviceID -> local subscriber count (refcount)
	closed bool
	done   chan struct{}
}

// New creates a Fanout and starts its receive loop. deliver runs on one goroutine.
func New(rdb *goredis.Client, deliver func(deviceID string, payload []byte)) *Fanout {
	f := &Fanout{
		rdb:     rdb,
		ps:      rdb.Subscribe(context.Background()),
		deliver: deliver,
		subs:    map[string]int{},
		done:    make(chan struct{}),
	}
	go f.loop()
	return f
}

func (f *Fanout) loop() {
	for msg := range f.ps.Channel() {
		deviceID := strings.TrimPrefix(msg.Channel, channelPrefix)
		f.deliver(deviceID, []byte(msg.Payload))
	}
	close(f.done)
}

// Subscribe registers a local subscriber for a device's channel. Subscriptions are
// refcounted: the Redis channel is subscribed on the first local connection and
// unsubscribed only when the last one goes away. This prevents a reconnecting
// device's stale connection (whose deferred Unsubscribe fires later) from tearing
// down the channel the new live connection still needs.
func (f *Fanout) Subscribe(ctx context.Context, deviceID string) error {
	f.mu.Lock()
	n := f.subs[deviceID]
	f.subs[deviceID] = n + 1
	f.mu.Unlock()
	if n == 0 {
		return f.ps.Subscribe(ctx, channelPrefix+deviceID)
	}
	return nil
}

func (f *Fanout) Unsubscribe(ctx context.Context, deviceID string) error {
	f.mu.Lock()
	n := f.subs[deviceID]
	if n <= 1 {
		delete(f.subs, deviceID)
	} else {
		f.subs[deviceID] = n - 1
	}
	f.mu.Unlock()
	if n <= 1 {
		return f.ps.Unsubscribe(ctx, channelPrefix+deviceID)
	}
	return nil
}

// Publish sends payload to a device's channel (delivered on whatever node subscribes).
func (f *Fanout) Publish(ctx context.Context, deviceID string, payload []byte) error {
	return f.rdb.Publish(ctx, channelPrefix+deviceID, payload).Err()
}

func (f *Fanout) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	return f.ps.Close()
}
