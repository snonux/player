package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const tokenByteLength = 32

// Compile-time check that *tokenManager satisfies the TokenManager interface.
var _ TokenManager = (*tokenManager)(nil)

type tokenManager struct{}

// NewTokenManager creates a TokenManager.
func NewTokenManager() TokenManager {
	return &tokenManager{}
}

// Generate creates a plaintext API token and its stored hash.
func (m *tokenManager) Generate() (plaintext, hash string) {
	b := make([]byte, tokenByteLength)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("generate token: %v", err))
	}

	plaintext = hex.EncodeToString(b)
	return plaintext, m.Hash(plaintext)
}

// Hash returns the SHA-256 hash of a plaintext API token.
func (m *tokenManager) Hash(plaintext string) string {
	// SHA-256 is used instead of bcrypt because API tokens are high-entropy
	// random bytes; bcrypt adds CPU cost without meaningful security benefit.
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
