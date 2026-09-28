package persistors

// Shared test helpers (recovered from fpersistor_test.go when the fs backend
// was retired in G2 STOR-1 — they are backend-agnostic).

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"testing"
)

func isNotImplErr(err error) bool {
	_, ok := asPersistorError(err).(*NotImplementedError)
	return ok
}

func md5HexOfString(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func mustTempFile(t *testing.T, contents string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "source-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(contents); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}
