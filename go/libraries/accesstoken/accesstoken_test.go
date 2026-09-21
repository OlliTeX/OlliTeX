package accesstoken

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// TestDecryptGolden2023 — byte-parity check against the Node test suite's
// known token (AccessTokenEncryptorTests.js `encrypted2023`): decrypting
// THIS ciphertext MUST yield {"hello":"world"}. Pins HKDF-SHA512 key
// derivation, the salt/iv split, hex/base64 encoding and AES-256-CTR
// direction — all against the live Node implementation.
func TestDecryptGolden2023(t *testing.T) {
	e, err := New(Config{
		CipherLabel:     "2023.1-v3",
		CipherPasswords: map[string]string{"2023.1-v3": strings.Repeat("4", 38)},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.DecryptToJson(
		"2023.1-v3:a6dd3781dd6ce93a4134874b505a209c:9TdIDAc8V9SeR0ffSn63Jj4=:d8b2de0b733c81b949993dce229abb4c")
	if err != nil {
		t.Fatalf("golden decryption: %v", err)
	}
	b, _ := json.Marshal(out)
	if string(b) != `{"hello":"world"}` {
		t.Fatalf("golden payload = %s, want {\"hello\":\"world\"}", b)
	}
}

func TestConstructorValidation(t *testing.T) {
	longPass := strings.Repeat("1", 32)
	cases := []struct {
		cfg  Config
		want string
	}{
		{Config{CipherLabel: "", CipherPasswords: map[string]string{"": ""}}, "cipherLabel cannot be empty"},
		{Config{CipherLabel: "2023:1-v2", CipherPasswords: map[string]string{"2023:1-v2": ""}}, "cipherLabel must not contain a colon (:), got 2023:1-v2"},
		{Config{CipherLabel: "2023.1", CipherPasswords: map[string]string{"2023.1": longPass}}, "cipherLabel must contain version suffix (e.g. 2042.1-v42), got 2023.1"},
		{Config{CipherLabel: "2023.1-", CipherPasswords: map[string]string{"2023.1-": longPass}}, "cipherLabel must contain version suffix (e.g. 2042.1-v42), got 2023.1-"},
		{Config{CipherLabel: "2023.1-v3", CipherPasswords: map[string]string{"2023.1-v3": ""}}, "cipherPasswords['2023.1-v3'] is missing"},
		// Node: cipherPasswords {'2023.1-v3': undefined} → falsy → "missing";
		// Go maps express this as an absent key, so an EMPTY map walks the
		// same Node code path (no labels to validate) and fails at the
		// default-lookup step instead:
		{Config{CipherLabel: "2023.1-v3", CipherPasswords: map[string]string{}}, "unknown default cipherLabel 2023.1-v3"},
		{Config{CipherLabel: "2023.1-v3", CipherPasswords: map[string]string{"2023.1-v3": "foo"}}, "cipherPasswords['2023.1-v3'] is too short"},
		{Config{CipherLabel: "2023.1-v0", CipherPasswords: map[string]string{"2023.1-v0": longPass}}, "unknown version 'v0' for 2023.1-v0"},
		{Config{CipherLabel: "2000.1-v3", CipherPasswords: map[string]string{"2023.1-v3": longPass}}, "unknown default cipherLabel 2000.1-v3"},
	}
	for i, c := range cases {
		_, err := New(c.cfg)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("case %d: err = %v, want containing %q", i, err, c.want)
		}
	}
	// 15-unit password: too short; 16 units: ok. JS length is UTF-16 units.
	if _, err := New(Config{CipherLabel: "2023.1-v3", CipherPasswords: map[string]string{"2023.1-v3": strings.Repeat("a", 15)}}); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Fatalf("15-unit password must be too short, got %v", err)
	}
	if _, err := New(Config{CipherLabel: "2023.1-v3", CipherPasswords: map[string]string{"2023.1-v3": strings.Repeat("a", 16)}}); err != nil {
		t.Fatalf("16-unit password must pass, got %v", err)
	}
}

func TestRoundTrip(t *testing.T) {
	e, err := New(Config{
		CipherLabel:     "2023.1-v3",
		CipherPasswords: map[string]string{"2023.1-v3": strings.Repeat("4", 38)},
	})
	if err != nil {
		t.Fatal(err)
	}
	obj := map[string]any{"hello": "world"}
	enc, err := e.EncryptJson(obj)
	if err != nil {
		t.Fatal(err)
	}
	// Node test: /^2023.1-v3:[0-9a-f]{32}:[a-zA-Z0-9=+/]+:[0-9a-f]{32}$/
	re := regexp.MustCompile(`^2023\.1-v3:[0-9a-f]{32}:[a-zA-Z0-9=+/]+:[0-9a-f]{32}$`)
	if !re.MatchString(enc) {
		t.Fatalf("token %q does not match the Node format regex", enc)
	}
	out, err := e.DecryptToJson(enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["hello"] != "world" {
		t.Fatalf("round trip = %v", out)
	}
	// Node test: consecutive encryptions differ (fresh salt+iv).
	enc2, _ := e.EncryptJson(obj)
	if enc2 == enc {
		t.Fatalf("two encryptions must differ (random salt+iv)")
	}
	if out2, _ := e.DecryptToJson(enc2); asMap(out2)["hello"] != "world" {
		t.Fatalf("second round trip = %v", out2)
	}
}

func TestUnknownLabels(t *testing.T) {
	e, _ := New(Config{
		CipherLabel:     "2023.1-v3",
		CipherPasswords: map[string]string{"2023.1-v3": strings.Repeat("4", 38)},
	})
	// Node test pins (legacy-format tokens fail at the label lookup).
	body := "c7a39310056b694c:jQf+Uh5Den3JREtvc82GW5Q="
	for _, label := range []string{"2015.1", "2016.1", "2019.1", "xxxxxx"} {
		_, err := e.DecryptToJson(label + ":" + body)
		if err == nil || err.Error() != "unknown access-token-encryptor label "+label {
			t.Fatalf("label %s: err = %v, want exact unknown-label message", label, err)
		}
	}
}

func TestDecryptCorruptCiphertext(t *testing.T) {
	e, _ := New(Config{
		CipherLabel:     "2023.1-v3",
		CipherPasswords: map[string]string{"2023.1-v3": strings.Repeat("4", 38)},
	})
	// Known label, 16 bytes of arbitrary ciphertext (valid base64): CTR
	// decryption yields garbage → JSON parse fails → 'error decrypting
	// token' — the Node contract for undecryptable payloads.
	bad := "2023.1-v3:c7a39310056b694c:jQf+Uh5Den3JREtvc82GW5Q=:aaaa55bb66cc77dd88ee99ff00112233"
	_, err := e.DecryptToJson(bad)
	if err == nil || err.Error() != "error decrypting token" {
		t.Fatalf("corrupt ciphertext: err = %v, want 'error decrypting token'", err)
	}
}

func TestDecryptInvalidBase64(t *testing.T) {
	e, _ := New(Config{
		CipherLabel:     "2023.1-v3",
		CipherPasswords: map[string]string{"2023.1-v3": strings.Repeat("4", 38)},
	})
	bad := "2023.1-v3:c7a39310056b694c:not-base64!!!:aaaa55bb66cc77dd88ee99ff00112233"
	if _, err := e.DecryptToJson(bad); err == nil {
		t.Fatal("invalid base64 must fail")
	}
}

func TestDecryptMissingPayload(t *testing.T) {
	e, _ := New(Config{
		CipherLabel:     "2023.1-v3",
		CipherPasswords: map[string]string{"2023.1-v3": strings.Repeat("4", 38)},
	})
	// Fewer than 4 fields with a known label.
	if _, err := e.DecryptToJson("2023.1-v3:c7a39310056b694c"); err == nil {
		t.Fatal("short token must fail")
	}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
