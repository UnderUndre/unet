package logger

import (
	"log/slog"
	"sync/atomic"
)

// LevelConfig holds global and per-component log level thresholds.
// Supports hot-reload via atomic.Value.
type LevelConfig struct {
	global    slog.Level
	overrides map[string]slog.Level
}

// levelConfigHolder wraps LevelConfig for atomic.Value storage.
type levelConfigHolder struct {
	config LevelConfig
}

// LevelFilter provides thread-safe per-component level filtering with hot-reload.
type LevelFilter struct {
	value atomic.Value // stores *levelConfigHolder
}

// NewLevelFilter creates a filter with the given global level and optional per-component overrides.
func NewLevelFilter(global slog.Level, overrides map[string]slog.Level) *LevelFilter {
	lf := &LevelFilter{}
	lf.value.Store(&levelConfigHolder{
		config: LevelConfig{
			global:    global,
			overrides: overrides,
		},
	})
	return lf
}

// Enabled checks if a log record with the given level should be emitted for the given component.
// Checks component override first, falls back to global threshold.
func (lf *LevelFilter) Enabled(component string, level slog.Level) bool {
	holder := lf.value.Load().(*levelConfigHolder)
	cfg := holder.config

	threshold := cfg.global
	if compLevel, ok := cfg.overrides[component]; ok {
		threshold = compLevel
	}
	return level >= threshold
}

// Global returns the current global log level.
func (lf *LevelFilter) Global() slog.Level {
	holder := lf.value.Load().(*levelConfigHolder)
	return holder.config.global
}

// Overrides returns the current per-component overrides map.
func (lf *LevelFilter) Overrides() map[string]slog.Level {
	holder := lf.value.Load().(*levelConfigHolder)
	return holder.config.overrides
}

// Reload atomically swaps the level configuration. Readers never block.
func (lf *LevelFilter) Reload(global slog.Level, overrides map[string]slog.Level) {
	lf.value.Store(&levelConfigHolder{
		config: LevelConfig{
			global:    global,
			overrides: overrides,
		},
	})
}

// ParseLevel converts a string level name to slog.Level.
// Returns slog.LevelInfo and false if unrecognized.
func ParseLevel(s string) (slog.Level, bool) {
	switch stringsToLower(s) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}

// MustParseLevel parses a level string or panics.
func MustParseLevel(s string) slog.Level {
	l, ok := ParseLevel(s)
	if !ok {
		return slog.LevelInfo
	}
	return l
}

func stringsToLower(s string) string {
	// Avoid importing strings for a single call — use simple loop
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}
