// Package auth handles authentication and authorization.
package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// Hasher hashes passwords and compares plaintext against hashes.
type Hasher interface {
	Hash(password string) (string, error)
	Compare(hash, password string) error
}

// BCryptHasher implements Hasher using bcrypt.
type BCryptHasher struct {
	cost int
}

// NewBCryptHasher creates a BCryptHasher with the given cost.
func NewBCryptHasher(cost int) *BCryptHasher {
	return &BCryptHasher{cost: cost}
}

// Hash returns a bcrypt hash of the password.
func (b *BCryptHasher) Hash(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), b.cost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(bytes), nil
}

// Compare checks a password against a bcrypt hash.
func (b *BCryptHasher) Compare(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// Compile-time interface check.
var _ Hasher = (*BCryptHasher)(nil)
