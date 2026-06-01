package wizard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStateFilePath(t *testing.T) {
	got := StateFilePath("/tmp/test")
	want := filepath.Join("/tmp/test", "wizard-state.json")
	if got != want {
		t.Errorf("StateFilePath(%q) = %q, want %q", "/tmp/test", got, want)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()

	original := &WizardState{
		SessionID:   "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		CurrentStep: StepCreatePeer,
		Status:      StatusInProgress,
		Inputs: WizardInputs{
			SSH: &SSHInput{
				Host:     "192.168.1.1",
				Port:     22,
				User:     "root",
				AuthType: "key",
				KeyPath:  "/home/user/.ssh/id_rsa",
			},
			DomainMode:     "byo",
			Domain:         "example.com",
			NipioEnabled:   false,
			FirstPeerName:  "my-peer",
			FirstPortExpose: &PortExpose{LocalPort: 8080, Subdomain: "api"},
		},
		PreflightResult: &PreflightResult{
			TargetHost:    "192.168.1.1",
			Distro:        "ubuntu",
			DistroVersion: "22.04",
			Arch:          "x86_64",
			DiskFreeGB:    50.5,
			RAMMB:         4096,
			HasSudo:       true,
			HasDocker:     true,
			DockerRunning: true,
			Port443Free:   true,
			Port80Free:    true,
			PortWGFree:    true,
			Compatible:    true,
			Warnings:      []string{"low disk"},
		},
		DomainCheckResult: &DomainCheckResult{
			Domain:      "example.com",
			Mode:        "byo",
			ARecordIPs:  []string{"192.168.1.1"},
			PointsToVPS: true,
			TLSFeasible: true,
		},
		ProgressPct:  75,
		StartedAt:    "2025-01-01T00:00:00Z",
		LastSavedAt:  "2025-01-01T00:00:00Z",
		ErrorMessage: "",
	}

	if err := SaveState(dir, original); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if loaded.SessionID != original.SessionID {
		t.Errorf("SessionID mismatch: got %q, want %q", loaded.SessionID, original.SessionID)
	}
	if loaded.CurrentStep != original.CurrentStep {
		t.Errorf("CurrentStep mismatch: got %q, want %q", loaded.CurrentStep, original.CurrentStep)
	}
	if loaded.Status != original.Status {
		t.Errorf("Status mismatch: got %q, want %q", loaded.Status, original.Status)
	}
	if loaded.Inputs.DomainMode != original.Inputs.DomainMode {
		t.Errorf("Inputs.DomainMode mismatch: got %q, want %q", loaded.Inputs.DomainMode, original.Inputs.DomainMode)
	}
	if loaded.Inputs.Domain != original.Inputs.Domain {
		t.Errorf("Inputs.Domain mismatch: got %q, want %q", loaded.Inputs.Domain, original.Inputs.Domain)
	}
	if loaded.Inputs.SSH == nil {
		t.Fatal("Inputs.SSH is nil")
	}
	if loaded.Inputs.SSH.Host != original.Inputs.SSH.Host {
		t.Errorf("Inputs.SSH.Host mismatch: got %q, want %q", loaded.Inputs.SSH.Host, original.Inputs.SSH.Host)
	}
	if loaded.Inputs.SSH.Port != original.Inputs.SSH.Port {
		t.Errorf("Inputs.SSH.Port mismatch: got %d, want %d", loaded.Inputs.SSH.Port, original.Inputs.SSH.Port)
	}
	if loaded.Inputs.FirstPortExpose == nil {
		t.Fatal("Inputs.FirstPortExpose is nil")
	}
	if loaded.Inputs.FirstPortExpose.LocalPort != original.Inputs.FirstPortExpose.LocalPort {
		t.Errorf("FirstPortExpose.LocalPort mismatch: got %d, want %d", loaded.Inputs.FirstPortExpose.LocalPort, original.Inputs.FirstPortExpose.LocalPort)
	}
	if loaded.PreflightResult == nil {
		t.Fatal("PreflightResult is nil")
	}
	if loaded.PreflightResult.Compatible != original.PreflightResult.Compatible {
		t.Errorf("PreflightResult.Compatible mismatch: got %v, want %v", loaded.PreflightResult.Compatible, original.PreflightResult.Compatible)
	}
	if loaded.PreflightResult.DiskFreeGB != original.PreflightResult.DiskFreeGB {
		t.Errorf("PreflightResult.DiskFreeGB mismatch: got %v, want %v", loaded.PreflightResult.DiskFreeGB, original.PreflightResult.DiskFreeGB)
	}
	if loaded.DomainCheckResult == nil {
		t.Fatal("DomainCheckResult is nil")
	}
	if loaded.DomainCheckResult.TLSFeasible != original.DomainCheckResult.TLSFeasible {
		t.Errorf("DomainCheckResult.TLSFeasible mismatch: got %v, want %v", loaded.DomainCheckResult.TLSFeasible, original.DomainCheckResult.TLSFeasible)
	}
	if loaded.ProgressPct != original.ProgressPct {
		t.Errorf("ProgressPct mismatch: got %d, want %d", loaded.ProgressPct, original.ProgressPct)
	}
	if loaded.StartedAt != original.StartedAt {
		t.Errorf("StartedAt mismatch: got %q, want %q", loaded.StartedAt, original.StartedAt)
	}
	if loaded.LastSavedAt == "" {
		t.Error("LastSavedAt should have been set by SaveState")
	}
}

func TestSaveStateUpdatesLastSavedAt(t *testing.T) {
	dir := t.TempDir()

	state := &WizardState{
		SessionID:   "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		CurrentStep: StepWelcome,
		Status:      StatusInProgress,
		StartedAt:   "2025-01-01T00:00:00Z",
		LastSavedAt: "2025-01-01T00:00:00Z",
	}

	if err := SaveState(dir, state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	if state.LastSavedAt == "2025-01-01T00:00:00Z" {
		t.Error("SaveState should update LastSavedAt")
	}
}

func TestSaveStateFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions not enforced on Windows")
	}

	dir := t.TempDir()

	state := &WizardState{
		SessionID:   "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		CurrentStep: StepWelcome,
		Status:      StatusInProgress,
		StartedAt:   "2025-01-01T00:00:00Z",
	}

	if err := SaveState(dir, state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	info, err := os.Stat(StateFilePath(dir))
	if err != nil {
		t.Fatalf("failed to stat state file: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestSaveStateAtomicNoTempFileRemains(t *testing.T) {
	dir := t.TempDir()

	state := &WizardState{
		SessionID:   "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		CurrentStep: StepWelcome,
		Status:      StatusInProgress,
		StartedAt:   "2025-01-01T00:00:00Z",
	}

	if err := SaveState(dir, state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".wizard-state-") && strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", entry.Name())
		}
	}

	if len(entries) != 1 {
		t.Errorf("expected exactly 1 file in dir, got %d", len(entries))
	}
}

func TestDeleteStateRemovesFile(t *testing.T) {
	dir := t.TempDir()

	state := &WizardState{
		SessionID:   "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		CurrentStep: StepWelcome,
		Status:      StatusInProgress,
		StartedAt:   "2025-01-01T00:00:00Z",
	}

	if err := SaveState(dir, state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	if _, err := os.Stat(StateFilePath(dir)); err != nil {
		t.Fatalf("state file should exist before delete: %v", err)
	}

	if err := DeleteState(dir); err != nil {
		t.Fatalf("DeleteState failed: %v", err)
	}

	if _, err := os.Stat(StateFilePath(dir)); !os.IsNotExist(err) {
		t.Error("state file should not exist after delete")
	}
}

func TestDeleteStateNoFileNoError(t *testing.T) {
	dir := t.TempDir()

	if err := DeleteState(dir); err != nil {
		t.Errorf("DeleteState on non-existent file should not error: %v", err)
	}
}

func TestLoadStateNoFile(t *testing.T) {
	dir := t.TempDir()

	state, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState on missing file should not error: %v", err)
	}
	if state != nil {
		t.Error("LoadState on missing file should return nil state")
	}
}

func TestLoadStateCorruptedJSON(t *testing.T) {
	dir := t.TempDir()

	path := StateFilePath(dir)
	if err := os.WriteFile(path, []byte("{not valid json}"), 0600); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	_, err := LoadState(dir)
	if err == nil {
		t.Fatal("LoadState with corrupt JSON should return error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error should mention invalid JSON, got: %v", err)
	}
}

func TestLoadStateEmptyFile(t *testing.T) {
	dir := t.TempDir()

	path := StateFilePath(dir)
	if err := os.WriteFile(path, []byte(""), 0600); err != nil {
		t.Fatalf("failed to write empty file: %v", err)
	}

	_, err := LoadState(dir)
	if err == nil {
		t.Fatal("LoadState with empty file should return error")
	}
}

func TestLoadStateInvalidSessionID(t *testing.T) {
	dir := t.TempDir()

	state := &WizardState{
		SessionID:   "not-a-uuid",
		CurrentStep: StepWelcome,
		Status:      StatusInProgress,
		StartedAt:   "2025-01-01T00:00:00Z",
	}

	data, _ := json.MarshalIndent(state, "", "  ")
	path := StateFilePath(dir)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err := LoadState(dir)
	if err == nil {
		t.Fatal("LoadState with invalid session_id should return error")
	}
	if !strings.Contains(err.Error(), "invalid session_id") {
		t.Errorf("error should mention invalid session_id, got: %v", err)
	}
}

func TestLoadStateInvalidCurrentStep(t *testing.T) {
	dir := t.TempDir()

	state := &WizardState{
		SessionID:   "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		CurrentStep: "nonexistent_step",
		Status:      StatusInProgress,
		StartedAt:   "2025-01-01T00:00:00Z",
	}

	data, _ := json.MarshalIndent(state, "", "  ")
	path := StateFilePath(dir)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err := LoadState(dir)
	if err == nil {
		t.Fatal("LoadState with invalid current_step should return error")
	}
	if !strings.Contains(err.Error(), "invalid current_step") {
		t.Errorf("error should mention invalid current_step, got: %v", err)
	}
}

func TestSaveStateNilReturnsError(t *testing.T) {
	dir := t.TempDir()

	if err := SaveState(dir, nil); err == nil {
		t.Fatal("SaveState with nil state should return error")
	}
}

func TestLoadStateResumeCommitInProgress(t *testing.T) {
	dir := t.TempDir()

	state := &WizardState{
		SessionID:   "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		CurrentStep: StepCommit,
		Status:      StatusInProgress,
		StartedAt:   "2025-01-01T00:00:00Z",
		LastSavedAt: "2025-01-01T00:05:00Z",
	}

	if err := SaveState(dir, state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if loaded.CurrentStep != StepCommit {
		t.Errorf("CurrentStep = %q, want %q", loaded.CurrentStep, StepCommit)
	}
	if loaded.Status != StatusInProgress {
		t.Errorf("Status = %q, want %q", loaded.Status, StatusInProgress)
	}
}
