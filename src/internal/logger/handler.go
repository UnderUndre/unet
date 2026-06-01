package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/underundre/unet/internal/logstream"
)

// jsonHandler implements slog.Handler with JSONL output, dual-write (file + ring buffer),
// secret redaction, and per-component level filtering.
type jsonHandler struct {
	mu       sync.Mutex // protects writer swap and file rotation
	writer   io.Writer  // current file writer (lumberjack or custom rotator)
	ring     *logstream.Ring
	filter   *LevelFilter
	seq      atomic.Int64
	attrs    []slog.Attr  // accumulated attrs from WithAttrs
	groups   []string    // accumulated group prefixes
	comp     string      // resolved component name
	src      string      // source: daemon|container|lifecycle|api-audit
	logDir   string      // ~/.unet/logs/
	baseName string      // daemon-YYYY-MM-DD
	date     string      // current date string (for date-based rotation)
	maxSize  int64       // max file size in bytes
	file     *os.File    // current open file handle
	fileSize    int64          // tracked file size
	stdout      bool           // also write to stdout
	degradeLevel atomic.Int32  // 0=normal, 1=drop debug, 2=drop info, 3=file write stopped
	alertCh     chan struct{}  // signals SSE-only alert (for disk-full)
}

// HandlerOptions configures the custom slog handler.
type HandlerOptions struct {
	LogDir         string         // ~/.unet/logs/ — directory for JSONL files
	MaxFileSizeMB  int            // max file size before rotation (default 100)
	Ring           *logstream.Ring // ring buffer for SSE/admin UI
	Filter         *LevelFilter   // per-component level filter
	Source         string         // daemon|container|lifecycle|api-audit (default "daemon")
	Component      string         // component name (default "unknown")
	LogToStdout    bool           // also write to stdout (migration bridge)
}

// NewHandler creates a new slog.Handler with JSON output, dual-write, and rotation.
func NewHandler(opts HandlerOptions) (*jsonHandler, error) {
	if opts.MaxFileSizeMB <= 0 {
		opts.MaxFileSizeMB = 100
	}
	if opts.Source == "" {
		opts.Source = "daemon"
	}
	if opts.Component == "" {
		opts.Component = "unknown"
	}

	h := &jsonHandler{
		ring:      opts.Ring,
		filter:    opts.Filter,
		src:       opts.Source,
		comp:      opts.Component,
		logDir:    opts.LogDir,
		maxSize:   int64(opts.MaxFileSizeMB) * 1024 * 1024,
		stdout:    opts.LogToStdout,
		baseName:  "daemon",
		alertCh:   make(chan struct{}, 1),
	}

	if opts.LogDir != "" {
		if err := os.MkdirAll(opts.LogDir, 0700); err != nil {
			return nil, fmt.Errorf("create log dir %s: %w", opts.LogDir, err)
		}
		if err := h.openNewFile(); err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
	}

	return h, nil
}

// openNewFile opens a new log file for today's date.
func (h *jsonHandler) openNewFile() error {
	now := time.Now().UTC()
	h.date = now.Format("2006-01-02")
	filename := filepath.Join(h.logDir, h.baseName+"-"+h.date+".jsonl")

	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", filename, err)
	}

	// Track file size from existing content
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("stat log file %s: %w", filename, err)
	}

	h.file = f
	h.writer = f
	h.fileSize = stat.Size()
	return nil
}

// rotateIfNeeded checks if rotation is needed (size or date change) and performs it.
func (h *jsonHandler) rotateIfNeeded() error {
	// Check date change
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	if today != h.date {
		return h.rotateDate(today)
	}

	// Check size
	if h.fileSize >= h.maxSize {
		return h.rotateSize()
	}
	return nil
}

// rotateDate performs date-based rotation.
func (h *jsonHandler) rotateDate(newDate string) error {
	if h.file != nil {
		h.file.Close()
	}
	h.date = newDate
	return h.openNewFile()
}

// rotateSize performs size-based rotation.
func (h *jsonHandler) rotateSize() error {
	if h.file == nil {
		return nil
	}

	// Close current file
	oldName := h.file.Name()
	h.file.Close()

	// Find next available rotation index
	for i := 1; i < 1000; i++ {
		rotated := h.baseName + "-" + h.date + "." + fmt.Sprintf("%d", i) + ".jsonl"
		rotatedPath := filepath.Join(h.logDir, rotated)
		if _, err := os.Stat(rotatedPath); os.IsNotExist(err) {
			// Rename current to rotated
			if err := os.Rename(oldName, rotatedPath); err != nil {
				// Can't rotate — reopen for append
				f, ferr := os.OpenFile(oldName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
				if ferr != nil {
					return fmt.Errorf("rotate rename %s→%s: %w, reopen: %w", oldName, rotatedPath, err, ferr)
				}
				h.file = f
				h.writer = f
				return fmt.Errorf("rotate rename failed: %w", err)
			}
			break
		}
	}

	// Open new file
	return h.openNewFile()
}

// Enabled implements slog.Handler.
func (h *jsonHandler) Enabled(_ context.Context, level slog.Level) bool {
	if h.filter == nil {
		return level >= slog.LevelInfo
	}
	return h.filter.Enabled(h.comp, level)
}

// Handle implements slog.Handler. Writes a single JSONL line.
func (h *jsonHandler) Handle(_ context.Context, r slog.Record) error {
	seq := h.seq.Add(1)
	now := time.Now().UTC()

	// Build fields map
	fields := make(map[string]any, r.NumAttrs()+len(h.attrs))
	component := h.comp
	source := h.src

	// Add handler-level attrs first
	for _, a := range h.attrs {
		k, v := attrToKV(a, h.groups)
		if k == "component" {
			if s, ok := v.(string); ok {
				component = s
			}
		} else if k == "source" {
			if s, ok := v.(string); ok {
				source = s
			}
		} else {
			fields[k] = v
		}
	}

	// Add record-level attrs
	r.Attrs(func(a slog.Attr) bool {
		k, v := attrToKV(a, h.groups)
		if k == "component" {
			if s, ok := v.(string); ok {
				component = s
			}
		} else if k == "source" {
			if s, ok := v.(string); ok {
				source = s
			}
		} else {
			fields[k] = v
		}
		return true
	})

	// Redact secrets
	fields = RedactFields(fields)

	// Build log record
	msg := r.Message
	if msg == "" {
		msg = " " // non-empty per schema
	}

	record := map[string]any{
		"ts":        now.Format("2006-01-02T15:04:05.000Z"),
		"level":     levelToString(r.Level),
		"component": component,
		"source":    source,
		"msg":       msg,
		"seq":       seq,
	}
	if len(fields) > 0 {
		record["fields"] = fields
	}

	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal log record: %w", err)
	}
	line = append(line, '\n')

	// Dual-write under mutex
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check rotation
	if h.logDir != "" {
		_ = h.rotateIfNeeded()
	}

	// Write to file (with disk-full graceful degradation)
	if h.writer != nil {
		dl := h.degradeLevel.Load()
		if dl >= 3 {
			// File writing stopped — ring buffer only. Signal alert once.
			select {
			case h.alertCh <- struct{}{}:
			default:
			}
		} else if dl == 2 && r.Level < slog.LevelWarn {
			// Drop info and debug
		} else if dl == 1 && r.Level < slog.LevelInfo {
			// Drop debug
		} else {
			n, werr := h.writer.Write(line)
			h.fileSize += int64(n)
			if werr != nil {
				h.handleWriteError(werr)
			} else {
				// Success — try to recover degradation level
				if dl > 0 {
					h.degradeLevel.CompareAndSwap(dl, dl-1)
				}
			}
		}
	}

	// Write to stdout (migration bridge)
	if h.stdout {
		os.Stdout.Write(line)
	}

	// Write to ring buffer (lock-free)
	if h.ring != nil {
		lr := logstream.LogRecord{
			TS:        record["ts"].(string),
			Level:     record["level"].(string),
			Component: component,
			Source:    source,
			Msg:       msg,
			Seq:       seq,
			Fields:    fields,
		}
		h.ring.Write(lr)
	}

	return nil
}

// WithAttrs implements slog.Handler.
func (h *jsonHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newH := h.clone()
	newH.attrs = append(newH.attrs, attrs...)
	return newH
}

// WithGroup implements slog.Handler.
func (h *jsonHandler) WithGroup(name string) slog.Handler {
	newH := h.clone()
	newH.groups = append(newH.groups, name)
	return newH
}

// clone creates a shallow copy of the handler.
func (h *jsonHandler) clone() *jsonHandler {
	return &jsonHandler{
		writer:   h.writer,
		ring:     h.ring,
		filter:   h.filter,
		comp:     h.comp,
		src:      h.src,
		logDir:   h.logDir,
		baseName: h.baseName,
		date:     h.date,
		maxSize:  h.maxSize,
		file:     h.file,
		fileSize: h.fileSize,
		stdout:   h.stdout,
		attrs:    append([]slog.Attr{}, h.attrs...),
		groups:   append([]string{}, h.groups...),
	}
}

// Close flushes and closes the current log file.
func (h *jsonHandler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file != nil {
		return h.file.Close()
	}
	return nil
}

// attrToKV converts a slog.Attr to a key-value pair.
// Resolves group prefixes.
func attrToKV(a slog.Attr, groups []string) (string, any) {
	if a.Key == "" {
		return "", nil
	}
	key := a.Key
	if len(groups) > 0 {
		prefix := ""
		for _, g := range groups {
			if g != "" {
				prefix += g + "."
			}
		}
		key = prefix + key
	}

	val := a.Value.Resolve()
	switch val.Kind() {
	case slog.KindString:
		return key, val.String()
	case slog.KindInt64:
		return key, val.Int64()
	case slog.KindFloat64:
		return key, val.Float64()
	case slog.KindBool:
		return key, val.Bool()
	case slog.KindDuration:
		return key, val.Duration().String()
	case slog.KindTime:
		return key, val.Time().Format(time.RFC3339Nano)
	case slog.KindGroup:
		// Flatten group attrs into a map
		groupAttrs := val.Group()
		m := make(map[string]any, len(groupAttrs))
		for _, ga := range groupAttrs {
			k, v := attrToKV(ga, nil)
			if k != "" {
				m[k] = v
			}
		}
		return key, m
	default:
		return key, val.Any()
	}
}

// levelToString converts slog.Level to string.
func levelToString(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warn"
	case l >= slog.LevelInfo:
		return "info"
	default:
		return "debug"
	}
}

// SetComponent creates a child handler with the given component tag.
func SetComponent(h slog.Handler, component string) slog.Handler {
	return h.WithAttrs([]slog.Attr{
		slog.String("component", component),
	})
}

// SetSource creates a child handler with the given source tag.
func SetSource(h slog.Handler, source string) slog.Handler {
	return h.WithAttrs([]slog.Attr{
		slog.String("source", source),
	})
}

// handleWriteError implements disk-full graceful degradation (TASK-2.3).
// Cascade: delete oldest archives → drop debug → drop info → SSE-only alert.
// Async cleanup dispatched to avoid blocking the handler mutex.
func (h *jsonHandler) handleWriteError(err error) {
	dl := h.degradeLevel.Load()

	// Try async cleanup: delete oldest archives to free space
	if h.logDir != "" {
		go h.asyncFreeSpace()
	}

	// Escalate degradation
	switch dl {
	case 0:
		h.degradeLevel.Store(1) // drop debug
	case 1:
		h.degradeLevel.Store(2) // drop info
	case 2:
		h.degradeLevel.Store(3) // file write stopped
		// Alert SSE subscribers (not file — chicken-and-egg)
		select {
		case h.alertCh <- struct{}{}:
		default:
		}
	}
}

// asyncFreeSpace attempts to delete oldest archive files to free disk space.
// Runs in a goroutine — does NOT hold the handler mutex.
func (h *jsonHandler) asyncFreeSpace() {
	entries, err := os.ReadDir(h.logDir)
	if err != nil {
		return
	}

	// Sort by name (oldest first due to date in filename)
	import_sort_entries(entries)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		// Skip active file (current date)
		if strings.Contains(entry.Name(), time.Now().UTC().Format("2006-01-02")) &&
			!strings.Contains(entry.Name(), ".1.") &&
			!strings.Contains(entry.Name(), ".2.") {
			continue
		}

		path := filepath.Join(h.logDir, entry.Name())
		if err := os.Remove(path); err == nil {
			// Freed some space — check if we can recover
			dl := h.degradeLevel.Load()
			if dl > 0 {
				h.degradeLevel.CompareAndSwap(dl, dl-1)
			}
			return
		}
	}
}

func import_sort_entries(entries []os.DirEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
}
