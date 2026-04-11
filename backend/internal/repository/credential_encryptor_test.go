package repository

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// minimalConfig returns a config.Config with only the fields needed for
// NewCredentialEncryptor tests populated.
func minimalConfig(mode, credKey string) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Mode: mode,
		},
		Security: config.SecurityConfig{
			CredentialEncryptionKey: credKey,
		},
	}
}

// TestNewCredentialEncryptor_ReleaseMode_EmptyKey verifies that an error is
// returned when the server is in release mode and no encryption key is set.
func TestNewCredentialEncryptor_ReleaseMode_EmptyKey(t *testing.T) {
	cfg := minimalConfig("release", "")

	enc, err := NewCredentialEncryptor(cfg)
	if err == nil {
		t.Fatal("expected error for release mode with empty key, got nil")
	}
	if enc != nil {
		t.Fatalf("expected nil encryptor, got %v", enc)
	}

	// Error message must be actionable.
	if !strings.Contains(err.Error(), "release mode") {
		t.Errorf("error should mention 'release mode': %v", err)
	}
	if !strings.Contains(err.Error(), "openssl rand -hex 32") {
		t.Errorf("error should include key generation command: %v", err)
	}
	if !strings.Contains(err.Error(), "SECURITY_CREDENTIAL_ENCRYPTION_KEY") {
		t.Errorf("error should mention the env var name: %v", err)
	}
}

// TestNewCredentialEncryptor_NonReleaseMode_EmptyKey verifies that (nil, nil)
// is returned when the key is empty but the mode is not "release".
func TestNewCredentialEncryptor_NonReleaseMode_EmptyKey(t *testing.T) {
	modes := []string{"debug", "standard", "development", ""}
	for _, mode := range modes {
		t.Run("mode="+mode, func(t *testing.T) {
			cfg := minimalConfig(mode, "")

			enc, err := NewCredentialEncryptor(cfg)
			if err != nil {
				t.Fatalf("mode=%q: expected nil error, got %v", mode, err)
			}
			if enc != nil {
				t.Fatalf("mode=%q: expected nil encryptor, got %v", mode, enc)
			}
		})
	}
}

// TestNewCredentialEncryptor_ValidKey verifies that a valid 32-byte hex key
// produces a working encryptor regardless of mode.
func TestNewCredentialEncryptor_ValidKey(t *testing.T) {
	// 64 hex chars = 32 bytes.
	validKey := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

	for _, mode := range []string{"debug", "release"} {
		t.Run("mode="+mode, func(t *testing.T) {
			cfg := minimalConfig(mode, validKey)

			enc, err := NewCredentialEncryptor(cfg)
			if err != nil {
				t.Fatalf("mode=%q: unexpected error: %v", mode, err)
			}
			if enc == nil {
				t.Fatalf("mode=%q: expected non-nil encryptor", mode)
			}

			// Quick round-trip smoke test.
			plain := map[string]any{"api_key": "test-value"}
			encrypted, err := enc.EncryptCredentials(plain)
			if err != nil {
				t.Fatalf("EncryptCredentials: %v", err)
			}
			if _, ok := encrypted["_encrypted"]; !ok {
				t.Fatal("encrypted map missing _encrypted key")
			}
			decrypted, err := enc.DecryptCredentials(encrypted)
			if err != nil {
				t.Fatalf("DecryptCredentials: %v", err)
			}
			if decrypted["api_key"] != "test-value" {
				t.Errorf("round-trip mismatch: got %v", decrypted)
			}
		})
	}
}

// TestNewCredentialEncryptor_InvalidKey verifies that a malformed key returns
// an error in all modes.
func TestNewCredentialEncryptor_InvalidKey(t *testing.T) {
	cfg := minimalConfig("debug", "not-valid-hex!")

	enc, err := NewCredentialEncryptor(cfg)
	if err == nil {
		t.Fatal("expected error for invalid key, got nil")
	}
	if enc != nil {
		t.Fatalf("expected nil encryptor, got %v", enc)
	}
}
