package accesstoken

// accesstoken_relaxed_test.go — oracle-pinned tests for the RelaxedLabels
// mode (owner task (b): let the web service's per-provider token ciphers reuse
// this one crypto implementation). The crypto/wire format is UNCHANGED from
// the strict Node-oracle path; only the label-VERSION gate is waived.

import (
	"regexp"
	"strings"
	"testing"
)

var relaxLongPass = strings.Repeat("4", 38)

// TestStrictStillRejectsMultiDashLabels pins the Node-oracle behaviour
// unchanged: the shared AccessTokenEncryptor constructor derives the version
// as `cipherLabel.split('-')[1]`, so a label with MULTIPLE dash segments
// (version segment = the 2nd dash-group, not 'v3') is rejected. web's cipher
// has NO such gate (it uses the label verbatim as a token prefix), so labels
// an operator might configure (e.g. `OL-CEP-2042-v3`) that web accepts are
// rejected by strict `New`. RelaxedLabels is the waiver that lets web reuse
// one crypto implementation for ALL such labels.
func TestStrictStillRejectsMultiDashLabels(t *testing.T) {
	for _, label := range []string{"OL-CEP-2042-v3", "SS-TEST-2042-1-v3"} {
		_, err := New(Config{
			CipherLabel:     label,
			CipherPasswords: map[string]string{label: relaxLongPass},
		})
		if err == nil {
			t.Fatalf("strict New(%q) must be rejected (version segment != v3)", label)
		}
		if !strings.Contains(err.Error(), "unknown version") {
			t.Fatalf("strict New(%q) err=%q, want 'unknown version'", label, err)
		}
	}
}

// TestRelaxedAcceptsAllWebShapes proves the opt-in: labels the shared
// constructor would reject (multi-dash) are accepted when RelaxedLabels is
// set, and round-trip for both a JSON-quoted string (site-settings secrets)
// and a JSON object (zotero/mendeley credentials). web's real default labels
// (`OL_CEP-v3`, `OL_WEBDAV-v3`, `2024.1-v3`) are strict-valid too — included
// to prove the swap works under the mode web actually uses.
func TestRelaxedAcceptsAllWebShapes(t *testing.T) {
	for _, label := range []string{"OL_CEP-v3", "OL_WEBDAV-v3", "2024.1-v3", "OL-CEP-2042-v3"} {
		e, err := New(Config{
			CipherLabel:     label,
			CipherPasswords: map[string]string{label: relaxLongPass},
			RelaxedLabels:   true,
		})
		if err != nil {
			t.Fatalf("relaxed New(%q) = %v (should be accepted)", label, err)
		}

		// (a) JSON-quoted string plaintext (EncryptText/secret parity).
		got, err := e.EncryptJson("hello-secret")
		if err != nil {
			t.Fatalf("relaxed EncryptJson(%q): %v", label, err)
		}
		v, err := e.DecryptToJson(got)
		if err != nil {
			t.Fatalf("relaxed DecryptToJson: %v", err)
		}
		if s, ok := v.(string); !ok || s != "hello-secret" {
			t.Fatalf("relaxed %q string round-trip = %v, want %q", label, v, "hello-secret")
		}

		// (b) JSON object plaintext (raw-credentials parity).
		obj := []any{"apiKey", "user-id-123"}
		got2, err := e.EncryptJson(map[string]any{"apiKey": "k-1", "uid": obj})
		if err != nil {
			t.Fatalf("relaxed EncryptJson(map): %v", err)
		}
		v2, err := e.DecryptToJson(got2)
		if err != nil {
			t.Fatalf("relaxed DecryptToJson(map): %v", err)
		}
		m, ok := v2.(map[string]any)
		if !ok {
			t.Fatalf("relaxed %q object round-trip not a map: %v", label, v2)
		}
		if m["apiKey"] != "k-1" {
			t.Fatalf("relaxed %q object round-trip apiKey = %v, want k-1", label, m["apiKey"])
		}

		// (c) wire format: label : hex(16B salt) : b64(ct) : hex(16B iv)
		re := regexp.MustCompile(`^` + regexp.QuoteMeta(label) + `:[0-9a-f]{32}:[A-Za-z0-9+/=]+:[0-9a-f]{32}$`)
		if !re.MatchString(got) {
			t.Fatalf("relaxed %q token %q does not match v3 wire format", label, got)
		}
	}
}

// TestRelaxedKeepsOtherValidation pins that relaxed mode waives ONLY the
// label-version gate — every other constructor validation still fires.
func TestRelaxedKeepsOtherValidation(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"empty label", Config{CipherLabel: "", CipherPasswords: map[string]string{"": relaxLongPass}, RelaxedLabels: true}, "cipherLabel cannot be empty"},
		{"colon label", Config{CipherLabel: "OL:CEP-v3", CipherPasswords: map[string]string{"OL:CEP-v3": relaxLongPass}, RelaxedLabels: true}, "cipherLabel must not contain a colon"},
		{"missing password", Config{CipherLabel: "OL_CEP-v3", CipherPasswords: map[string]string{"OL_CEP-v3": ""}, RelaxedLabels: true}, "cipherPasswords['OL_CEP-v3'] is missing"},
		{"short password", Config{CipherLabel: "OL_CEP-v3", CipherPasswords: map[string]string{"OL_CEP-v3": "x"}, RelaxedLabels: true}, "cipherPasswords['OL_CEP-v3'] is too short"},
		{"unknown default", Config{CipherLabel: "OL_OTHER-v3", CipherPasswords: map[string]string{"OL_CEP-v3": relaxLongPass}, RelaxedLabels: true}, "unknown default cipherLabel OL_OTHER-v3"},
	}
	for _, tc := range cases {
		_, err := New(tc.cfg)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err=%v, want %q", tc.name, err, tc.want)
		}
	}
}

// TestRelaxedSharesCryptoWithStrict proves relaxed mode uses the IDENTICAL
// v3 crypto: an encryptor configured (strict-valid) label + password decrypts
// a token that strict-mode `New` would produce, and vice versa — i.e. the two
// modes differ only in which labels they admit, never in the ciphertext.
func TestRelaxedSharesCryptoWithStrict(t *testing.T) {
	// A label that is valid under BOTH modes, so we can prove identity.
	cfg := Config{
		CipherLabel:     "2023.1-v3",
		CipherPasswords: map[string]string{"2023.1-v3": relaxLongPass},
	}
	strict, err := New(cfg)
	if err != nil {
		t.Fatalf("strict New: %v", err)
	}
	cfg2 := cfg
	cfg2.RelaxedLabels = true
	relaxed, err := New(cfg2)
	if err != nil {
		t.Fatalf("relaxed New: %v", err)
	}

	// strict encrypt -> relaxed decrypt
	st, err := strict.EncryptJson("shared-plaintext")
	if err != nil {
		t.Fatalf("strict EncryptJson: %v", err)
	}
	if v, err := relaxed.DecryptToJson(st); err != nil || v.(string) != "shared-plaintext" {
		t.Fatalf("relaxed decrypted strict token = %v / %v", v, err)
	}

	// relaxed encrypt -> strict decrypt
	rt, err := relaxed.EncryptJson("shared-plaintext")
	if err != nil {
		t.Fatalf("relaxed EncryptJson: %v", err)
	}
	if v, err := strict.DecryptToJson(rt); err != nil || v.(string) != "shared-plaintext" {
		t.Fatalf("strict decrypted relaxed token = %v / %v", v, err)
	}
}
