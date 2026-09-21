// Package accesstoken is the 1:1 Go port of
// `libraries/access-token-encryptor` (npm `@overleaf/access-token-encryptor`
// v3.0.0): symmetric AES-256-CTR encryption of JSON access tokens with
// HKDF-SHA512 key derivation from a per-scheme password.
//
// Wire format (v3 scheme, label-prefixed):
//
//	<label>:<salt 16 bytes hex>:<ciphertext base64>:<iv 16 bytes hex>
//
// key = HKDF-SHA512(password_utf8, salt, info="", L=32);
// plaintext = AES-256-CTR(plaintext_json, iv). Encryption is randomized
// (fresh salt+iv per call); the same plaintext therefore yields different
// ciphertexts on every call (pinned in the Node test suite).
package accesstoken

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"
)

const algorithm = "aes-256-ctr" // Node ALGORITHM constant

// Config mirrors the Node settings object
// (`{ cipherLabel, cipherPasswords: { <label>: <password> … } }`).
type Config struct {
	CipherLabel     string
	CipherPasswords map[string]string
	// PasswordOrder is the insertion order of CipherPasswords (Node iterates
	// Object.keys in insertion order, so constructor validation errors and
	// unknown-scheme reports are deterministic there). Go map iteration is
	// random; pass the config file order to keep multi-scheme failure
	// reporting deterministic and Node-ordered.
	PasswordOrder []string
}

// Encryptor mirrors `AccessTokenEncryptor`: one scheme per cipher label
// (only v3 is implementable) plus a default scheme for encryption.
type Encryptor struct {
	schemeByCipherLabel map[string]*scheme
	defaultScheme       *scheme
}

type scheme struct {
	cipherLabel    string
	cipherPassword string
}

// New mirrors the `new AccessTokenEncryptor(settings)` constructor,
// including every validation error message (Node source order):
//
//  1. `cipherLabel cannot be empty`
//  2. `cipherLabel must not contain a colon (:), got <label>`
//  3. `cipherLabel must contain version suffix (e.g. 2042.1-v42), got <label>`
//  4. `cipherPasswords['<label>'] is missing`
//  5. `cipherPasswords['<label>'] is too short`
//  6. `unknown version '<v>' for <label>`
//  7. `unknown default cipherLabel <label>`
func New(cfg Config) (*Encryptor, error) {
	e := &Encryptor{schemeByCipherLabel: map[string]*scheme{}}
	order := labelsInOrder(cfg)
	for _, label := range order {
		if label == "" {
			return nil, errors.New("cipherLabel cannot be empty")
		}
		if strings.Contains(label, ":") {
			return nil, fmt.Errorf("cipherLabel must not contain a colon (:), got %s", label)
		}
		// Node: const [, version] = cipherLabel.split('-')
		parts := strings.Split(label, "-")
		version := ""
		if len(parts) > 1 {
			version = parts[1]
		}
		if version == "" {
			return nil, fmt.Errorf("cipherLabel must contain version suffix (e.g. 2042.1-v42), got %s", label)
		}
		password, present := cfg.CipherPasswords[label]
		if !present || password == "" {
			return nil, fmt.Errorf("cipherPasswords['%s'] is missing", label)
		}
		// Node: cipherPassword.length is a JS string length (UTF-16 code
		// units), not a byte count.
		if utf16Len(password) < 16 {
			return nil, fmt.Errorf("cipherPasswords['%s'] is too short", label)
		}
		switch version {
		case "v3":
			e.schemeByCipherLabel[label] = &scheme{cipherLabel: label, cipherPassword: password}
		default:
			return nil, fmt.Errorf("unknown version '%s' for %s", version, label)
		}
	}
	def, ok := e.schemeByCipherLabel[cfg.CipherLabel]
	if !ok {
		return nil, fmt.Errorf("unknown default cipherLabel %s", cfg.CipherLabel)
	}
	e.defaultScheme = def
	return e, nil
}

// labelsInOrder returns config labels in Node (insertion) order when
// PasswordOrder is provided, else sorted for determinism.
func labelsInOrder(cfg Config) []string {
	if cfg.PasswordOrder != nil {
		// Keep PasswordOrder authoritative (Node insertion order); append
		// any config keys not listed (defensive), sorted for determinism.
		seen := make(map[string]bool, len(cfg.PasswordOrder))
		out := make([]string, 0, len(cfg.CipherPasswords))
		for _, l := range cfg.PasswordOrder {
			if _, in := cfg.CipherPasswords[l]; !in {
				continue
			}
			if seen[l] {
				continue
			}
			seen[l] = true
			out = append(out, l)
		}
		var extra []string
		for l := range cfg.CipherPasswords {
			if !seen[l] {
				extra = append(extra, l)
			}
		}
		sort.Strings(extra)
		return append(out, extra...)
	}
	out := make([]string, 0, len(cfg.CipherPasswords))
	for l := range cfg.CipherPasswords {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// EncryptJson mirrors `encryptJson(json)`: default scheme, fresh
// random salt+iv, returns `label:saltHex:ciphertextB64:ivHex`.
func (e *Encryptor) EncryptJson(v any) (string, error) {
	plain, err := jsonMarshalNodeStyle(v)
	if err != nil {
		return "", err
	}
	// Node: bytes = randomBytes(32); salt = bytes.slice(0,16); iv = bytes.slice(16,32)
	bytes32 := make([]byte, 32)
	if _, err := rand.Read(bytes32); err != nil {
		return "", fmt.Errorf("random bytes: %w", err)
	}
	salt, iv := bytes32[:16], bytes32[16:]
	key, err := keyFnV3(e.defaultScheme.cipherPassword, salt)
	if err != nil {
		return "", err
	}
	ct := ctrEncrypt(key, iv, plain)
	return strings.Join([]string{
		e.defaultScheme.cipherLabel,
		hexEncode(salt),
		base64.StdEncoding.EncodeToString(ct),
		hexEncode(iv),
	}, ":"), nil
}

// DecryptToJson mirrors `decryptToJson(encryptedJson)`: label lookup (the
// first `-`-free… actually first `:`-separated part), scheme dispatch,
// CTR decrypt, `JSON.parse`.
func (e *Encryptor) DecryptToJson(encrypted string) (any, error) {
	parts := splitMax(encrypted, ":", 4)
	label := parts[0]
	sc, ok := e.schemeByCipherLabel[label]
	if !ok {
		return nil, errors.New("unknown access-token-encryptor label " + label)
	}
	if len(parts) < 4 {
		// Node throws a TypeError on the undefined salt here; surface a
		// clean error instead (decryption is impossible either way).
		return nil, fmt.Errorf("malformed token for label %s", label)
	}
	saltHex, cipherB64, ivHex := parts[1], parts[2], parts[3]
	salt, err := hexDecode(saltHex)
	if err != nil {
		return nil, fmt.Errorf("malformed salt for label %s", label)
	}
	iv, err := hexDecode(ivHex)
	if err != nil {
		return nil, fmt.Errorf("malformed iv for label %s", label)
	}
	plain, err := ctrDecrypt(sc, salt, cipherB64, iv)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(plain, &out); err != nil {
		// Node: catch (e) { throw new Error('error decrypting token') }
		return nil, errors.New("error decrypting token")
	}
	return out, nil
}

// ---------- v3 scheme (HKDF-SHA512) ----------

// keyFnV3 mirrors AccessTokenSchemeV3.keyFn: HKDF-SHA512(password, salt,
// info="", L=32). HKDF is RFC 5869 — byte-identical across stacks.
func keyFnV3(password string, salt []byte) ([]byte, error) {
	// info = '' (Node `optionalInfo = ''`); L = 32.
	return hkdf.Key(sha512.New, []byte(password), salt, "", 32)
}

// ctrEncrypt mirrors createCipheriv('aes-256-ctr', key, iv).
func ctrEncrypt(key, iv, plain []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err) // 32-byte key: impossible
	}
	stream := cipher.NewCTR(block, iv)
	out := make([]byte, len(plain))
	stream.XORKeyStream(out, plain)
	return out
}

// ctrDecrypt mirrors createDecipheriv('aes-256-ctr', key, iv) — CTR has no
// auth tag, so "invalid ciphertext" is caught by the JSON.parse step
// (Node 'error decrypting token'), reproduced by the caller.
func ctrDecrypt(sc *scheme, salt []byte, cipherB64 string, iv []byte) ([]byte, error) {
	key, err := keyFnV3(sc.cipherPassword, salt)
	if err != nil {
		return nil, err
	}
	ct, err := base64.StdEncoding.DecodeString(cipherB64)
	if err != nil {
		// Node's decipher would surface garbled bytes → the parse fails as
		// well; normalize to the same contract.
		return nil, errors.New("error decrypting token")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCTR(block, iv)
	out := make([]byte, len(ct))
	stream.XORKeyStream(out, ct)
	return out, nil
}

// ---------- encoding helpers (Node Buffer parity) ----------

// hexEncode mirrors Buffer.toString('hex').
func hexEncode(b []byte) string { return strings.ToLower(fmt.Sprintf("%x", b)) }

// hexDecode mirrors Buffer.from(s, 'hex') for even-length canonical hex.
func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, errors.New("invalid hex")
	}
	out := make([]byte, len(s)/2)
	dec := func(c byte) int {
		switch {
		case c >= '0' && c <= '9':
			return int(c - '0')
		case c >= 'a' && c <= 'f':
			return int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			return int(c-'A') + 10
		}
		return -1
	}
	for i := 0; i < len(out); i++ {
		hi, lo := dec(s[2*i]), dec(s[2*i+1])
		if hi < 0 || lo < 0 {
			return nil, errors.New("invalid hex")
		}
		out[i] = byte(hi)<<4 | byte(lo)
	}
	return out, nil
}

// splitMax mirrors String.prototype.split(sep, limit) (limit truncates the
// number of pieces; the last piece keeps the remainder).
func splitMax(s, sep string, n int) []string {
	if n <= 1 {
		return []string{s}
	}
	return strings.SplitN(s, sep, n)
}

// jsonMarshalNodeStyle mirrors JSON.stringify(json): compact, no HTML
// escaping. Key ordering and float formatting follow Go's JSON rules, which
// is safe here: the ciphertext is only ever parsed back by a JSON parser
// (JSON.parse), where both encodings are equivalent.
func jsonMarshalNodeStyle(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1] // Encode appends a newline
	}
	return b, nil
}

// utf16Len mirrors JS string.length (UTF-16 code units).
func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }
