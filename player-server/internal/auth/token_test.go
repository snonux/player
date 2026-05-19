package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestTokenManager_Generate(t *testing.T) {
	tm := NewTokenManager()
	seen := make(map[string]string)

	for i := 0; i < 1000; i++ {
		plaintext, hash, err := tm.Generate()
		if err != nil {
			t.Fatalf("generate token: %v", err)
		}
		if len(plaintext) != tokenByteLength*2 {
			t.Fatalf("expected plaintext length %d, got %d", tokenByteLength*2, len(plaintext))
		}
		decoded, err := hex.DecodeString(plaintext)
		if err != nil {
			t.Fatalf("expected hex plaintext: %v", err)
		}
		if len(decoded) != tokenByteLength {
			t.Fatalf("expected %d decoded bytes, got %d", tokenByteLength, len(decoded))
		}
		if hash != tm.Hash(plaintext) {
			t.Fatal("expected hash to match plaintext")
		}
		if previousHash, ok := seen[plaintext]; ok {
			t.Fatalf("duplicate token generated with hashes %q and %q", previousHash, hash)
		}
		seen[plaintext] = hash
	}
}

func TestTokenManager_Hash(t *testing.T) {
	tm := NewTokenManager()
	tests := []struct {
		name      string
		plaintext string
	}{
		{"empty token", ""},
		{"hex token", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{"plain string", "api-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := tm.Hash(tt.plaintext)
			if hash == "" {
				t.Fatal("expected non-empty hash")
			}
			if len(hash) != sha256.Size*2 {
				t.Fatalf("expected hash length %d, got %d", sha256.Size*2, len(hash))
			}
			if _, err := hex.DecodeString(hash); err != nil {
				t.Fatalf("expected hex hash: %v", err)
			}
			if hash != tm.Hash(tt.plaintext) {
				t.Fatal("expected deterministic hash")
			}
			if tt.plaintext != "" && hash == tm.Hash(tt.plaintext+"x") {
				t.Fatal("expected different plaintext to have different hash")
			}
		})
	}
}
