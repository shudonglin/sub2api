package repository

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// credentialEncryptionVersion is the current version of the encryption envelope.
// Bumping this version allows future migration to a different algorithm or key.
const credentialEncryptionVersion = 1

// CredentialEncryptor encrypts and decrypts account credentials (map[string]any)
// using AES-256-GCM via the existing AESEncryptor.
//
// Storage format: when encryption is active, the JSONB column contains
//
//	{"_encrypted": "<base64-ciphertext>", "_v": 1}
//
// The decrypt path is backwards-compatible: if the map does NOT contain
// the "_encrypted" key it is treated as legacy plaintext and returned as-is.
//
// TODO: proxy passwords and JWT secrets should also be encrypted at rest
// as a follow-up improvement.
type CredentialEncryptor struct {
	enc *AESEncryptor
}

// NewCredentialEncryptor creates a CredentialEncryptor from the config.
// If the credential encryption key is not configured and the server is running
// in release mode, an error is returned — plaintext credential storage is not
// permitted in production.  In non-release modes a warning is logged and
// (nil, nil) is returned to enable the plaintext fallback path.
func NewCredentialEncryptor(cfg *config.Config) (*CredentialEncryptor, error) {
	key := cfg.Security.CredentialEncryptionKey
	if key == "" {
		if cfg.Server.Mode == "release" {
			return nil, fmt.Errorf(
				"security.credential_encryption_key is required in release mode but is not set — " +
					"provider credentials cannot be stored in PLAINTEXT in production. " +
					"Generate a key with: openssl rand -hex 32 " +
					"and set it via the SECURITY_CREDENTIAL_ENCRYPTION_KEY environment variable",
			)
		}
		slog.Warn("security.credential_encryption_key is not set — provider credentials will be stored in PLAINTEXT. " +
			"Generate a key with 'openssl rand -hex 32' and set SECURITY_CREDENTIAL_ENCRYPTION_KEY.")
		return nil, nil
	}

	// Reuse the AESEncryptor constructor logic but with the credential key.
	enc, err := newAESEncryptorFromHex(key)
	if err != nil {
		return nil, fmt.Errorf("credential encryption key: %w", err)
	}
	return &CredentialEncryptor{enc: enc}, nil
}

// EncryptCredentials serialises the plaintext credentials map to JSON,
// encrypts the blob, and returns a wrapper map suitable for JSONB storage.
// If the encryptor is nil (disabled), returns the original map unchanged.
func (c *CredentialEncryptor) EncryptCredentials(plain map[string]any) (map[string]any, error) {
	if c == nil || c.enc == nil {
		return plain, nil
	}
	if len(plain) == 0 {
		return plain, nil
	}
	// Do not double-encrypt.
	if _, ok := plain["_encrypted"]; ok {
		return plain, nil
	}

	blob, err := json.Marshal(plain)
	if err != nil {
		return nil, fmt.Errorf("marshal credentials for encryption: %w", err)
	}

	ciphertext, err := c.enc.Encrypt(string(blob))
	if err != nil {
		return nil, fmt.Errorf("encrypt credentials: %w", err)
	}

	return map[string]any{
		"_encrypted": ciphertext,
		"_v":         credentialEncryptionVersion,
	}, nil
}

// DecryptCredentials detects whether the credentials map is an encrypted
// envelope or legacy plaintext. Encrypted envelopes are decrypted and
// deserialised back to map[string]any; plaintext maps are returned as-is.
// If the encryptor is nil (disabled) and the map IS encrypted, an error
// is returned because the data cannot be read without a key.
func (c *CredentialEncryptor) DecryptCredentials(stored map[string]any) (map[string]any, error) {
	if len(stored) == 0 {
		return stored, nil
	}

	ciphertext, isEncrypted := stored["_encrypted"]
	if !isEncrypted {
		// Legacy plaintext — return as-is.
		return stored, nil
	}

	if c == nil || c.enc == nil {
		return nil, fmt.Errorf("credentials are encrypted but no credential_encryption_key is configured — cannot decrypt")
	}

	ct, ok := ciphertext.(string)
	if !ok {
		return nil, fmt.Errorf("invalid _encrypted field type: expected string, got %T", ciphertext)
	}

	plainJSON, err := c.enc.Decrypt(ct)
	if err != nil {
		return nil, fmt.Errorf("decrypt credentials: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(plainJSON), &result); err != nil {
		return nil, fmt.Errorf("unmarshal decrypted credentials: %w", err)
	}
	return result, nil
}

// IsEncrypted reports whether a credentials map is in encrypted envelope form.
func IsEncrypted(creds map[string]any) bool {
	_, ok := creds["_encrypted"]
	return ok
}
