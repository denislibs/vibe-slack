package hub

import (
	"testing"
	"time"
)

func TestHubDeliversToRegisteredDevice(t *testing.T) {
	h := New(4)
	ch, remove := h.Add("devA")
	defer remove()

	if !h.Deliver("devA", []byte("hi")) {
		t.Fatal("deliver to present device should succeed")
	}
	select {
	case p := <-ch:
		if string(p) != "hi" {
			t.Fatalf("got %q", p)
		}
	case <-time.After(time.Second):
		t.Fatal("expected payload on channel")
	}
}

func TestDeliverToAbsentDeviceReturnsFalse(t *testing.T) {
	h := New(4)
	if h.Deliver("ghost", []byte("x")) {
		t.Fatal("deliver to absent device should return false")
	}
}

func TestRemoveStopsDelivery(t *testing.T) {
	h := New(4)
	_, remove := h.Add("devA")
	remove()
	if h.Deliver("devA", []byte("x")) {
		t.Fatal("deliver after remove should return false")
	}
}

func TestDeliverDropsWhenBufferFull(t *testing.T) {
	h := New(1)
	_, remove := h.Add("devA")
	defer remove()
	if !h.Deliver("devA", []byte("1")) {
		t.Fatal("first deliver should fit the buffer")
	}
	if h.Deliver("devA", []byte("2")) {
		t.Fatal("deliver into a full buffer should return false (backpressure)")
	}
}
