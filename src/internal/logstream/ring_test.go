package logstream

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestRingWriteAndRead(t *testing.T) {
	r := NewRing(5)

	for i := 0; i < 3; i++ {
		r.Write(LogRecord{Msg: "test", Level: "info", Seq: 0})
	}

	if r.Len() != 3 {
		t.Fatalf("expected len 3, got %d", r.Len())
	}

	snap := r.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected snapshot len 3, got %d", len(snap))
	}

	// Verify seq numbers are assigned and monotonic
	for i, rec := range snap {
		if rec.Seq != int64(i+1) {
			t.Errorf("snap[%d].Seq = %d, want %d", i, rec.Seq, i+1)
		}
	}
}

func TestRingOverwrite(t *testing.T) {
	r := NewRing(3)

	for i := 0; i < 5; i++ {
		r.Write(LogRecord{Msg: "msg", Level: "info", Seq: 0})
	}

	if r.Len() != 3 {
		t.Fatalf("expected len 3 (cap), got %d", r.Len())
	}

	snap := r.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected snapshot len 3, got %d", len(snap))
	}

	// Oldest should be seq 3, newest seq 5
	if snap[0].Seq != 3 {
		t.Errorf("oldest seq = %d, want 3", snap[0].Seq)
	}
	if snap[2].Seq != 5 {
		t.Errorf("newest seq = %d, want 5", snap[2].Seq)
	}
}

func TestRingSince(t *testing.T) {
	r := NewRing(100)

	for i := 0; i < 10; i++ {
		r.Write(LogRecord{Msg: "msg", Level: "info", Seq: 0})
	}

	// Get records since seq 7
	since := r.Since(7)
	if len(since) != 3 {
		t.Fatalf("expected 3 records since seq 7, got %d", len(since))
	}
	if since[0].Seq != 8 {
		t.Errorf("first since seq = %d, want 8", since[0].Seq)
	}
}

func TestRingSinceEmpty(t *testing.T) {
	r := NewRing(10)
	since := r.Since(0)
	if since != nil {
		t.Fatalf("expected nil for empty ring, got %v", since)
	}
}

func TestRingSnapshotEmpty(t *testing.T) {
	r := NewRing(10)
	snap := r.Snapshot()
	if snap != nil {
		t.Fatalf("expected nil for empty ring, got %v", snap)
	}
}

func TestRingConcurrentReadWrite(t *testing.T) {
	r := NewRing(100)
	var wg sync.WaitGroup

	// Writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			r.Write(LogRecord{Msg: "concurrent", Level: "info", Seq: 0})
		}
	}()

	// Readers
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				snap := r.Snapshot()
				_ = snap
			}
		}()
	}

	wg.Wait()

	if r.Len() != 100 {
		t.Errorf("expected len 100 (cap), got %d", r.Len())
	}
}

func TestLogRecordJSON(t *testing.T) {
	rec := LogRecord{
		TS:        "2026-06-01T12:00:00.000Z",
		Level:     "info",
		Component: "tunnel",
		Source:    "daemon",
		Msg:       "connection established",
		Seq:       42,
		Fields:    map[string]any{"port": 443, "host": "example.com"},
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed LogRecord
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.Seq != 42 {
		t.Errorf("seq = %d, want 42", parsed.Seq)
	}
	if parsed.Component != "tunnel" {
		t.Errorf("component = %s, want tunnel", parsed.Component)
	}
}

func TestRingCap(t *testing.T) {
	r := NewRing(42)
	if r.Cap() != 42 {
		t.Errorf("cap = %d, want 42", r.Cap())
	}
}

func TestRingDefaultCap(t *testing.T) {
	r := NewRing(0)
	if r.Cap() != 200 {
		t.Errorf("default cap = %d, want 200", r.Cap())
	}
}

func TestRingSinceOverflow(t *testing.T) {
	r := NewRing(5)

	// Write 10 records (overflows cap=5)
	for i := 0; i < 10; i++ {
		r.Write(LogRecord{Msg: "msg", Level: "info", Seq: 0})
	}

	// Since(3) — should only return records still in ring (seq > 3)
	// Ring has seq 6-10 (last 5)
	since := r.Since(3)
	if len(since) != 5 {
		t.Fatalf("expected 5 records, got %d", len(since))
	}
	// All should have seq > 3
	for _, rec := range since {
		if rec.Seq <= 3 {
			t.Errorf("seq %d should be > 3", rec.Seq)
		}
	}
}
