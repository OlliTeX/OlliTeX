// Package hashmanager mirrors app/js/HashManager.js — git-style blob SHA1.
//
// The hash is the SHA1 of "blob <byteLength>\x00" followed by the content,
// exactly the format git uses for a loose blob object. We implement both the
// string and file paths (the Node version streams the file).
package hashmanager

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
)

// GetBlobHashFromString returns git blob SHA1 for s.
func GetBlobHashFromString(s string) string {
	blob := "blob " + itoa(len(s)) + "\x00"
	h := sha1.New()
	h.Write([]byte(blob))
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// GetBlobHash returns (sha1, byteLength) for the file at path, mirroring the
// Node streaming implementation.
func GetBlobHash(path string) (string, int64, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", 0, fmt.Errorf("failed to stat file in _getBlobHash: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()
	h := sha1.New()
	if _, err := h.Write([]byte("blob " + itoa(int(st.Size())) + "\x00")); err != nil {
		return "", 0, err
	}
	if _, err := io.Copy(h, f); err != nil {
		return "", 0, fmt.Errorf("error streaming file from disk: %w", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), st.Size(), nil
}
