package hub

import "sync"

// Hub is a node-local registry of device connections. Each device has a bounded
// send channel drained by that connection's write pump.
type Hub struct {
	mu      sync.RWMutex
	conns   map[string]chan []byte
	bufSize int
}

func New(bufSize int) *Hub {
	return &Hub{conns: make(map[string]chan []byte), bufSize: bufSize}
}

// Add registers a device and returns its receive channel plus a remove func.
// A second Add for the same device replaces the first (newest connection wins).
func (h *Hub) Add(deviceID string) (<-chan []byte, func()) {
	ch := make(chan []byte, h.bufSize)
	h.mu.Lock()
	if old, ok := h.conns[deviceID]; ok {
		close(old)
	}
	h.conns[deviceID] = ch
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if cur, ok := h.conns[deviceID]; ok && cur == ch {
			delete(h.conns, deviceID)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Deliver pushes payload to a device's channel without blocking. Returns false if
// the device is absent or its buffer is full (backpressure → caller closes the conn).
func (h *Hub) Deliver(deviceID string, payload []byte) bool {
	h.mu.RLock()
	ch, ok := h.conns[deviceID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	select {
	case ch <- payload:
		return true
	default:
		return false
	}
}
