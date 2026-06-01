package invite

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const storeFileName = "invites.jsonl"

type Store struct {
	path  string
	file  *os.File
	index map[string]int64
	mu    sync.Mutex
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("mkdir invites: %w", err)
	}

	path := filepath.Join(dir, storeFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("open invites store: %w", err)
	}

	s := &Store{
		path:  path,
		file:  f,
		index: make(map[string]int64),
	}

	if err := s.buildIndex(); err != nil {
		f.Close()
		return nil, fmt.Errorf("build index: %w", err)
	}

	return s, nil
}

func (s *Store) buildIndex() error {
	var offset int64 = 0
	scanner := bufio.NewScanner(s.file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			offset += int64(len(line)) + 1
			continue
		}

		var inv InviteLink
		if err := json.Unmarshal([]byte(line), &inv); err == nil && inv.TokenHash != "" {
			s.index[inv.TokenHash] = offset
		}
		offset += int64(len(line)) + 1
	}

	return scanner.Err()
}

func (s *Store) Append(inv *InviteLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(inv)
	if err != nil {
		return fmt.Errorf("marshal invite: %w", err)
	}
	data = append(data, '\n')

	offset, err := s.file.Seek(0, 2)
	if err != nil {
		return fmt.Errorf("seek end: %w", err)
	}

	if _, err := s.file.Write(data); err != nil {
		return fmt.Errorf("write invite: %w", err)
	}

	if inv.TokenHash != "" {
		s.index[inv.TokenHash] = offset
	}

	return nil
}

func (s *Store) FindByTokenHash(hash string) (*InviteLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	offset, ok := s.index[hash]
	if !ok {
		return nil, nil
	}

	if _, err := s.file.Seek(offset, 0); err != nil {
		return nil, fmt.Errorf("seek to offset: %w", err)
	}

	scanner := bufio.NewScanner(s.file)
	if !scanner.Scan() {
		return nil, nil
	}

	var inv InviteLink
	if err := json.Unmarshal([]byte(scanner.Text()), &inv); err != nil {
		return nil, fmt.Errorf("unmarshal invite: %w", err)
	}

	return &inv, nil
}

func (s *Store) GC() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.file.Close(); err != nil {
		return fmt.Errorf("close for gc: %w", err)
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read for gc: %w", err)
	}

	var kept [][]byte
	now := time.Now()
	scanner := bufio.NewScanner(bytes.NewReader(data))
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var inv InviteLink
		if err := json.Unmarshal(line, &inv); err != nil {
			continue
		}

		expired := !inv.ExpiresAt.IsZero() && now.After(inv.ExpiresAt)
		consumed := inv.MaxUses > 0 && inv.UseCount >= inv.MaxUses

		if !expired && !consumed {
			kept = append(kept, append([]byte{}, line...))
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan during gc: %w", err)
	}

	f, err := os.Create(s.path)
	if err != nil {
		return fmt.Errorf("create after gc: %w", err)
	}
	defer f.Close()

	clear(s.index)
	var offset int64 = 0
	for _, line := range kept {
		var inv InviteLink
		_ = json.Unmarshal(line, &inv)
		if inv.TokenHash != "" {
			s.index[inv.TokenHash] = offset
		}
		f.Write(line)
		f.Write([]byte("\n"))
		offset += int64(len(line)) + 1
	}

	s.file = f
	return nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}
