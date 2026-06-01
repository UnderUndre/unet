package wizard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const stateFileName = "wizard-state.json"

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func StateFilePath(dir string) string {
	return filepath.Join(dir, stateFileName)
}

func SaveState(dir string, state *WizardState) error {
	if state == nil {
		return fmt.Errorf("state must not be nil")
	}

	state.LastSavedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal wizard state: %w", err)
	}

	path := StateFilePath(dir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".wizard-state-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write state to temp file: %w", err)
	}

	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to set file permissions: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file to state file: %w", err)
	}

	return nil
}

func LoadState(dir string) (*WizardState, error) {
	path := StateFilePath(dir)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read wizard state: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("wizard state file is empty")
	}

	var state WizardState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("wizard state file contains invalid JSON: %w", err)
	}

	if !uuidPattern.MatchString(state.SessionID) {
		return nil, fmt.Errorf("wizard state has invalid session_id: %q (expected UUID format)", state.SessionID)
	}

	if !IsValidStep(state.CurrentStep) {
		return nil, fmt.Errorf("wizard state has invalid current_step: %q", state.CurrentStep)
	}

	return &state, nil
}

func DeleteState(dir string) error {
	path := StateFilePath(dir)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete wizard state: %w", err)
	}
	return nil
}
