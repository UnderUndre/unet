package logstream

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
)

// Subscriber represents an SSE client connected to the log stream.
type Subscriber struct {
	id     int64
	ch     chan []byte // outbound SSE events (JSON-encoded)
	filters SubFilter  // optional level/component filters
	closed  atomic.Bool
}

// SubFilter defines what events a subscriber wants to receive.
type SubFilter struct {
	Levels     map[string]bool // if non-nil, only these levels
	Components map[string]bool // if non-nil, only these components
	Sources    map[string]bool // if non-nil, only these sources
}

// NewSubscriber creates a new subscriber with a buffered channel.
func NewSubscriber(bufferSize int, filters SubFilter) *Subscriber {
	if bufferSize <= 0 {
		bufferSize = 1000
	}
	return &Subscriber{
		ch:     make(chan []byte, bufferSize),
		filters: filters,
	}
}

// ID returns the subscriber's unique ID.
func (s *Subscriber) ID() int64 { return s.id }

// Channel returns the subscriber's outbound event channel.
func (s *Subscriber) Channel() <-chan []byte { return s.ch }

// Close marks the subscriber as closed and drains the channel.
func (s *Subscriber) Close() {
	if s.closed.CompareAndSwap(false, true) {
		close(s.ch)
	}
}

// IsClosed returns whether the subscriber has been closed.
func (s *Subscriber) IsClosed() bool { return s.closed.Load() }

// Matches checks if a log record passes the subscriber's filters.
func (s *Subscriber) Matches(rec LogRecord) bool {
	if len(s.filters.Levels) > 0 && !s.filters.Levels[rec.Level] {
		return false
	}
	if len(s.filters.Components) > 0 && !s.filters.Components[rec.Component] {
		return false
	}
	if len(s.filters.Sources) > 0 && !s.filters.Sources[rec.Source] {
		return false
	}
	return true
}

// send enqueues an event. Returns false if the subscriber is closed or full.
func (s *Subscriber) send(data []byte) bool {
	if s.closed.Load() {
		return false
	}
	select {
	case s.ch <- data:
		return true
	default:
		// Buffer full — drop oldest silently
		select {
		case <-s.ch: // discard one
		default:
		}
		select {
		case s.ch <- data:
			return true
		default:
			return false // still full, drop
		}
	}
}

// Hub manages SSE subscribers and fan-out from the ring buffer.
// Single writer (slog handler), multiple readers (SSE goroutines).
type Hub struct {
	ring        *Ring
	mu          sync.RWMutex
	subscribers map[int64]*Subscriber
	nextID      atomic.Int64
	stopCh      chan struct{}

	// Optional: subscribe to new events (for real-time push)
	notifyCh chan int64 // publishes last seq
}

// NewHub creates a new SSE hub around a ring buffer.
func NewHub(ring *Ring) *Hub {
	return &Hub{
		ring:        ring,
		subscribers: make(map[int64]*Subscriber),
		stopCh:      make(chan struct{}),
		notifyCh:    make(chan int64, 256),
	}
}

// Subscribe registers a new SSE subscriber with the given filters.
// Returns the subscriber (for reading events) and the last seq number
// (for Last-Event-ID replay).
func (h *Hub) Subscribe(filter SubFilter) (*Subscriber, int64) {
	sub := NewSubscriber(1000, filter)
	sub.id = h.nextID.Add(1)

	h.mu.Lock()
	h.subscribers[sub.id] = sub
	h.mu.Unlock()

	slog.Debug("SSE subscriber added", "subscriber_id", sub.id)
	return sub, h.ring.NextSeq() - 1
}

// Unsubscribe removes a subscriber and closes its channel.
func (h *Hub) Unsubscribe(id int64) {
	h.mu.Lock()
	sub, ok := h.subscribers[id]
	if ok {
		delete(h.subscribers, id)
	}
	h.mu.Unlock()

	if ok {
		sub.Close()
		slog.Debug("SSE subscriber removed", "subscriber_id", id)
	}
}

// Subscribers returns the current subscriber count.
func (h *Hub) Subscribers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

// Notify is called by the slog handler after writing to the ring buffer.
// Triggers fan-out to all subscribers.
func (h *Hub) Notify(seq int64) {
	select {
	case h.notifyCh <- seq:
	default:
		// Notification channel full — Run will catch up on next tick
	}
}

// Run starts the hub's event loop. Blocks until Stop is called.
// Reads notifications and fans out to subscribers.
func (h *Hub) Run() {
	for {
		select {
		case <-h.stopCh:
			return
		case seq := <-h.notifyCh:
			h.fanout(seq)
		}
	}
}

// fanout sends new records to all matching subscribers.
func (h *Hub) fanout(afterSeq int64) {
	records := h.ring.Since(afterSeq - 1) // include the record at afterSeq
	if len(records) == 0 {
		return
	}

	// Pre-marshal records
	type sseEvent struct {
		Event string     `json:"event"`
		Data  LogRecord  `json:"data"`
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, rec := range records {
		// Check if any subscriber wants this record
		event := sseEvent{Event: "log", Data: rec}
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}

		for _, sub := range h.subscribers {
			if sub.Matches(rec) {
				sub.send(data)
			}
		}
	}
}

// Replay sends all records since afterSeq to a specific subscriber.
// Used for Last-Event-ID reconnection.
func (h *Hub) Replay(sub *Subscriber, afterSeq int64) {
	records := h.ring.Since(afterSeq)
	for _, rec := range records {
		if sub.Matches(rec) {
			event := map[string]any{"event": "log", "data": rec}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			sub.send(data)
		}
	}
}

// Stop signals the hub to stop and closes all subscriber channels.
func (h *Hub) Stop() {
	close(h.stopCh)

	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subscribers {
		sub.Close()
	}
	h.subscribers = make(map[int64]*Subscriber)
}
