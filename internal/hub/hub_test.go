package hub_test

import (
	"sync"
	"testing"
	"time"

	"github.com/ko5tas/t212/internal/hub"
)

func TestHub_SubscribeAndBroadcast(t *testing.T) {
	h := hub.New()

	ch, unsub := h.Subscribe()
	defer unsub()

	msg := []byte(`{"test":true}`)
	go h.Broadcast(msg)

	select {
	case got := <-ch:
		if string(got) != string(msg) {
			t.Errorf("got %q, want %q", got, msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for broadcast")
	}
}

func TestHub_Unsubscribe(t *testing.T) {
	h := hub.New()

	ch, unsub := h.Subscribe()
	unsub()

	// Broadcasting after unsubscribe should not block or panic.
	done := make(chan struct{})
	go func() {
		h.Broadcast([]byte("hello"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Broadcast blocked after unsubscribe")
	}

	// Channel should be closed after unsubscribe.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed")
		}
	default:
		t.Error("expected channel to be closed (not just empty)")
	}
}

func TestHub_MultipleSubscribers(t *testing.T) {
	h := hub.New()
	const n = 5

	channels := make([]<-chan []byte, n)
	unsubs := make([]func(), n)
	for i := range n {
		channels[i], unsubs[i] = h.Subscribe()
		defer unsubs[i]()
	}

	msg := []byte("broadcast")
	go h.Broadcast(msg)

	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(ch <-chan []byte) {
			defer wg.Done()
			select {
			case got := <-ch:
				if string(got) != string(msg) {
					t.Errorf("got %q, want %q", got, msg)
				}
			case <-time.After(time.Second):
				t.Error("timeout waiting for message")
			}
		}(channels[i])
	}
	wg.Wait()
}
