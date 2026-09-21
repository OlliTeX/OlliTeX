package accesstoken

import (
	"strings"
	"testing"
)

// coverage_test.go — closes the uncovered narrow branches (target ≥85%).

func TestLabelsInOrderPasswordOrder(t *testing.T) {
	pw := strings.Repeat("4", 38)
	cfg := Config{
		CipherLabel: "2024.1-v3",
		CipherPasswords: map[string]string{
			"2023.1-v3": strings.Repeat("1", 32),
			"2024.1-v3": pw,
			"orphan":    strings.Repeat("2", 32), // not in PasswordOrder → appended
		},
		PasswordOrder: []string{"2024.1-v3", "2023.1-v3", "not-in-config"},
	}
	got := labelsInOrder(cfg)
	want := []string{"2024.1-v3", "2023.1-v3", "orphan"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("labelsInOrder = %v, want %v", got, want)
	}
	_ = pw
}

func TestEncryptJsonMarshalError(t *testing.T) {
	e, err := New(Config{
		CipherLabel:     "2023.1-v3",
		CipherPasswords: map[string]string{"2023.1-v3": strings.Repeat("4", 38)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan int)
	if _, err := e.EncryptJson(ch); err == nil {
		t.Fatal("marshalling a chan must fail")
	}
}

func TestEncryptJsonNoHtmlEscape(t *testing.T) {
	e, _ := New(Config{
		CipherLabel:     "2023.1-v3",
		CipherPasswords: map[string]string{"2023.1-v3": strings.Repeat("4", 38)},
	})
	enc, err := e.EncryptJson(map[string]any{"tag": "<b>hi</b>", "eq": "a&b"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.DecryptToJson(enc)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := out.(map[string]any)
	if m["tag"] != "<b>hi</b>" || m["eq"] != "a&b" {
		t.Fatalf("payload = %v (HTML escaping must be off, like JSON.stringify)", m)
	}
}

func TestDecryptMalformedHex(t *testing.T) {
	e, _ := New(Config{
		CipherLabel:     "2023.1-v3",
		CipherPasswords: map[string]string{"2023.1-v3": strings.Repeat("4", 38)},
	})
	// Odd-length salt hex.
	if _, err := e.DecryptToJson("2023.1-v3:abc:jQf+Uh5Den3JREtvc82GW5Q=:aaaa55bb66cc77dd88ee99ff00112233"); err == nil || !strings.Contains(err.Error(), "malformed salt") {
		t.Fatalf("odd salt: err = %v, want malformed salt", err)
	}
	// Non-hex iv.
	if _, err := e.DecryptToJson("2023.1-v3:c7a39310056b694c:jQf+Uh5Den3JREtvc82GW5Q=:zzzzzzzzzzzzzzzz"); err == nil || !strings.Contains(err.Error(), "malformed iv") {
		t.Fatalf("bad iv: err = %v, want malformed iv", err)
	}
}

func TestHexDecode(t *testing.T) {
	if b, err := hexDecode("ABcd"); err != nil || len(b) != 2 || b[0] != 0xAB || b[1] != 0xcd {
		t.Fatalf("mixed-case hex: %v %v", b, err)
	}
	if _, err := hexDecode("abc"); err == nil {
		t.Fatal("odd-length hex must fail")
	}
	if _, err := hexDecode("00zz"); err == nil {
		t.Fatal("non-hex characters must fail")
	}
}

func TestSplitMax(t *testing.T) {
	if got := strings.Join(splitMax("a:b:c", ":", 1), "|"); got != "a:b:c" {
		t.Fatalf("n=1: %q", got)
	}
	if got := strings.Join(splitMax("a:b:c", ":", 2), "|"); got != "a|b:c" {
		t.Fatalf("n=2: %q", got)
	}
	if got := strings.Join(splitMax("a:b:c", ":", 4), "|"); got != "a|b|c" {
		t.Fatalf("n=4: %q", got)
	}
}

func TestMultiSchemeDefaultsToConfiguredLabel(t *testing.T) {
	pwA := strings.Repeat("1", 32)
	pwB := strings.Repeat("2", 32)
	e, err := New(Config{
		CipherLabel: "2024.1-v3",
		CipherPasswords: map[string]string{
			"2023.1-v3": pwA,
			"2024.1-v3": pwB,
		},
		PasswordOrder: []string{"2023.1-v3", "2024.1-v3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	enc, err := e.EncryptJson(map[string]any{"hello": "world"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "2024.1-v3:") {
		t.Fatalf("token %q must use the default label 2024.1-v3", enc)
	}
	out, err := e.DecryptToJson(enc)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := out.(map[string]any)
	if m["hello"] != "world" {
		t.Fatalf("payload = %v", m)
	}
	// A 2023.1-v3 token made with its own password also decodes (label lookup).
	e2, _ := New(Config{
		CipherLabel:     "2023.1-v3",
		CipherPasswords: map[string]string{"2023.1-v3": pwA},
	})
	encA, _ := e2.EncryptJson(map[string]any{"other": 1})
	outA, err := e.DecryptToJson(encA)
	if err != nil || outA.(map[string]any)["other"] != float64(1) {
		t.Fatalf("cross-scheme decrypt = %v, %v", outA, err)
	}
}
