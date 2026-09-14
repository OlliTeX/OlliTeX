package sitesettings

import (
	"crypto/aes"
	cipherx "crypto/cipher"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/hkdf"
)

// Secret cipher — byte-compatible with the Node SiteSettings SecretCipher
// (libraries/access-token-encryptor, AccessTokenSchemeV3):
//
//	keyFn(salt)  = HKDF-SHA512(ikm=cipherPassword, salt, info="", L=32)
//	token        = label : hex(salt[16]) : base64(AES-256-CTR(plain)) : hex(iv[16])
//	encryptText(v) = "ss::" + encryptJson(v); decryptText strips "ss::".
//
// Both web (Node + Go) run in the SAME container, so they share the same
// cipher password (TOKEN_CIPHER_PASSWORD env or the .token-cipher.json
// file) and the same label — Go and Node therefore encrypt/decrypt
// each other's secrets interchangeably.

type cipher struct {
	label   string
	password []byte
}

func envNonEmpty(k, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return fallback
}

// loadCipher mirrors SecretCipher.getEncryptorData + label resolution:
//
//	label      = SITE_SETTINGS_CIPHER_LABEL || TOKEN_CIPHER_LABEL || 'OL_CEP-v3'
//	password   = TOKEN_CIPHER_PASSWORD || cipherFile.cipherPasswords[label]
//	cipherFile = SITE_SETTINGS_CIPHER_FILE || TOKEN_CIPHER_FILE
//	             || '/var/lib/overleaf/data/.token-cipher.json'
func loadCipher() (*cipher, error) {
	label := strings.TrimSpace(os.Getenv("SITE_SETTINGS_CIPHER_LABEL"))
	if label == "" {
		label = strings.TrimSpace(os.Getenv("TOKEN_CIPHER_LABEL"))
	}
	if label == "" {
		label = "OL_CEP-v3"
	}
	pw := strings.TrimSpace(os.Getenv("TOKEN_CIPHER_PASSWORD"))
	if pw != "" {
		return &cipher{label: label, password: []byte(pw)}, nil
	}
	file := strings.TrimSpace(os.Getenv("SITE_SETTINGS_CIPHER_FILE"))
	if file == "" {
		file = strings.TrimSpace(os.Getenv("TOKEN_CIPHER_FILE"))
	}
	if file == "" {
		file = "/var/lib/overleaf/data/.token-cipher.json"
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("cipher file unreadable: %w", err)
	}
	var doc struct {
		CipherLabel     string            `json:"cipherLabel"`
		CipherPasswords map[string]string `json:"cipherPasswords"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("cipher file parse: %w", err)
	}
	// label may come from the file (e.g. a different label than the env)
	useLabel := doc.CipherLabel
	if useLabel == "" {
		useLabel = label
	}
	pwv := doc.CipherPasswords[useLabel]
	if pwv == "" {
		pwv = doc.CipherPasswords[label]
	}
	if pwv == "" {
		return nil, fmt.Errorf("cipher password for label %q not found", useLabel)
	}
	return &cipher{label: useLabel, password: []byte(pwv)}, nil
}

func hkdfSHA512(ikm, salt, info []byte, length int) ([]byte, error) {
	h := hkdf.New(sha512.New, ikm, salt, info)
	out := make([]byte, length)
	if _, err := io.ReadFull(h, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cipher) key(salt []byte) ([]byte, error) {
	return hkdfSHA512(c.password, salt, nil, cipherKeyLen)
}

// encrypt mirrors encryptJson(plain): plain is the RAW plaintext string
// (NOT JSON-encoded) — the caller supplies the plaintext and this returns
// the token. (Node wraps the value; here we receive the value.)
func (c *cipher) encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	// Node: encryptJson(JSON.stringify(value)) — for a string value the
	// plaintext is the JSON-quoted form `"<value>"`.
	jsonPlain := jsString(plain)
	salt := make([]byte, cipherSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	iv := make([]byte, cipherIVLen)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	key, err := c.key(salt)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	stream := cipherx.NewCTR(block, iv)
	ct := make([]byte, len(jsonPlain))
	stream.XORKeyStream(ct, []byte(jsonPlain))
	return c.label + ":" + hex.EncodeToString(salt) + ":" +
		base64.StdEncoding.EncodeToString(ct) + ":" + hex.EncodeToString(iv), nil
}

// decrypt mirrors decryptToJson(tok): parses the token, returns the
// plaintext string (JSON.parse of the decrypted JSON-quoted value).
func decryptWith(tok string, keyfunc func(salt []byte) ([]byte, error)) (string, bool) {
	parts := strings.SplitN(tok, ":", 4)
	if len(parts) != 4 {
		return "", false
	}
	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	iv, err := hex.DecodeString(parts[3])
	if err != nil {
		return "", false
	}
	ct, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return "", false
	}
	key, err := keyfunc(salt)
	if err != nil {
		return "", false
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", false
	}
	stream := cipherx.NewCTR(block, iv)
	plain := make([]byte, len(ct))
	stream.XORKeyStream(plain, ct)
	// plain is JSON-quoted (JSON.stringify of the string). Parse it back.
	var out string
	if err := json.Unmarshal(plain, &out); err != nil {
		return "", false
	}
	return out, true
}

// encryptText = "ss::" + encrypt(plain) ("" when plain "").
func (c *cipher) encryptText(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	tok, err := c.encrypt(plain)
	if err != nil {
		return "", err
	}
	return "ss::" + tok, nil
}

// decryptText — "ss::" prefix optional; returns (value, ok).
func (c *cipher) decryptText(tok string) (string, bool) {
	if tok == "" {
		return "", false
	}
	if strings.HasPrefix(tok, "ss::") {
		tok = tok[len("ss::"):]
	}
	return decryptWith(tok, c.key)
}

// ---------- exported interop bridge ----------
// Unexported type, exported methods: cmd/sscipher-probe and interop tests
// use these to prove Go<->Node secret-cipher parity without exposing the
// internal struct fields. `Open` loads the cipher from env/FILE (same rules
// as loadCipher).
func Open() (*cipher, error) { return loadCipher() }

func (c *cipher) DecryptText(tok string) (string, bool) { return c.decryptText(tok) }

func (c *cipher) EncryptText(plain string) (string, error) { return c.encryptText(plain) }
