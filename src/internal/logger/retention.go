package logger

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RetentionSweeper deletes expired log archive files on a schedule.
// Active files (current day) are never deleted.
type RetentionSweeper struct {
	logDir       string
	retentionDays int
	mu           sync.Mutex
	stopCh       chan struct{}
	doneCh       chan struct{}
}

// NewRetentionSweeper creates a new sweeper.
func NewRetentionSweeper(logDir string, retentionDays int) *RetentionSweeper {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	return &RetentionSweeper{
		logDir:        logDir,
		retentionDays: retentionDays,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}
}

// Start runs the sweeper: once immediately, then daily.
// Blocks until Stop is called.
func (rs *RetentionSweeper) Start() {
	defer close(rs.doneCh)

	// Run once at start
	rs.sweep()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-rs.stopCh:
			return
		case <-ticker.C:
			rs.sweep()
		}
	}
}

// Stop signals the sweeper to stop and waits for it to finish.
func (rs *RetentionSweeper) Stop() {
	close(rs.stopCh)
	<-rs.doneCh
}

// SetRetentionDays updates the retention period. Takes effect on next sweep.
func (rs *RetentionSweeper) SetRetentionDays(days int) {
	rs.mu.Lock()
	rs.retentionDays = days
	rs.mu.Unlock()
}

// sweep scans the log directory and deletes expired archives.
func (rs *RetentionSweeper) sweep() {
	rs.mu.Lock()
	days := rs.retentionDays
	rs.mu.Unlock()

	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	today := time.Now().UTC().Format("2006-01-02")

	entries, err := os.ReadDir(rs.logDir)
	if err != nil {
		slog.Error("retention sweep: read dir failed", "dir", rs.logDir, "error", err)
		return
	}

	// Sort by name for deterministic processing
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	deleted := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Only process our log files
		if !strings.HasPrefix(name, "daemon-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}

		// Never delete current day's file (even if it's the active file)
		// e.g., daemon-2026-05-27.jsonl or daemon-2026-05-27.1.jsonl
		datePart := extractDateFromFilename(name)
		if datePart == today {
			continue
		}

		// Parse date and check against cutoff
		fileDate, err := time.Parse("2006-01-02", datePart)
		if err != nil {
			continue // can't parse, skip
		}

		if fileDate.Before(cutoff) {
			path := filepath.Join(rs.logDir, name)
			if err := os.Remove(path); err != nil {
				slog.Error("retention sweep: delete failed", "file", name, "error", err)
			} else {
				deleted++
			}
		}
	}

	if deleted > 0 {
		slog.Info("retention sweep completed", "deleted", deleted, "retention_days", days)
	}
}

// extractDateFromFilename extracts the YYYY-MM-DD date from a log filename.
// e.g., "daemon-2026-05-27.1.jsonl" → "2026-05-27"
func extractDateFromFilename(name string) string {
	// Remove prefix "daemon-"
	s := strings.TrimPrefix(name, "daemon-")
	// s is now "2026-05-27.jsonl" or "2026-05-27.1.jsonl"

	// Find the .jsonl suffix
	s = strings.TrimSuffix(s, ".jsonl")
	// s is now "2026-05-27" or "2026-05-27.1"

	// Split on first dot to separate date from rotation index
	idx := strings.Index(s, ".")
	if idx >= 0 {
		s = s[:idx]
	}
	return s
}
