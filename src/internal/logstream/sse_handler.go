package logstream

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SSEHandler implements http.Handler for SSE log streaming at /api/v1/logs/stream.
type SSEHandler struct {
	hub *Hub
}

// NewSSEHandler creates a new SSE handler.
func NewSSEHandler(hub *Hub) *SSEHandler {
	return &SSEHandler{hub: hub}
}

// ServeHTTP handles SSE connections.
// Protocol per contracts/sse-protocol.md:
//   - GET /api/v1/logs/stream
//   - Optional query params: level, component, source (comma-separated filters)
//   - Optional header: Last-Event-ID for reconnection replay
//   - Response: text/event-stream with events: log, metrics, system
func (h *SSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check Accept header
	accept := r.Header.Get("Accept")
	if accept != "" && !strings.Contains(accept, "text/event-stream") && !strings.Contains(accept, "*/*") {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}

	// Flushable response writer
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Parse filters from query params
	filter := SubFilter{
		Levels:     parseFilterSet(r.URL.Query().Get("level")),
		Components: parseFilterSet(r.URL.Query().Get("component")),
		Sources:    parseFilterSet(r.URL.Query().Get("source")),
	}

	// Subscribe
	sub, _ := h.hub.Subscribe(filter)
	defer h.hub.Unsubscribe(sub.ID())

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Last-Event-ID")

	// Replay from Last-Event-ID if provided
	if lastEventID := r.Header.Get("Last-Event-ID"); lastEventID != "" {
		if afterSeq, err := strconv.ParseInt(lastEventID, 10, 64); err == nil {
			h.hub.Replay(sub, afterSeq)
		}
	}

	// Send initial system event
	h.writeSSE(w, flusher, "system", map[string]any{
		"type":         "connected",
		"subscriber":   sub.ID(),
		"buffer_size":  h.hub.ring.Cap(),
	})

	// Heartbeat ticker
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	// Event loop
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			slog.Debug("SSE client disconnected", "subscriber_id", sub.ID())
			return

		case data, ok := <-sub.Channel():
			if !ok {
				return
			}
			// Data is already JSON-encoded with event type
			var event map[string]any
			if err := json.Unmarshal(data, &event); err != nil {
				continue
			}
			eventType := "log"
			if e, ok := event["event"].(string); ok {
				eventType = e
			}
			h.writeSSE(w, flusher, eventType, event["data"])

		case <-heartbeat.C:
			h.writeSSE(w, flusher, "system", map[string]any{
				"type": "heartbeat",
				"ts":   time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
			})
		}
	}
}

// writeSSE writes a single SSE event to the response writer.
func (h *SSEHandler) writeSSE(w http.ResponseWriter, flusher http.Flusher, eventType string, data any) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return
	}

	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", dataJSON)
	flusher.Flush()
}

// parseFilterSet parses a comma-separated filter string into a set.
// Returns nil if empty (no filter).
func parseFilterSet(s string) map[string]bool {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	set := make(map[string]bool, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			set[strings.ToLower(p)] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}
