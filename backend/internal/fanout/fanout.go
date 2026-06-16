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
	closed bool
	done   chan struct{}
}

// New creates a Fanout and starts its receive loop. deliver runs on one goroutine.
func New(rdb *goredis.Client, deliver func(deviceID string, payload []byte)) *Fanout {
	f := &Fanout{
		rdb:     rdb,
		ps:      rdb.Subscribe(context.Background()),
		deliver: deliver,
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

func (f *Fanout) Subscribe(ctx context.Context, deviceID string) error {
	return f.ps.Subscribe(ctx, channelPrefix+deviceID)
}

func (f *Fanout) Unsubscribe(ctx context.Context, deviceID string) error {
	return f.ps.Unsubscribe(ctx, channelPrefix+deviceID)
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
