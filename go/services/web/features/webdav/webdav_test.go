// P6.9 webdav — unit checks (offline: cipher round-trip through the shared
// V3 scheme with a per-provider key file, credentials-plain JSON shape,
// path helpers).

package webdav

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"ollitex/go/services/web/features/sitesettings"
)

// TestWdClientBodylessOps — regression (2026-10-02, fixture B4 import):
// bodyless ops (GET / DELETE / MKCOL) panicked inside net/http — wdOnce
// declared `var rdr *bytes.Reader` and passed a TYPED-NIL to
// NewRequestWithContext; net/http's type switch matched *bytes.Reader and
// called .Len() on nil → "invalid memory address or nil pointer"
// (500 "Internal Server Error" from the recovered panic). First caught by the
// tests/tools/webdav fixture (wdImportFiles → cl.get). Fix: body is an
// io.Reader (untyped nil matches `case nil`).
func TestWdClientBodylessOps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			_, _ = w.Write([]byte("file-bytes"))
		case "DELETE":
			w.WriteHeader(204)
		case "MKCOL":
			w.WriteHeader(201)
		case "PUT":
			w.WriteHeader(201)
		default:
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()

	plain := `{"baseUrl":"` + srv.URL + `","username":"u","password":"p","rootPath":"/"}`
	cl, err := wdNewCredsClient(plain)
	if err != nil {
		t.Fatalf("wdNewCredsClient: %v", err)
	}
	ctx := context.Background()

	b, gerr := cl.get(ctx, "/x.txt")
	if gerr != nil || string(b) != "file-bytes" {
		t.Fatalf("get: body=%q err=%v (want file-bytes, nil)", b, gerr)
	}
	if derr := cl.remove(ctx, "/x.txt"); derr != nil {
		t.Fatalf("remove: %v", derr)
	}
	if derr := cl.createDirectory(ctx, "/d"); derr != nil {
		t.Fatalf("createDirectory: %v", derr)
	}
	// the bodied path keeps working (no regression on the other side)
	if perr := cl.put(ctx, "/y.txt", []byte("z"), nil); perr != nil {
		t.Fatalf("put: %v", perr)
	}
}

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
	uid, _ := bson.ObjectIDFromHex("6aa4b8b573ef0e5094f4cbc0")
	coll, _ := bson.ObjectIDFromHex("6aa4b8b573ef0e5094f4cbc1")
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
