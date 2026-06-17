package hub

import "sync"

// conn holds a device's bounded send channel guarded by a per-connection mutex.
// The mutex makes deliver (send) and close mutually exclusive so a send can never
// race a close and panic with "send on closed channel".
type conn struct {
	ch     chan []byte
	mu     sync.Mutex
	closed bool
}

func (c *conn) deliver(payload []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	select {
	case c.ch <- payload:
		return true
	default:
		return false
	}
}

func (c *conn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.ch)
	}
}

// Hub is a node-local registry of device connections. Each device has a bounded
// send channel drained by that connection's write pump.
type Hub struct {
	mu      sync.RWMutex
	conns   map[string]*conn
	bufSize int
}

func New(bufSize int) *Hub {
	return &Hub{conns: make(map[string]*conn), bufSize: bufSize}
}

// Add registers a device and returns its receive channel plus a remove func.
// A second Add for the same device replaces the first (newest connection wins).
func (h *Hub) Add(deviceID string) (<-chan []byte, func()) {
	c := &conn{ch: make(chan []byte, h.bufSize)}
	h.mu.Lock()
	if old, ok := h.conns[deviceID]; ok {
		old.close()
	}
	h.conns[deviceID] = c
	h.mu.Unlock()
	return c.ch, func() {
		h.mu.Lock()
		if cur, ok := h.conns[deviceID]; ok && cur == c {
			delete(h.conns, deviceID)
		}
		h.mu.Unlock()
		c.close()
	}
}

// Deliver pushes payload to a device's channel without blocking. Returns false if
// the device is absent, removed, or its buffer is full (backpressure → caller
// closes the conn). The per-conn mutex guarantees the send never hits a closed channel.
func (h *Hub) Deliver(deviceID string, payload []byte) bool {
	h.mu.RLock()
	c, ok := h.conns[deviceID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	return c.deliver(payload)
}
