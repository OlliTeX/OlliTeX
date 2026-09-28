package otpure

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"regexp"
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

// NewBlobHash returns a hash.Hash primed with the git blob header for the given
// byte length. It enforces the guard the Node module has: a byte length that is
// not a non-negative integer would yield a header of "blob NaN\0".
func NewBlobHash(byteLength int64) (hash.Hash, error) {
	if byteLength < 0 {
		return nil, fmt.Errorf("blobHash: bad byteLength: %d", byteLength)
	}
	h := sha1.New()
	_, _ = h.Write([]byte(gitBlobHeaderPrefix + strconv.FormatInt(byteLength, 10) + "\x00"))
	return h, nil
}

func FinishBlobHash(h hash.Hash) string { return string(hex.EncodeToString(h.Sum(nil))) }

// MustNewBlobHash panics on a negative length — an invariant that can never
// hold for the from-String/Buffer callers (a slice's length is always >= 0).
func MustNewBlobHash(byteLength int64) hash.Hash {
	h, err := NewBlobHash(byteLength)
	if err != nil {
		panic(err)
	}
	return h
}

// BlobHashFromString mirrors blobHashFromString (hashes the UTF-8 bytes, using
// the byte length — not the character count — in the header).
func BlobHashFromString(s string) string {
	h := MustNewBlobHash(int64(len([]byte(s))))
	_, _ = h.Write([]byte(s))
	return FinishBlobHash(h)
}

// BlobHashFromBuffer mirrors blobHashFromBuffer.
func BlobHashFromBuffer(buf []byte) string {
	h := MustNewBlobHash(int64(len(buf)))
	_, _ = h.Write(buf)
	return FinishBlobHash(h)
}

// BlobHashFromStream mirrors blobHashFromStream.
func BlobHashFromStream(byteLength int64, r io.Reader) (string, error) {
	h, err := NewBlobHash(byteLength)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return FinishBlobHash(h), nil
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
	units := UTF16Units(data)
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
	if byteLength > maxEditableByteLengthBound {
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

// ---- pure constants (moved from otc/blob.go + otc/text_operation.go) ----

var HexHashRx = regexp.MustCompile(`^[0-9a-f]{40,40}$`)

const (
	// maxEditableByteLengthBound bounds the byte length of a file that might be
	// editable: 3 * MaxStringLength (a BMP char is at most 3 UTF-8 bytes).
	maxEditableByteLengthBound = 3 * MaxStringLength
	// MaxEditableByteLengthBound — public name (otc model + tests share it).
	MaxEditableByteLengthBound = maxEditableByteLengthBound
	// HexHashRxString is the hash pattern (a git/sha1 hex digest).
	HexHashRxString = `^[0-9a-f]{40,40}$`

	// MaxStringLength is the longest file we will attempt to edit (moved from
	// otc/text_operation.go — pure, otc-free).
	MaxStringLength = 3 * 1024 * 1024

	// EmptyHash is the git hash of an empty blob.
	EmptyHash = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
)

func int64Ptr(n int) *int64 {
	v := int64(n)
	return &v
}
