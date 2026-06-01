package observability

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/underundre/unet/internal/logstream"
	"github.com/underundre/unet/internal/logger"
)

// Init initializes the observability subsystem:
// 1. Creates log directory
// 2. Creates ring buffer
// 3. Creates level filter
// 4. Creates slog handler
// 5. Sets as default logger
// 6. Returns Bus for downstream wiring (SSE, metrics, container capture)
func Init(cfg Config, logDir string) (*Bus, *logstream.Ring, error) {
	bus := NewBus(cfg, logDir)

	// Create log directory
	if err := os.MkdirAll(bus.LogDir, 0700); err != nil {
		return nil, nil, fmt.Errorf("create log dir %s: %w", bus.LogDir, err)
	}

	// Check disk headroom (500MB warning)
	checkDiskHeadroom(bus.LogDir)

	// Create ring buffer
	ring := logstream.NewRing(bus.RingSize)

	// Create level filter
	globalLevel := MustParseLevel(bus.Config.GlobalLevel)
	overrides := parseLevelOverrides(bus.Config.LogLevels)
	filter := logger.NewLevelFilter(globalLevel, overrides)

	// Create slog handler
	handler, err := logger.NewHandler(logger.HandlerOptions{
		LogDir:        bus.LogDir,
		MaxFileSizeMB: bus.Config.MaxFileSizeMB,
		Ring:          ring,
		Filter:        filter,
		Source:        "daemon",
		Component:     "unknown",
		LogToStdout:   bus.Config.LogToStdout,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create log handler: %w", err)
	}

	// Set as default slog handler
	slog.SetDefault(slog.New(handler))

	// Bridge existing log.Printf calls
	// Go 1.21+: slog.SetDefault redirects log.Default() output

	bus.mu.Lock()
	bus.started = true
	bus.mu.Unlock()

	slog.Info("observability subsystem initialized",
		"log_dir", bus.LogDir,
		"global_level", bus.Config.GlobalLevel,
		"ring_size", bus.RingSize,
	)

	return bus, ring, nil
}

// checkDiskHeadroom emits a warning if < 500MB free on the log filesystem.
// Full implementation deferred to TASK-2.3 — requires platform-specific syscall.
func checkDiskHeadroom(logDir string) {
	// Best-effort — continue even if we can't check
}
