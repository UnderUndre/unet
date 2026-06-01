package daemon

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// LifecycleEvent represents a structured event for the events endpoint.
type LifecycleEvent struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
	Details   string `json:"details,omitempty"`
}

// EventRing is a bounded ring buffer for lifecycle events (max 1000).
type EventRing struct {
	mu     sync.RWMutex
	events []LifecycleEvent
	max    int
	seq    int
}

// NewEventRing creates a ring buffer with the given capacity.
func NewEventRing(max int) *EventRing {
	return &EventRing{
		events: make([]LifecycleEvent, 0, max),
		max:    max,
	}
}

// Add appends an event, evicting oldest if at capacity.
func (r *EventRing) Add(eventType, message, details string) LifecycleEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.seq++
	evt := LifecycleEvent{
		ID:        itoa3(r.seq),
		Type:      eventType,
		Timestamp: time.Now().Format(time.RFC3339),
		Message:   message,
		Details:   details,
	}

	if len(r.events) >= r.max {
		r.events = r.events[1:]
	}
	r.events = append(r.events, evt)
	return evt
}

// List returns events with optional pagination.
// after = cursor (event ID), limit = max events to return.
func (r *EventRing) List(after string, limit int) []LifecycleEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	// If after is specified, find the event and return everything after it.
	start := 0
	if after != "" {
		for i, evt := range r.events {
			if evt.ID == after {
				start = i + 1
				break
			}
		}
	}

	result := make([]LifecycleEvent, 0, limit)
	for i := start; i < len(r.events) && len(result) < limit; i++ {
		result = append(result, r.events[i])
	}
	return result
}

// EventHandler serves GET /api/v1/events.
type EventHandler struct {
	ring   *EventRing
	server *Server
}

// NewEventHandler creates a new EventHandler with a 1000-entry ring buffer.
func NewEventHandler(srv *Server) *EventHandler {
	return &EventHandler{
		ring:   NewEventRing(1000),
		server: srv,
	}
}

// RegisterRoutes registers the events endpoint.
func (h *EventHandler) RegisterRoutes() {
	h.server.HandleFunc("GET /api/v1/events", h.handleEvents)
}

// Ring returns the underlying EventRing for adding events.
func (h *EventHandler) Ring() *EventRing {
	return h.ring
}

func (h *EventHandler) handleEvents(w http.ResponseWriter, r *http.Request) {
	after := r.URL.Query().Get("after")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := atoi2(l); err == nil && v > 0 {
			limit = v
		}
	}

	events := h.ring.List(after, limit)
	if events == nil {
		events = []LifecycleEvent{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func itoa3(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func atoi2(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, nil
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
