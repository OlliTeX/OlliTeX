// P6.9 webdav — unit checks (offline: cipher round-trip through the shared
// V3 scheme with a per-provider key file, credentials-plain JSON shape,
// path helpers).

package webdav

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/features/sitesettings"
)

// TestWdCipherRoundTrip — a token encrypted under the webdav per-provider key
// file decrypts with the SAME key (V3 scheme parity) and NOT with a different
// key label (per-provider isolation, same contract as zotero/mendeley).
func TestWdCipherRoundTrip(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ".webdav-token-cipher.json")
	t.Setenv("WEBDAV_TOKEN_CIPHER_FILE", file)
	t.Setenv("WEBDAV_TOKEN_CIPHER_PASSWORD", "")
	t.Setenv("WEBDAV_TOKEN_CIPHER_LABEL", "OL_WEBDAV-v3")
	resetWdCipherForTest()

	plain := `{"baseUrl":"https://dav.e2e.invalid","username":"alice","password":"s3cret","rootPath":"/Overleaf"}`
	ci := wdCipherInst()
	if ci == nil {
		t.Fatal("cipher unavailable")
	}
	tok, err := ci.EncryptRaw(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(tok, "OL_WEBDAV-v3:") {
		t.Fatalf("token label mismatch: %q", tok[:16])
	}
	back, ok := ci.DecryptRaw(tok)
	if !ok || back != plain {
		t.Fatalf("round-trip: ok=%v back=%q", ok, back)
	}

	// A token under a DIFFERENT label must NOT decrypt through the webdav
	// bridge (Node selects the scheme by the token's label: "unknown
	// access-token-encryptor label").
	if _, ok := wdDecrypt("OTHER-v3:" + tok[len("OL_WEBDAV-v3:"):]); ok {
		t.Fatal("cross-label decrypt must fail")
	}
}

var _ = sitesettings.OpenProvider

func TestWdCredsPlain(t *testing.T) {
	// absent keys dropped; nulls kept; Node key order.
	got := wdCredsPlain(map[string]json.RawMessage{
		"baseUrl":  json.RawMessage(`"https://dav.e2e.invalid"`),
		"password": json.RawMessage(`"s3cret"`),
		"rootPath": json.RawMessage(`null`),
	})
	want := `{"baseUrl":"https://dav.e2e.invalid","password":"s3cret","rootPath":null}`
	if got != want {
		t.Fatalf("creds plain: got %q want %q", got, want)
	}
	// empty body → {}
	if got := wdCredsPlain(map[string]json.RawMessage{}); got != `{}` {
		t.Fatalf("empty creds: got %q", got)
	}
}

func TestWdRemotePath(t *testing.T) {
	cases := []struct{ root, name, want string }{
		{"/Overleaf", "webdav-p69-gate", "/Overleaf/webdav-p69-gate"},
		{"", "proj", "/proj"},
		{"/", "proj", "/proj"},
		{"Overleaf/inner", "proj", "/Overleaf/inner/proj"},
		{"/", "", "/untitled"},
	}
	for _, c := range cases {
		if got := wdRemotePath(c.root, c.name); got != c.want {
			t.Fatalf("remotePath(%q,%q) = %q want %q", c.root, c.name, got, c.want)
		}
	}
}

func TestWdJSNumber(t *testing.T) {
	if got := jsNumberFinite(42); got != "42" {
		t.Fatalf("int: %q", got)
	}
	if got := jsNumberFinite(4.5); got != "4.5" {
		t.Fatalf("float: %q", got)
	}
}

func TestWdCanWrite(t *testing.T) {
	uid, _ := primitive.ObjectIDFromHex("6aa4b8b573ef0e5094f4cbc0")
	coll, _ := primitive.ObjectIDFromHex("6aa4b8b573ef0e5094f4cbc1")
	doc := bson.D{
		{Key: "owner", Value: bson.D{{Key: "userId", Value: uid}}},
		{Key: "owner_ref", Value: uid},
		{Key: "collaberator_refs", Value: bson.A{coll}},
	}
	if !wdCanWrite(uid.Hex(), doc) {
		t.Fatal("owner must be able to write")
	}
	if wdCanWrite("6aa4b8b573ef0e5094f4cbc9", doc) {
		t.Fatal("stranger must not write")
	}
	_ = os.Getenv
}
