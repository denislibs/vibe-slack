package hub

import (
	"sync"
	"testing"
)

// Hammer Deliver concurrently with Add/remove for the same device. Must never
// panic ("send on closed channel") and must be race-clean.
func TestDeliverNeverPanicsUnderConcurrentAddRemove(t *testing.T) {
	h := New(8)
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Deliverer: hammers Deliver.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				h.Deliver("devA", []byte("x"))
			}
		}
	}()

	// Churner: repeatedly registers + removes the same device (reconnect storm),
	// draining the channel so buffers don't wedge.
	for i := 0; i < 2000; i++ {
		ch, remove := h.Add("devA")
		done := make(chan struct{})
		go func() {
			for range ch {
			}
			close(done)
		}()
		h.Deliver("devA", []byte("y"))
		remove()
		<-done
	}
	close(stop)
	wg.Wait()
}
