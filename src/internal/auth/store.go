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

func (s *Store) load() (map[string]json.RawMessage, []APIToken, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]json.RawMessage), []APIToken{}, nil
		}
		return nil, nil, err
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, nil, err
	}

	var tokens []APIToken
	if tokensRaw, ok := root["apiTokens"]; ok {
		if err := json.Unmarshal(tokensRaw, &tokens); err != nil {
			return nil, nil, err
		}
	} else {
		tokens = []APIToken{}
	}

	return root, tokens, nil
}

func (s *Store) save(root map[string]json.RawMessage, tokens []APIToken) error {
	tokensRaw, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	root["apiTokens"] = tokensRaw

	data, err := json.MarshalIndent(root, "", "  ")
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

	root, tokens, err := s.load()
	if err != nil {
		return err
	}

	for _, t := range tokens {
		if t.Name == token.Name {
			return ErrDuplicateName
		}
	}

	tokens = append(tokens, *token)
	return s.save(root, tokens)
}

// GetByID returns a token by its ID.
func (s *Store) GetByID(id string) (*APIToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, tokens, err := s.load()
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

// GetByName returns a token by its Name.
func (s *Store) GetByName(name string) (*APIToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, tokens, err := s.load()
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

// List returns all tokens.
func (s *Store) List() ([]APIToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, tokens, err := s.load()
	if err != nil {
		return nil, err
	}
	return tokens, nil
}

// Update saves modifications to an existing token.
func (s *Store) Update(token *APIToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	root, tokens, err := s.load()
	if err != nil {
		return err
	}

	found := false
	for i, t := range tokens {
		if t.ID == token.ID {
			// Ensure name uniqueness if name changed
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

	return s.save(root, tokens)
}

// SoftDelete marks a token as disabled instead of physically removing it.
func (s *Store) SoftDelete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	root, tokens, err := s.load()
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

	return s.save(root, tokens)
}
