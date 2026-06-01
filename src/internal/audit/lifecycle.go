package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/underundre/unet/internal/config"
)

// --- Lifecycle-specific Action enum (extends 002's Action) ---

type LifecycleAction string

const (
	ActionBootstrapStart    LifecycleAction = "bootstrap_start"
	ActionBootstrapComplete LifecycleAction = "bootstrap_complete"
	ActionAttach            LifecycleAction = "attach"
	ActionDetach            LifecycleAction = "detach"
	ActionUpgradeStart      LifecycleAction = "upgrade_start"
	ActionUpgradeComplete   LifecycleAction = "upgrade_complete"
	ActionUpgradeRollback   LifecycleAction = "upgrade_rollback"
	ActionSnapshotCreate    LifecycleAction = "snapshot_create"
	ActionRollback          LifecycleAction = "rollback"
	ActionMigrateStart      LifecycleAction = "migrate_start"
	ActionMigrateCutover    LifecycleAction = "migrate_cutover"
	ActionMigrateComplete   LifecycleAction = "migrate_complete"
	ActionPartitionDetected LifecycleAction = "partition_detected"
	ActionReconnectSuccess  LifecycleAction = "reconnect_success"
	ActionStateExport       LifecycleAction = "state_export"
	ActionStateImport       LifecycleAction = "state_import"
)

// LifecycleResult describes the outcome of a lifecycle action.
type LifecycleResult string

const (
	ResultSuccess LifecycleResult = "success"
	ResultFailure LifecycleResult = "failure"
	ResultPartial LifecycleResult = "partial"
)

const lifecycleMaxMetadataSize = 4096

// LifecycleEvent is an immutable record of a VPS lifecycle action.
// Stored at ~/.unet/lifecycle-audit.jsonl, distinct from 002's audit.jsonl.
type LifecycleEvent struct {
	ID               string         `json:"id"`
	Timestamp        string         `json:"timestamp"`
	ActorTokenID     string         `json:"actorTokenId"`
	ActorTokenName   string         `json:"actorTokenName"`
	Action           LifecycleAction `json:"action"`
	TargetResourceID string         `json:"targetResourceId"`
	SourceIP         string         `json:"sourceIp"`
	UserAgent        string         `json:"userAgent"`
	Result           LifecycleResult `json:"result"`
	Metadata         map[string]any  `json:"metadata,omitempty"`
}

// LifecycleLogger writes lifecycle events to an append-only JSONL file.
type LifecycleLogger struct {
	mu   sync.Mutex
	file *os.File
}

// DefaultLifecyclePath returns the default path for lifecycle audit:
// ~/.unet/lifecycle-audit.jsonl
func DefaultLifecyclePath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lifecycle-audit.jsonl"), nil
}

// NewLifecycleLogger creates a new logger that appends to path.
// Creates the file if it does not exist.
func NewLifecycleLogger(path string) (*LifecycleLogger, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("lifecycle-audit: create dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lifecycle-audit: open file: %w", err)
	}

	return &LifecycleLogger{file: f}, nil
}

// Write appends a lifecycle event to the JSONL file. Auto-fills ID and
// timestamp if empty. Validates metadata size.
func (l *LifecycleLogger) Write(entry LifecycleEvent) error {
	if entry.Metadata != nil {
		raw, err := json.Marshal(entry.Metadata)
		if err != nil {
			return fmt.Errorf("lifecycle-audit: marshal metadata: %w", err)
		}
		if len(raw) > lifecycleMaxMetadataSize {
			return fmt.Errorf("lifecycle-audit: metadata exceeds %d bytes", lifecycleMaxMetadataSize)
		}
	}

	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("lifecycle-audit: marshal entry: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, err := l.file.Write(append(line, '\n')); err != nil {
		return err
	}
	return l.file.Sync()
}

// Close closes the underlying file.
func (l *LifecycleLogger) Close() error {
	return l.file.Close()
}
