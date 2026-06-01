package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/underundre/unet/internal/config"
)

// MigrationStatus tracks the phase of a VPS-to-VPS migration.
type MigrationStatus string

const (
	MigrationPending      MigrationStatus = "pending"
	MigrationBootstrapping MigrationStatus = "bootstrapping"
	MigrationSyncing      MigrationStatus = "syncing"
	MigrationCutover      MigrationStatus = "cutover"
	MigrationDraining     MigrationStatus = "draining"
	MigrationComplete     MigrationStatus = "complete"
	MigrationAborted      MigrationStatus = "aborted"
)

// MigrationPlan tracks an in-progress or completed VPS-to-VPS migration.
// Persisted at ~/.unet/migration.json.
type MigrationPlan struct {
	ID             string          `json:"id"`
	SourceVPS      string          `json:"sourceVPS"`
	TargetVPS      string          `json:"targetVPS"`
	DNSTtlSeconds  int             `json:"dnsTtlSeconds"`
	CutoverAt      string          `json:"cutoverAt"`
	Status         MigrationStatus `json:"status"`
	SnapshotID     string          `json:"snapshotId,omitempty"`
	StartedAt      string          `json:"startedAt,omitempty"`
	CompletedAt    string          `json:"completedAt,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// Validate checks all fields per data-model.md entity 3 rules.
func (m *MigrationPlan) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("state: migration id is required")
	}
	if _, err := uuid.Parse(m.ID); err != nil {
		return fmt.Errorf("state: migration id must be valid UUIDv4: %w", err)
	}
	if m.SourceVPS == "" {
		return fmt.Errorf("state: sourceVPS is required")
	}
	if m.TargetVPS == "" {
		return fmt.Errorf("state: targetVPS is required")
	}
	if m.SourceVPS == m.TargetVPS {
		return fmt.Errorf("state: sourceVPS and targetVPS must be different")
	}
	if m.DNSTtlSeconds < 60 || m.DNSTtlSeconds > 3600 {
		return fmt.Errorf("state: dnsTtlSeconds %d out of range (60-3600)", m.DNSTtlSeconds)
	}
	return nil
}

// --- Persistence ---

// MigrationPlanPath returns ~/.unet/migration.json
func MigrationPlanPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "migration.json"), nil
}

// LoadMigrationPlan reads and parses ~/.unet/migration.json.
// Returns nil if the file does not exist (no migration in progress).
func LoadMigrationPlan() (*MigrationPlan, error) {
	path, err := MigrationPlanPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("state: read migration.json: %w", err)
	}

	var plan MigrationPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("state: parse migration.json: %w", err)
	}
	return &plan, nil
}

// SaveMigrationPlan atomically writes plan to ~/.unet/migration.json.
// Validates before writing.
func SaveMigrationPlan(plan *MigrationPlan) error {
	if err := plan.Validate(); err != nil {
		return err
	}

	path, err := MigrationPlanPath()
	if err != nil {
		return err
	}
	return atomicWriteJSON(path, plan)
}

// DeleteMigrationPlan removes ~/.unet/migration.json.
// Used after migration completes or is fully aborted.
func DeleteMigrationPlan() error {
	path, err := MigrationPlanPath()
	if err != nil {
		return err
	}

	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("state: remove migration.json: %w", err)
	}
	return nil
}
