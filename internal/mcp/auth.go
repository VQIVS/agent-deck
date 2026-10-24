package mcp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// Identity is resolved from a transport credential, never tool arguments.
type Identity struct {
	Team   string
	Member string
}

// Tokens stores only credential hashes and supports immediate rotation/revocation.
type Tokens struct {
	mu     sync.RWMutex
	values map[[32]byte]Identity
}

func NewTokens() *Tokens {
	return &Tokens{values: make(map[[32]byte]Identity)}
}

func (t *Tokens) Issue(identity Identity, isolated bool) (string, error) {
	if identity.Team == "" || identity.Member == "" || !isolated {
		return "", fmt.Errorf("member credentials require an isolated directory and identity")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate member credential: %w", err)
	}
	token := hex.EncodeToString(b)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.revoke(identity)
	t.values[sha256.Sum256([]byte(token))] = identity
	return token, nil
}

func (t *Tokens) Revoke(identity Identity) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.revoke(identity)
}

func (t *Tokens) revoke(identity Identity) {
	for hash, existing := range t.values {
		if existing == identity {
			delete(t.values, hash)
		}
	}
}

func (t *Tokens) Authenticate(token string) (Identity, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	identity, ok := t.values[sha256.Sum256([]byte(token))]
	return identity, ok
}
