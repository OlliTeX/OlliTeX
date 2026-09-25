// Package configstore — field-level encryption (D6, 2026-09-25).
//
// Values written while an encryption key is active are stored as
// "enc:v1:<base64(nonce||ciphertext||tag)>" (AES-256-GCM). Values written
// before encryption (legacy plaintext) keep reading unchanged: Get/All/Dump
// pass non-prefixed rows through untouched. Re-setting such a key while a
// key is active transparently re-encrypts it.
//
// The key itself NEVER lives in the database. It is supplied either via
// NewWithKey (programs/tests) or the CONFIG_DB_ENCRYPTION_KEY env var
// (hex-64 or base64 44 chars; a 32-byte key). Without a key the store
// behaves exactly as before (plaintext).
package configstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
)

// encPrefix marks an encrypted row value.
const encPrefix = "enc:v1:"

// EncryptionKeyEnv is the (only) env parameter the owner keeps outside the
// database: the config-DB field-encryption key (D2/Q2, 2026-09-25).
const EncryptionKeyEnv = "CONFIG_DB_ENCRYPTION_KEY"

// GenerateKey returns a fresh 32-byte key encoded as lowercase hex (64 chars),
// suitable for the operator to place in the container environment.
func GenerateKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("configstore: generate key: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// ParseKey accepts a hex (64) or base64 (44) 32-byte key.
func ParseKey(raw string) ([]byte, error) {
	if len(raw) == 64 {
		b, err := hex.DecodeString(raw)
		if err == nil && len(b) == 32 {
			return b, nil
		}
	}
	if len(raw) == 44 {
		b, err := base64.StdEncoding.DecodeString(raw)
		if err == nil && len(b) == 32 {
			return b, nil
		}
	}
	return nil, fmt.Errorf("configstore: encryption key must be 32 bytes (hex-64 or base64-44), got %d chars", len(raw))
}

// KeyFromEnv reads CONFIG_DB_ENCRYPTION_KEY; empty/absent == no encryption.
func KeyFromEnv() ([]byte, error) {
	raw := os.Getenv(EncryptionKeyEnv)
	if raw == "" {
		return nil, nil
	}
	return ParseKey(raw)
}

// Seal encrypts plaintext with AES-256-GCM and returns the stored form
// (encPrefix + base64(nonce||ct||tag)).
func Seal(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("configstore: seal: %w", err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, 12)
	if err != nil {
		return "", fmt.Errorf("configstore: seal gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("configstore: seal nonce: %w", err)
	}
	return encPrefix + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), nil)), nil
}

// Open decodes a stored row value. Non-prefixed (legacy plaintext) values are
// returned unchanged; prefixed values are decrypted (tamper/invalid → error).
func Open(key []byte, stored string) (string, error) {
	if !hasPrefix(stored, encPrefix) {
		return stored, nil
	}
	if key == nil {
		return "", fmt.Errorf("configstore: value is encrypted but no key is configured (%s)", EncryptionKeyEnv)
	}
	raw, err := base64.StdEncoding.DecodeString(stored[len(encPrefix):])
	if err != nil {
		return "", fmt.Errorf("configstore: open: bad encoding: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("configstore: open: %w", err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, 12)
	if err != nil {
		return "", fmt.Errorf("configstore: open gcm: %w", err)
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("configstore: open: ciphertext too short")
	}
	pt, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("configstore: open: decrypt failed (wrong key or tampered value)")
	}
	return string(pt), nil
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
