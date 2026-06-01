package logger

import (
	"log/slog"
	"testing"
)

func TestNewLevelFilter(t *testing.T) {
	lf := NewLevelFilter(slog.LevelInfo, map[string]slog.Level{
		"tunnel":  slog.LevelDebug,
		"api":     slog.LevelWarn,
	})
	defer lf.Reload(slog.LevelInfo, nil)

	// Global level: info
	if !lf.Enabled("unknown", slog.LevelInfo) {
		t.Error("info should be enabled for unknown component at global info level")
	}
	if lf.Enabled("unknown", slog.LevelDebug) {
		t.Error("debug should NOT be enabled for unknown component at global info level")
	}

	// Override: tunnel=debug
	if !lf.Enabled("tunnel", slog.LevelDebug) {
		t.Error("debug should be enabled for tunnel component (override)")
	}

	// Override: api=warn
	if lf.Enabled("api", slog.LevelInfo) {
		t.Error("info should NOT be enabled for api component (override=warn)")
	}
	if !lf.Enabled("api", slog.LevelWarn) {
		t.Error("warn should be enabled for api component (override)")
	}
}

func TestLevelFilterReload(t *testing.T) {
	lf := NewLevelFilter(slog.LevelInfo, nil)

	// Before reload: debug disabled
	if lf.Enabled("any", slog.LevelDebug) {
		t.Error("debug should be disabled at info level")
	}

	// Reload to debug
	lf.Reload(slog.LevelDebug, nil)

	// After reload: debug enabled
	if !lf.Enabled("any", slog.LevelDebug) {
		t.Error("debug should be enabled after reload to debug level")
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
		ok       bool
	}{
		{"debug", slog.LevelDebug, true},
		{"info", slog.LevelInfo, true},
		{"warn", slog.LevelWarn, true},
		{"error", slog.LevelError, true},
		{"DEBUG", slog.LevelDebug, true},
		{"Info", slog.LevelInfo, true},
		{"unknown", slog.LevelInfo, false},
		{"", slog.LevelInfo, false},
	}

	for _, tt := range tests {
		level, ok := ParseLevel(tt.input)
		if ok != tt.ok {
			t.Errorf("ParseLevel(%q) ok = %v, want %v", tt.input, ok, tt.ok)
		}
		if level != tt.expected {
			t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, level, tt.expected)
		}
	}
}

func TestLevelFilterGlobal(t *testing.T) {
	lf := NewLevelFilter(slog.LevelWarn, nil)
	if lf.Global() != slog.LevelWarn {
		t.Errorf("global = %v, want %v", lf.Global(), slog.LevelWarn)
	}
}

func TestLevelFilterOverrides(t *testing.T) {
	overrides := map[string]slog.Level{
		"tunnel": slog.LevelDebug,
	}
	lf := NewLevelFilter(slog.LevelInfo, overrides)

	ov := lf.Overrides()
	if ov["tunnel"] != slog.LevelDebug {
		t.Errorf("override tunnel = %v, want %v", ov["tunnel"], slog.LevelDebug)
	}
}
