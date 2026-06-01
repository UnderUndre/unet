// Package logstream provides in-memory ring buffer and SSE fan-out for structured log streaming.
package logstream

import (
	"sync/atomic"
)

// LogRecord represents a single structured log entry in the ring buffer.
// Matches contracts/log-record.schema.json.
type LogRecord struct {
	TS        string         `json:"ts"`
	Level     string         `json:"level"`
	Component string         `json:"component"`
	Source    string         `json:"source"`
	Msg       string         `json:"msg"`
	Seq       int64          `json:"seq"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// Ring is a bounded, lock-free ring buffer for LogRecord entries.
// Single-writer (slog handler mutex), multi-reader (SSE subscribers).
// Write is O(1) with zero allocations on the write path.
type Ring struct {
	records []LogRecord
	cap     uint64
	writeIdx atomic.Uint64
	total    atomic.Int64 // total entries ever written
	seq      atomic.Int64 // monotonic sequence counter
}

// NewRing creates a ring buffer with the given capacity.
func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		capacity = 200
	}
	return &Ring{
		records: make([]LogRecord, capacity),
		cap:     uint64(capacity),
	}
}

// Write appends a record to the ring buffer. Returns the assigned sequence number.
// NOT goroutine-safe for concurrent writes — caller must hold external mutex.
// Goroutine-safe for concurrent reads via Snapshot/Since.
func (r *Ring) Write(rec LogRecord) int64 {
	seq := r.seq.Add(1)
	rec.Seq = seq

	idx := r.writeIdx.Add(1) - 1 // pre-increment, get slot
	r.records[idx%r.cap] = rec
	r.total.Add(1)

	return seq
}

// NextSeq returns the next sequence number that will be assigned,
// without incrementing. Useful for pre-assigning seq before Write.
func (r *Ring) NextSeq() int64 {
	return r.seq.Load() + 1
}

// Snapshot returns a copy of all current entries in chronological order.
// Safe for concurrent use.
func (r *Ring) Snapshot() []LogRecord {
	total := r.total.Load()
	if total == 0 {
		return nil
	}
	cap := r.cap
	count := uint64(total)
	if count > cap {
		count = cap
	}

	result := make([]LogRecord, count)
	start := count // oldest entry index in ring
	for i := uint64(0); i < count; i++ {
		idx := (uint64(total) - count + i)
		result[i] = r.records[idx%cap]
	}
	_ = start
	return result
}

// Since returns all entries with seq > afterSeq, in chronological order.
// Used for SSE Last-Event-ID replay. Returns empty slice if nothing new.
func (r *Ring) Since(afterSeq int64) []LogRecord {
	total := r.total.Load()
	if total == 0 {
		return nil
	}
	cap := r.cap
	count := uint64(total)

	// Find starting position
	var startIdx uint64
	if count > cap {
		startIdx = count - cap // oldest available
	} else {
		startIdx = 0
	}

	var result []LogRecord
	for i := startIdx; i < count; i++ {
		rec := r.records[i%cap]
		if rec.Seq > afterSeq {
			result = append(result, rec)
		}
	}
	return result
}

// Len returns the number of entries currently in the ring.
func (r *Ring) Len() int {
	total := r.total.Load()
	if total <= 0 {
		return 0
	}
	cap := int64(r.cap)
	if total > cap {
		return int(cap)
	}
	return int(total)
}

// Cap returns the ring capacity.
func (r *Ring) Cap() int {
	return int(r.cap)
}
