package loguicapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

// FileAggregator tails a JSONL file and re-emits new lines as structured log records.
// Uses polling (stat for size change, read new bytes) — no fsnotify dependency.
// No double-write — the file is owned by another subsystem; we read-only.
type FileAggregator struct {
	paths     map[string]string // source → file path
	wg        sync.WaitGroup
	cancel    context.CancelFunc
}

// FileAggregatorConfig maps source names to file paths.
// e.g., {"lifecycle": "~/.unet/lifecycle-audit.jsonl", "api-audit": "~/.unet/audit.jsonl"}
type FileAggregatorConfig map[string]string

// NewFileAggregator creates a new file aggregator.
func NewFileAggregator(config FileAggregatorConfig) *FileAggregator {
	return &FileAggregator{
		paths: config,
	}
}

// Start begins tailing all configured files. Spawns a goroutine per file.
func (fa *FileAggregator) Start(ctx context.Context) {
	ctx, fa.cancel = context.WithCancel(ctx)

	for source, path := range fa.paths {
		fa.wg.Add(1)
		go fa.tailFile(ctx, source, path)
	}
}

// Stop cancels all tailing goroutines and waits (up to 2s).
func (fa *FileAggregator) Stop() {
	if fa.cancel != nil {
		fa.cancel()
	}

	done := make(chan struct{})
	go func() {
		fa.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		slog.Warn("file aggregator stop timeout")
	}
}

// tailFile polls a file for new content and re-emits lines as structured log.
func (fa *FileAggregator) tailFile(ctx context.Context, source, path string) {
	defer fa.wg.Done()

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		slog.Warn("file not found, skipping tail", "source", source, "path", path)
		return
	}

	// Open and seek to end
	f, err := os.Open(path)
	if err != nil {
		slog.Warn("file open failed", "source", source, "path", path, "error", err)
		return
	}
	defer f.Close()

	// Seek to end — only read new content
	if _, err := f.Seek(0, 2); err != nil {
		slog.Warn("file seek failed", "source", source, "path", path, "error", err)
		return
	}

	var lastSize int64
	if stat, err := f.Stat(); err == nil {
		lastSize = stat.Size()
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	reader := bufio.NewReader(f)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		// Check if file was rotated: compare on-disk file vs open fd
		if diskStat, err := os.Stat(path); err == nil {
			fdStat, fdErr := f.Stat()
			if fdErr != nil || !os.SameFile(diskStat, fdStat) {
				slog.Warn("file rotated, reopening", "source", source, "path", path)
				f.Close()
				f, err = os.Open(path)
				if err != nil {
					slog.Warn("file reopen failed", "source", source, "path", path, "error", err)
					return
				}
				reader = bufio.NewReader(f)
				lastSize = 0
				continue
			}

			if diskStat.Size() < lastSize {
				slog.Warn("file truncated, resetting offset", "source", source, "path", path)
				f.Seek(0, 0)
				reader.Reset(f)
				lastSize = 0
				continue
			}

			lastSize = diskStat.Size()
		}

		// Read new lines
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			line = trimNewline(line)
			if line == "" {
				continue
			}

			fa.emitLine(source, line)
		}
	}
}

// emitLine re-emits a JSONL line as a structured log record.
func (fa *FileAggregator) emitLine(source, line string) {
	// Try to parse as JSON to extract fields
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		// Not JSON — emit as plain message
		slog.Info(line, "source", source, "component", source)
		return
	}

	// Extract known fields
	msg := ""
	if m, ok := raw["msg"]; ok {
		msg = fmt.Sprintf("%v", m)
	}
	if msg == "" {
		if m, ok := raw["message"]; ok {
			msg = fmt.Sprintf("%v", m)
		}
	}
	if msg == "" {
		msg = line
	}

	attrs := []any{"source", source, "component", source}

	// Pass through fields
	for k, v := range raw {
		switch k {
		case "ts", "time", "timestamp", "msg", "message", "level":
			// Skip — we set our own
		default:
			attrs = append(attrs, k, v)
		}
	}

	slog.Info(msg, attrs...)
}

func trimNewline(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '\r' {
		s = s[:len(s)-1]
	}
	return s
}
