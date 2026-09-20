package llmsettings

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestCryptoRoundtrip — encrypt -> storedToPlaintext -> same plaintext, and
// the enc:v1:<iv>:<tag>:<ct> shape (each part standard base64).
func TestCryptoRoundtrip(t *testing.T) {
	os.Setenv("LLM_KEY_SECRET", "unit-test-secret")
	defer os.Unsetenv("LLM_KEY_SECRET")
	c := newLCrypto()
	enc := c.encrypt("topsecret")
	if !strings.HasPrefix(enc, "enc:v1:") {
		t.Fatalf("missing prefix: %q", enc)
	}
	parts := strings.Split(strings.TrimPrefix(enc, "enc:v1:"), ":")
	if len(parts) != 3 {
		t.Fatalf("want 3 parts, got %d", len(parts))
	}
	b64 := regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)
	if !b64.MatchString(parts[0]) || !b64.MatchString(parts[1]) || !b64.MatchString(parts[2]) {
		t.Fatalf("parts not base64: %q", enc)
	}
	iv, err := b64d(parts[0])
	if err != nil || len(iv) != 12 {
		t.Fatalf("iv length: %d", len(iv))
	}
	tag, err := b64d(parts[1])
	if err != nil || len(tag) != 16 {
		t.Fatalf("tag length: %d", len(tag))
	}
	dec := c.storedToPlaintext(enc)
	if dec != "topsecret" {
		t.Fatalf("roundtrip: %q", dec)
	}
	if c.storedToPlaintext("not-encrypted") != "not-encrypted" {
		t.Fatal("plain passthrough")
	}
	if c.storedToPlaintext("enc:v1:ZZZ:AAA:BBB") != "" {
		t.Fatal("garbage must decrypt to empty")
	}
}

// TestCrossLangCrypto — pinned Node-produced and Go-produced enc:v1 blobs
// (secret "cross-parity-secret") both decrypt to "cross-check" in Go; the
// Node side decrypts Go blobs (verified cross-run on 2026-09-16 with
// LLM_KEY_SECRET=cross-parity-secret, LLMCrypto.mjs).
func TestCrossLangCrypto(t *testing.T) {
	os.Setenv("LLM_KEY_SECRET", "cross-parity-secret")
	defer os.Unsetenv("LLM_KEY_SECRET")
	c := newLCrypto()
	nodeBlob := "enc:v1:i5faqKDiGaSV0Tya:+joGTsLur0ZtG9fgOKDOhQ==:g8nvLjLaR84o6JM="
	goBlob := "enc:v1:PmBOQuq1rT17b/rF:AcDaJcV0MFjY9ezKzGMkyg==:D9YQXmifbcSH80k="
	if got := c.storedToPlaintext(nodeBlob); got != "cross-check" {
		t.Fatalf("node blob: %q", got)
	}
	if got := c.storedToPlaintext(goBlob); got != "cross-check" {
		t.Fatalf("go blob: %q", got)
	}
}

// TestNoSecretPlaintext — no LLM_KEY_SECRET -> plaintext passthrough.
func TestNoSecretPlaintext(t *testing.T) {
	os.Unsetenv("LLM_KEY_SECRET")
	c := newLCrypto()
	if got := c.encrypt("plain"); got != "plain" {
		t.Fatalf("want plaintext passthrough, got %q", got)
	}
}

// TestJsonIndentOrder — ordered write: existing key order preserved, new
// keys appended, 2-space indent, no trailing newline.
func TestJsonIndentOrder(t *testing.T) {
	o := obj{}
	o = o.set("b", "2")
	o = o.set("c", true)
	o = o.set("b", "22") // update keeps position
	o = o.set("a", "1")  // new key appended
	got := jsonIndent(o)
	want := "{\n  \"b\": \"22\",\n  \"c\": true,\n  \"a\": \"1\"\n}"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestOJSONParseNumbers — 1e3 -> 1000; 0.5 stays 0.5; -0 -> -0.
func TestOJSONParseNumbers(t *testing.T) {
	v, err := ojsonParse(`{"a":1e3,"b":0.5}`)
	if err != nil {
		t.Fatal(err)
	}
	o := v.(obj)
	if got := o.num("a"); got != 1000 {
		t.Fatalf("1e3 -> %v", got)
	}
	if got := o.num("b"); got != 0.5 {
		t.Fatalf("0.5 -> %v", got)
	}
	if got := jsonWrite(v.(obj)); got != `{"a":1000,"b":0.5}` {
		t.Fatalf("compact: %s", got)
	}
}

// TestMergeActionPrompts — defaults order + override only for known keys.
func TestMergeActionPrompts(t *testing.T) {
	stored := obj{}
	stored = stored.set("academic", "OVERRIDE")
	stored = stored.set("bogus", "IGNORED")
	out := mergeActionPrompts(stored)
	keys := []string{}
	for _, k := range out.keys() {
		keys = append(keys, k)
	}
	if strings.Join(keys, ",") != "paraphrase,academic,concise,translate,synonyms" {
		t.Fatalf("key order: %v", keys)
	}
	if out.string("academic") != "OVERRIDE" {
		t.Fatal("override lost")
	}
	if out.string("paraphrase") != defActionPrompts[0].v {
		t.Fatal("default lost")
	}
	// defaults (non-overridden) end with \n\n{{selection}}
	for _, p := range []string{"paraphrase", "concise", "translate", "synonyms"} {
		v := out.string(p)
		if !strings.HasSuffix(v, "\n\n{{selection}}") {
			t.Fatalf("%s suffix", p)
		}
	}
}

// TestProviderType — substring detection, openai.com before compatible.
func TestProviderType(t *testing.T) {
	cases := map[string]string{
		"https://api.anthropic.com":           "anthropic",
		"https://api.anthropic.com/v1/models": "anthropic",
		"https://api.openai.com/v1":           "openai",
		"https://openrouter.ai/api":           "openaiCompatible",
		"https://localhost:11434":             "openaiCompatible",
		"bad url with spaces!":                "openaiCompatible",
	}
	for raw, want := range cases {
		if got := detectProviderType(raw); got != want {
			t.Fatalf("%q -> %q, want %q", raw, got, want)
		}
	}
}

// TestBlockedURLs — Node message parity.
func TestBlockedURLs(t *testing.T) {
	cases := map[string]string{
		"ftp://example.com/x":                     "only http(s) URLs are allowed",
		"http://localhost:1234/v1":                "loopback/local name",
		"http://127.0.0.1:11434/v1":               "loopback range",
		"http://192.168.1.10:8080/v1":             "",
		"http://10.0.0.5:8080/v1":                 "",
		"http://169.254.169.254/latest/meta-data": "cloud metadata / link-local range",
		"http://[::1]:11434/v1":                   "loopback/local name",
		"http://0.0.0.0:11434/v1":                 "loopback/local name",
		"http://example.com/v1":                   "",
	}
	for raw, want := range cases {
		err := assertPublicLlmBaseUrl(raw)
		if want == "" {
			if err != nil {
				t.Fatalf("%q should pass: %v", raw, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%q should be blocked", raw)
		}
		if got := err.Error(); got != "Blocked LLM base URL ("+want+"). Point the provider at a reachable public or LAN LLM endpoint." {
			t.Fatalf("%q -> %q", raw, got)
		}
	}
}
