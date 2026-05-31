package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogger_Write(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.jsonl")

	logger, err := NewLogger(logPath)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer logger.Close()

	entry := Entry{
		ActorTokenID:     "token-1",
		ActorTokenName:   "admin",
		Action:           ActionCreatePeer,
		TargetResourceID: "peer-1",
		SourceIP:         "127.0.0.1",
		UserAgent:        "test/1.0",
		Metadata:         map[string]any{"peerName": "test"},
	}

	if err := logger.Write(entry); err != nil {
		t.Fatalf("Write: %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var parsed Entry
	if err := json.Unmarshal([]byte(lines[0]), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.ID == "" {
		t.Error("expected auto-generated ID")
	}
	if parsed.Timestamp == "" {
		t.Error("expected auto-generated timestamp")
	}
	if parsed.Action != ActionCreatePeer {
		t.Errorf("expected action %s, got %s", ActionCreatePeer, parsed.Action)
	}
	if parsed.ActorTokenID != "token-1" {
		t.Errorf("expected actorTokenId token-1, got %s", parsed.ActorTokenID)
	}
}

func TestLogger_AppendMultiple(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewLogger(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer logger.Close()

	for i := 0; i < 5; i++ {
		if err := logger.Write(Entry{Action: ActionCreatePeer}); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	raw, _ := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}
}

func TestLogger_MetadataTooLarge(t *testing.T) {
	dir := t.TempDir()
	logger, _ := NewLogger(filepath.Join(dir, "audit.jsonl"))
	defer logger.Close()

	bigMeta := make(map[string]any)
	bigMeta["data"] = strings.Repeat("x", 5000)

	err := logger.Write(Entry{Metadata: bigMeta})
	if err == nil {
		t.Error("expected error for oversized metadata")
	}
}

func TestLogger_FileCreatedOnFirstWrite(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "subdir", "audit.jsonl")

	logger, err := NewLogger(logPath)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer logger.Close()

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("file should exist after NewLogger")
	}
}
