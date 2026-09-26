package dropbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
)

// Cipher port of services/web/modules/dropbox/app/src/DropboxCredentials.mjs.
//
// Storage layout (Node): token = base64( iv12 ‖ ctBytes ‖ tag16 ) where
// ctBytes = Buffer.from(base64string(plaintext-ct), 'base64') = the RAW ct.
// Node's decrypt side passes those embedded bytes through decipher with
// inputEncoding 'base64' — Node first does buf.toString('base64') and then
// base64-decodes, an identity — so plain GCM.Open over the raw ct is the
// faithful port and the pair round-trips symmetrically on both stacks
// under the same env keys.

const dbxIVLen = 12
const dbxTagLen = 16

// dbxEncryptionKey — the current AES-256-GCM key. Node's getEncryptionKey:
//
//	secret = process.env['WEBDAV_TOKEN_CIPHER_PASSWORD']
//	if (secret) return sha256('overleaf-dropbox-credentials-v2|' + secret)
//	fallback(process.env['SECRET_TOKEN'])
//
// both envs absent → throws: the pinned 500 body message.
func dbxEncryptionKey() ([]byte, error) {
	pw := os.Getenv("WEBDAV_TOKEN_CIPHER_PASSWORD")
	if pw != "" {
		sum := sha256.Sum256([]byte("overleaf-dropbox-credentials-v2|" + pw))
		return sum[:], nil
	}
	st := os.Getenv("SECRET_TOKEN")
	if st != "" {
		sum := sha256.Sum256([]byte("overleaf-dropbox-credentials-secret-token-fallback|" + st))
		return sum[:], nil
	}
	return nil, &dbxCipherError{msg: "No encryption secret available for Dropbox credentials (set WEBDAV_TOKEN_CIPHER_PASSWORD or SECRET_TOKEN)"}
}

type dbxCipherError struct{ msg string }

func (e *dbxCipherError) Error() string { return e.msg }

// dbxEncryptToken — Node encryptToken (base64 in → base64 token out).
func dbxEncryptToken(plaintext string) (string, error) {
	if plaintext == "" {
		return "", &dbxCipherError{msg: "Invalid token for encryption"}
	}
	key, err := dbxEncryptionKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	iv := make([]byte, dbxIVLen)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, iv, []byte(plaintext), nil) // ct ‖ tag
	// Node: Buffer.concat([iv, Buffer.from(ctb64, 'base64') /* = raw ct */, authTag])
	//       .toString('base64')  →  stored = iv ‖ ct ‖ tag
	stored := make([]byte, 0, dbxIVLen+len(sealed))
	stored = append(stored, iv...)
	stored = append(stored, sealed...)
	return base64.StdEncoding.EncodeToString(stored), nil
}

// dbxDecryptToken — Node decryptToken: try [current, legacyNodeEnv,
// legacyRaw] keys. Node's decrypt reads the embedded bytes with input
// encoding 'base64' — but Node first does buffer.toString('base64') and
// then base64-decodes: an IDENTITY, so the embedded bytes are the RAW
// ciphertext and plain GCM.Open is the faithful port. Returns the
// plaintext or the exact wrapped error message.
func dbxDecryptToken(storedToken string) (string, error) {
	encrypted, err := base64.StdEncoding.DecodeString(storedToken)
	if err != nil || len(encrypted) < 16 {
		return "", &dbxCipherError{msg: "Invalid encrypted data format"}
	}
	iv := encrypted[:dbxIVLen]
	tag := encrypted[len(encrypted)-dbxTagLen:]
	var payload []byte
	if len(encrypted) >= dbxIVLen+dbxTagLen {
		payload = encrypted[dbxIVLen : len(encrypted)-dbxTagLen]
	}
	// (Node subarray clamps ct to empty when the token is 16..27 bytes —
	// mirrored by the empty-payload branch.)
	var lastErr error
	for _, key := range dbxCandidateKeys() {
		block, berr := aes.NewCipher(key)
		if berr != nil {
			lastErr = berr
			continue
		}
		gcm, gerr := cipher.NewGCM(block)
		if gerr != nil {
			lastErr = gerr
			continue
		}
		out, aerr := gcm.Open(nil, iv, append(append([]byte{}, payload...), tag...), nil)
		if aerr == nil {
			return string(out), nil
		}
		lastErr = aerr
	}
	_ = lastErr
	return "", &dbxCipherError{msg: "Decryption failed. Invalid token or encryption key."}
}

func dbxCandidateKeys() [][]byte {
	var keys [][]byte
	if cur, err := dbxEncryptionKey(); err == nil {
		keys = append(keys, cur)
	}
	keys = append(keys, dbxLegacyNodeEnvKey(), dbxLegacyRawKey())
	return keys
}

// dbxLegacyNodeEnvKey — Buffer.from(('overleaf-dropbox-credentials-v2|' +
// (NODE_ENV||'development')).padEnd(32,'x').slice(0,32), 'utf8').
func dbxLegacyNodeEnvKey() []byte {
	env := os.Getenv("NODE_ENV")
	if env == "" {
		env = "development"
	}
	s := "overleaf-dropbox-credentials-v2|" + env
	s = pad32(s)
	return []byte(s)
}

// dbxLegacyRawKey — Buffer.from(((NODE_ENV||'development')).padEnd(32,'x').
// slice(0,32), 'utf8').
func dbxLegacyRawKey() []byte {
	env := os.Getenv("NODE_ENV")
	if env == "" {
		env = "development"
	}
	return []byte(pad32(env))
}

func pad32(s string) string {
	if len(s) >= 32 {
		return s[:32]
	}
	return s + strings.Repeat("x", 32-len(s))
}
