package llmsettings

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"os"

	"golang.org/x/crypto/scrypt"
)

// lEnvGet — os.Getenv (indirected for testability).
func lEnvGet(name string) string { return os.Getenv(name) }

func scryptKey(password, salt []byte, n, r, p int, keyLen int) ([]byte, error) {
	return scrypt.Key(password, salt, n, r, p, keyLen)
}

func aesGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func b64e(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func b64d(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
