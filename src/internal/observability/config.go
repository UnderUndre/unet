// Package observability provides top-level initialization and configuration
// for the unet observability subsystem: structured logging, ring buffer,
// SSE streaming, Prometheus metrics, and container log aggregation.
package observability

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// Config holds all observability settings. Nested under "observability" in config.json.
type Config struct {
	Enabled              bool            `json:"enabled"`
	LogLevels            map[string]string `json:"logLevels"`            // component→level, e.g. {"tunnel":"debug"}
	GlobalLevel          string          `json:"globalLevel"`          // default "info"
	MaxFileSizeMB        int             `json:"maxFileSizeMB"`        // default 100
	RetentionDays        int             `json:"retentionDays"`        // default 30
	CaptureContainerLogs bool            `json:"captureContainerLogs"` // default true
	ScrubPii             bool            `json:"scrubPii"`             // default false
	SSEClientBuffer      int             `json:"sseClientBuffer"`      // default 1000
	LogToStdout          bool            `json:"logToStdout"`          // default true (migration bridge)
	Metrics              MetricsConfig   `json:"metrics"`
}

// MetricsConfig holds Prometheus metrics endpoint settings.
type MetricsConfig struct {
	Enabled    bool   `json:"enabled"`
	ListenAddr string `json:"listenAddr"`  // default "127.0.0.1:9090"
	BearerToken string `json:"bearerToken"` // required for non-loopback
}

// DefaultConfig returns the default observability configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:              true,
		GlobalLevel:          "info",
		MaxFileSizeMB:        100,
		RetentionDays:        30,
		CaptureContainerLogs: true,
		ScrubPii:             false,
		SSEClientBuffer:      1000,
		LogToStdout:          true,
		Metrics: MetricsConfig{
			Enabled:    false,
			ListenAddr: "127.0.0.1:9090",
		},
	}
}

// ApplyDefaults fills zero-value fields with defaults.
func (c *Config) ApplyDefaults() {
	d := DefaultConfig()
	if c.GlobalLevel == "" {
		c.GlobalLevel = d.GlobalLevel
	}
	if c.MaxFileSizeMB <= 0 {
		c.MaxFileSizeMB = d.MaxFileSizeMB
	}
	if c.RetentionDays <= 0 {
		c.RetentionDays = d.RetentionDays
	}
	if c.SSEClientBuffer <= 0 {
		c.SSEClientBuffer = d.SSEClientBuffer
	}
	if c.Metrics.ListenAddr == "" {
		c.Metrics.ListenAddr = d.Metrics.ListenAddr
	}
	// CaptureContainerLogs defaults to true — only set if explicitly false
	// (zero value of bool is false, but we want true as default)
	// Caller should use ApplyDefaults after JSON unmarshal, then explicitly
	// set CaptureContainerLogs=true if the JSON didn't contain the key.
}

// Bus is the central wiring point for the observability subsystem.
// It holds references to all components and provides lifecycle management.
type Bus struct {
	Config     Config
	LogDir     string
	RingSize   int

	mu      sync.Mutex
	started bool
}

// NewBus creates a new observability bus with the given config.
func NewBus(cfg Config, logDir string) *Bus {
	cfg.ApplyDefaults()
	if logDir == "" {
		home, _ := os.UserHomeDir()
		logDir = filepath.Join(home, ".unet", "logs")
	}
	ringSize := 200
	return &Bus{
		Config:   cfg,
		LogDir:   logDir,
		RingSize: ringSize,
	}
}

// parseLevelOverrides converts string map to slog.Level map.
func parseLevelOverrides(logLevels map[string]string) map[string]slog.Level {
	overrides := make(map[string]slog.Level, len(logLevels))
	for comp, lvl := range logLevels {
		parsed := MustParseLevel(lvl)
		overrides[comp] = parsed
	}
	return overrides
}

// MustParseLevel parses a level string or returns slog.LevelInfo.
func MustParseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
