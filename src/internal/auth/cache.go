package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"sync"
	"time"
)

type cacheEntry struct {
	tokenID   string
	tokenName string
	scope     Scope
	hash      string
	sha256Sum [sha256.Size]byte
	enabled   bool
	expiresAt *time.Time
	cachedAt  time.Time
}

type TokenCache struct {
	mu       sync.RWMutex
	entries  map[string]*cacheEntry
	ttl      time.Duration
	store    *Store
	bcryptSem chan struct{}
}

func NewTokenCache(store *Store, ttl time.Duration) *TokenCache {
	if ttl == 0 {
		ttl = 5 * time.Minute
	}
	return &TokenCache{
		entries:   make(map[string]*cacheEntry),
		ttl:       ttl,
		store:     store,
		bcryptSem: make(chan struct{}, 2),
	}
}

type TokenValidationResult struct {
	TokenID   string
	TokenName string
	Scope     Scope
}

func (c *TokenCache) Validate(plainToken string) (*TokenValidationResult, error) {
	prefix := plainToken
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}

	tokenSum := sha256.Sum256([]byte(plainToken))

	c.mu.RLock()
	if entry, ok := c.entries[prefix]; ok {
		if time.Since(entry.cachedAt) < c.ttl && entry.enabled {
			if subtle.ConstantTimeCompare(tokenSum[:], entry.sha256Sum[:]) == 1 {
				c.mu.RUnlock()
				return &TokenValidationResult{
					TokenID:   entry.tokenID,
					TokenName: entry.tokenName,
					Scope:     entry.scope,
				}, nil
			}
		}
	}
	c.mu.RUnlock()

	tokens, err := c.store.List()
	if err != nil {
		return nil, err
	}

	for i := range tokens {
		t := &tokens[i]
		if !t.Enabled {
			continue
		}
		if t.ExpiresAt != nil && t.ExpiresAt.Before(time.Now()) {
			continue
		}
		if c.verifyBcrypt(plainToken, t.TokenHash) {
			c.mu.Lock()
			c.entries[t.TokenPrefix] = &cacheEntry{
				tokenID:   t.ID,
				tokenName: t.Name,
				scope:     t.Scope,
				hash:      t.TokenHash,
				sha256Sum: tokenSum,
				enabled:   t.Enabled,
				expiresAt: t.ExpiresAt,
				cachedAt:  time.Now(),
			}
			c.mu.Unlock()

			return &TokenValidationResult{
				TokenID:   t.ID,
				TokenName: t.Name,
				Scope:     t.Scope,
			}, nil
		}
	}

	return nil, ErrTokenNotFound
}

func (c *TokenCache) Store() *Store { return c.store }

func (c *TokenCache) verifyBcrypt(plainToken, hash string) bool {
	c.bcryptSem <- struct{}{}
	defer func() { <-c.bcryptSem }()
	return VerifyToken(plainToken, hash)
}

func (c *TokenCache) Invalidate(tokenPrefix string) {
	c.mu.Lock()
	delete(c.entries, tokenPrefix)
	c.mu.Unlock()
}

func (c *TokenCache) InvalidateAll() {
	c.mu.Lock()
	c.entries = make(map[string]*cacheEntry)
	c.mu.Unlock()
}
