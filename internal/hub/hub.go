package hub

import "sync"

// Hub fans out byte messages to all registered subscribers.
type Hub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

// New returns an initialised Hub.
func New() *Hub {
	return &Hub{clients: make(map[chan []byte]struct{})}
}

// Subscribe registers a new subscriber and returns a receive channel and an
// unsubscribe function. The caller must call unsubscribe when done.
func (h *Hub) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.clients, ch)
			h.mu.Unlock()
			close(ch)
		})
	}
}

// Broadcast sends msg to every subscriber. Slow or disconnected subscribers
// that have a full buffer are skipped (non-blocking send).
func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
			// subscriber too slow — skip this tick
		}
	}
}
