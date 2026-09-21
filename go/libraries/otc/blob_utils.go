package otc

import (
	"crypto/sha1"
	"encoding/hex"
	"hash"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Mirrors libraries/overleaf-editor-core/lib/blob_utils.js.
//
// The git blob hash: sha1("blob " + byteLength + "\0" + content). This is how
// history-v1 addresses blobs and is byte-for-byte git's own object hash, so the
// hash history stores for a file is also the sha that file has in a git
// repository (and on GitHub). Blob.EmptyHash is git's empty-blob hash.

const gitBlobHeaderPrefix = "blob "

// newBlobHash returns a hash.Hash primed with the git blob header for the given
// byte length. It enforces the guard the Node module has: a byte length that is
// not a non-negative integer would yield a header of "blob NaN\0".
func newBlobHash(byteLength int64) (hash.Hash, error) {
	if byteLength < 0 {
		return nil, typeErrorf("blobHash: bad byteLength: %d", byteLength)
	}
	h := sha1.New()
	_, _ = h.Write([]byte(gitBlobHeaderPrefix + strconv.FormatInt(byteLength, 10) + "\x00"))
	return h, nil
}

func finishHash(h hash.Hash) string { return string(hex.EncodeToString(h.Sum(nil))) }

// mustNewBlobHash panics on a negative length — an invariant that can never
// hold for the from-String/Buffer callers (a slice's length is always >= 0).
func mustNewBlobHash(byteLength int64) hash.Hash {
	h, err := newBlobHash(byteLength)
	if err != nil {
		panic(err)
	}
	return h
}

// BlobHashFromString mirrors blobHashFromString (hashes the UTF-8 bytes, using
// the byte length — not the character count — in the header).
func BlobHashFromString(s string) string {
	h := mustNewBlobHash(int64(len([]byte(s))))
	_, _ = h.Write([]byte(s))
	return finishHash(h)
}

// BlobHashFromBuffer mirrors blobHashFromBuffer.
func BlobHashFromBuffer(buf []byte) string {
	h := mustNewBlobHash(int64(len(buf)))
	_, _ = h.Write(buf)
	return finishHash(h)
}

// BlobHashFromStream mirrors blobHashFromStream.
func BlobHashFromStream(byteLength int64, r io.Reader) (string, error) {
	h, err := newBlobHash(byteLength)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return finishHash(h), nil
}

// BlobHashFromFile mirrors blobHashFromFile (stats for the header length, then
// streams the content). A missing file surfaces as a *PathError (ENOENT).
func BlobHashFromFile(pathname string) (string, error) {
	stat, err := os.Stat(pathname)
	if err != nil {
		return "", err
	}
	f, err := os.Open(pathname)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return BlobHashFromStream(stat.Size(), f)
}

// GetStringLengthOfBuffer mirrors getStringLengthOfBuffer: the string length,
// or nil when the content is not editable text.
func GetStringLengthOfBuffer(buf []byte) *int64 {
	if !utf8.Valid(buf) {
		return nil
	}
	data := string(buf)
	units := utf16Units(data)
	if units > MaxStringLength {
		return nil
	}
	if ContainsNonBmpChars(data) {
		return nil
	}
	if strings.ContainsRune(data, 0) {
		return nil
	}
	return int64Ptr(units)
}

// GetStringLengthOfFile mirrors getStringLengthOfFile: skips the read entirely
// for a file too large to be editable.
func GetStringLengthOfFile(byteLength int64, pathname string) (*int64, error) {
	if byteLength > MaxEditableByteLengthBound {
		return nil, nil
	}
	buf, err := os.ReadFile(pathname)
	if err != nil {
		return nil, err
	}
	return GetStringLengthOfBuffer(buf), nil
}

// BlobForFile mirrors blobForFile: the hash, byte length, and (when editable)
// string length of a local file.
func BlobForFile(pathname string) (*Blob, error) {
	stat, err := os.Stat(pathname)
	if err != nil {
		return nil, err
	}
	byteLength := stat.Size()

	f, err := os.Open(pathname)
	if err != nil {
		return nil, err
	}
	hashStr, err := BlobHashFromStream(byteLength, f)
	f.Close()
	if err != nil {
		return nil, err
	}

	stringLength, err := GetStringLengthOfFile(byteLength, pathname)
	if err != nil {
		return nil, err
	}
	return NewBlob(hashStr, byteLength, stringLength), nil
}

func int64Ptr(n int) *int64 {
	v := int64(n)
	return &v
}
