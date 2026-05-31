package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Store provides CRUD operations for APITokens, backed by a JSON file.
type Store struct {
	mu   sync.RWMutex
	path string
}

// NewStore creates a new token store persisting to the given file path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) load() ([]APIToken, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []APIToken{}, nil
		}
		return nil, err
	}

	var tokens []APIToken
	if err := json.Unmarshal(data, &tokens); err != nil {
		if len(data) == 0 {
			return []APIToken{}, nil
		}
		return nil, err
	}

	return tokens, nil
}

func (s *Store) save(tokens []APIToken) error {
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "tokens.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	if err := os.Chmod(tmpName, 0600); err != nil {
		// Log error, but proceed
	}

	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return err
	}

	return nil
}

// Create adds a new token to the store.
func (s *Store) Create(token *APIToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokens, err := s.load()
	if err != nil {
		return err
	}

	for _, t := range tokens {
		if t.Name == token.Name {
			return ErrDuplicateName
		}
	}

	tokens = append(tokens, *token)
	return s.save(tokens)
}

// GetByID returns a token by its ID.
func (s *Store) GetByID(id string) (*APIToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tokens, err := s.load()
	if err != nil {
		return nil, err
	}

	for _, t := range tokens {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, ErrTokenNotFound
}

func (s *Store) GetByName(name string) (*APIToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tokens, err := s.load()
	if err != nil {
		return nil, err
	}

	for _, t := range tokens {
		if t.Name == name {
			return &t, nil
		}
	}
	return nil, ErrTokenNotFound
}

func (s *Store) List() ([]APIToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tokens, err := s.load()
	if err != nil {
		return nil, err
	}
	return tokens, nil
}

func (s *Store) Update(token *APIToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokens, err := s.load()
	if err != nil {
		return err
	}

	found := false
	for i, t := range tokens {
		if t.ID == token.ID {
			if t.Name != token.Name {
				for _, other := range tokens {
					if other.ID != token.ID && other.Name == token.Name {
						return ErrDuplicateName
					}
				}
			}
			tokens[i] = *token
			found = true
			break
		}
	}

	if !found {
		return ErrTokenNotFound
	}

	return s.save(tokens)
}

func (s *Store) SoftDelete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokens, err := s.load()
	if err != nil {
		return err
	}

	found := false
	for i, t := range tokens {
		if t.ID == id {
			tokens[i].Enabled = false
			found = true
			break
		}
	}

	if !found {
		return ErrTokenNotFound
	}

	return s.save(tokens)
}

