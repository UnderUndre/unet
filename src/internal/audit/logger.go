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

const maxMetadataSize = 4096

type Logger struct {
	mu   sync.Mutex
	file *os.File
}

func DefaultPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "audit.jsonl"), nil
}

func NewLogger(path string) (*Logger, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("audit: create dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("audit: open file: %w", err)
	}

	return &Logger{file: f}, nil
}

func (l *Logger) Write(entry Entry) error {
	if entry.Metadata != nil {
		raw, err := json.Marshal(entry.Metadata)
		if err != nil {
			return fmt.Errorf("audit: marshal metadata: %w", err)
		}
		if len(raw) > maxMetadataSize {
			return fmt.Errorf("audit: metadata exceeds %d bytes", maxMetadataSize)
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
		return fmt.Errorf("audit: marshal entry: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, err := l.file.Write(append(line, '\n')); err != nil {
		return err
	}
	return l.file.Sync()
}

func (l *Logger) Close() error {
	return l.file.Close()
}
