// Package webdav implements the Node services/web/modules/webdav route
// surface (the /user/webdav/* + /project/:id/webdav/* + /project/new/webdav
// endpoints) on the Go web service.
//
// This file: the per-provider TOKEN cipher. Node module WebdavCredentials →
// WebdavTokenEncryption: AccessTokenEncryptor (the SAME shared V3 scheme the
// site-settings/zotero/mendeley ciphers use — AES-256-CTR + HKDF-SHA512,
// label-prefixed base64 string), but a DIFFERENT key + label:
//
//	label      = WEBDAV_TOKEN_CIPHER_LABEL || "OL_WEBDAV-v3"
//	password   = WEBDAV_TOKEN_CIPHER_PASSWORD env (raw string), else the key
//	             file WEBDAV_TOKEN_CIPHER_FILE ||
//	             /var/lib/overleaf/data/.webdav-token-cipher.json — JSON
//	             {cipherLabel, cipherPasswords{label: base64(32B)}}
//
// A token encrypted under this key is NOT decryptable with the zotero or
// mendeley key (and vice versa).
//
// Node creates the key file on first use when absent (random 32B, base64,
// mode 0600) — Go emulates the identical creation path.

package webdav

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path"
	"strings"
	"sync"

	"ollitex/go/services/web/features/sitesettings"
)

const (
	wdDefaultLabel = "OL_WEBDAV-v3"
	wdDefaultKey   = "/var/lib/overleaf/data/.webdav-token-cipher.json"
)

type wdCipherBridge interface {
	EncryptRaw(plain string) (string, error)
	DecryptRaw(tok string) (string, bool)
}

var (
	wdCipherOnce sync.Once
	wdCipher     wdCipherBridge
	wdLabel      string // active label (scheme selection, Node parity)
)

func wdCipherInst() wdCipherBridge {
	wdCipherOnce.Do(func() { wdCipher, wdLabel = resolveWdCipher() })
	return wdCipher
}

// Test/diagnostic reset (package-private).
func resetWdCipherForTest() {
	wdCipherOnce = sync.Once{}
	wdCipher = nil
	wdLabel = ""
}

func resolveWdCipher() (wdCipherBridge, string) {
	label := strings.TrimSpace(os.Getenv("WEBDAV_TOKEN_CIPHER_LABEL"))
	if label == "" {
		label = wdDefaultLabel
	}

	if pw := strings.TrimSpace(os.Getenv("WEBDAV_TOKEN_CIPHER_PASSWORD")); pw != "" {
		return sitesettings.OpenProvider(label, []byte(pw)), label
	}

	file := strings.TrimSpace(os.Getenv("WEBDAV_TOKEN_CIPHER_FILE"))
	if file == "" {
		file = wdDefaultKey
	}

	if raw, err := os.ReadFile(file); err == nil {
		var doc struct {
			CipherLabel     string            `json:"cipherLabel"`
			CipherPasswords map[string]string `json:"cipherPasswords"`
		}
		if json.Unmarshal(raw, &doc) == nil {
			useLabel := doc.CipherLabel
			if useLabel == "" {
				useLabel = label
			}
			for _, k := range []string{useLabel, label} {
				if pw, ok := doc.CipherPasswords[k]; ok && pw != "" {
					return sitesettings.OpenProvider(k, []byte(pw)), k
				}
			}
		}
		// Unreadable/foreign file: fall through to creation (Node would
		// have thrown — the gate never lands here with a bad file).
	}

	key := make([]byte, 32)
	if _, rerr := rand.Read(key); rerr != nil {
		// rand.Read failure is not recoverable in-process; keep a stable
		// zero key rather than panic the whole web service.
		key = make([]byte, 32)
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	doc := struct {
		CipherLabel     string            `json:"cipherLabel"`
		CipherPasswords map[string]string `json:"cipherPasswords"`
	}{CipherLabel: label, CipherPasswords: map[string]string{label: encoded}}
	if b, jerr := json.Marshal(doc); jerr == nil {
		_ = os.MkdirAll(path.Dir(file), 0o755)
		_ = os.WriteFile(file, b, 0o600)
	}
	return sitesettings.OpenProvider(label, []byte(encoded)), label
}

// wdEncrypt/Decrypt — the raw-payload bridge (plaintext is a JSON object,
// NOT a JSON-quoted string — same as zotero/mendeley credentials). Node
// selects the scheme by the TOKEN'S label ("unknown access-token-encryptor
// label" on mismatch) → enforce the active label before delegating.
func wdEncrypt(plain string) (string, error) {
	ci := wdCipherInst()
	if ci == nil {
		return "", errWdCipher
	}
	return ci.EncryptRaw(plain)
}

func wdDecrypt(tok string) (string, bool) {
	ci := wdCipherInst()
	if ci == nil {
		return "", false
	}
	lab := wdLabel
	if lab == "" {
		lab = wdDefaultLabel
	}
	if !strings.HasPrefix(tok, lab+":") {
		return "", false
	}
	return ci.DecryptRaw(tok)
}

var errWdCipher = wdErr("webdav cipher unavailable")

type wdErr string

func (e wdErr) Error() string { return string(e) }
