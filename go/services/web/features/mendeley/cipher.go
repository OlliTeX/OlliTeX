package mendeley

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"path"
	"strings"
	"sync"

	"ollitex/go/services/web/features/sitesettings"
)

// Node mendeley AccessTokenEncryptorHelper (per-provider cipher):
//
//	password  = MENDELEY_CIPHER_PASSWORD env, else the key file
//	            /var/lib/overleaf/data/.mendeley-cipher-key — read if it
//	            exists and is ≥16 chars, else written as
//	            crypto.randomBytes(32).toString('base64') mode 0600.
//	label     = MENDELEY_CIPHER_LABEL env, else '2024.1-v3'.
//
// The cipher ALGORITHM is the shared AccessTokenEncryptor V3 scheme (the
// sitesettings cipher's primitives); only the key + label are per-provider.

const (
	mendeleyKeyFile    = "/var/lib/overleaf/data/.mendeley-cipher-key"
	mendeleyDefaultLab = "2024.1-v3"
)

type cipherBridge interface {
	EncryptRaw(plain string) (string, error)
	DecryptRaw(tok string) (string, bool)
}

var (
	mCipherOnce sync.Once
	mCipher     cipherBridge
)

func mCipherInst() cipherBridge {
	mCipherOnce.Do(func() {
		mCipher = resolveProviderCipher()
	})
	return mCipher
}

func resolveProviderCipher() cipherBridge {
	pw := strings.TrimSpace(os.Getenv("MENDELEY_CIPHER_PASSWORD"))
	if pw != "" {
		label := strings.TrimSpace(os.Getenv("MENDELEY_CIPHER_LABEL"))
		if label == "" {
			label = mendeleyDefaultLab
		}
		return providerCipher(label, []byte(pw))
	}
	raw, err := os.ReadFile(mendeleyKeyFile)
	if err == nil {
		key := strings.TrimSpace(string(raw))
		if len(key) >= 16 {
			label := strings.TrimSpace(os.Getenv("MENDELEY_CIPHER_LABEL"))
			if label == "" {
				label = mendeleyDefaultLab
			}
			return providerCipher(label, []byte(key))
		}
	}
	key := make([]byte, 32)
	if _, rerr := rand.Read(key); rerr != nil {
		return nil
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	_ = os.MkdirAll(path.Dir(mendeleyKeyFile), 0o755)
	_ = os.WriteFile(mendeleyKeyFile, []byte(encoded), 0o600)
	label := strings.TrimSpace(os.Getenv("MENDELEY_CIPHER_LABEL"))
	if label == "" {
		label = mendeleyDefaultLab
	}
	return providerCipher(label, []byte(encoded))
}

func providerCipher(label string, password []byte) cipherBridge {
	return sitesettings.OpenProvider(label, password)
}
