// Package contenthash ports `storage/lib/content_hash.js` and the git-blob
// hash helpers from `overleaf-editor-core/lib/blob_utils.js`.
//
// The git blob hash (sha1("blob " + byteLength + "\x00" + content)) and
// EMPTY_HASH are reused from the shared `go/libraries/otc` package so the two
// sides maintain one implementation; the plain sha1 content hash (history's
// content hash) has no upstream equivalent and stays local.
package contenthash

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"

	otc "ollitex/go/libraries/otc"
)

// EMPTY_HASH is the git hash of the empty blob: "blob 0\0" (otc.EmptyHash).
const EMPTY_HASH = otc.EmptyHash

// HEXHashLength is the number of hex digits in a blob hash.
const HEXHashLength = 40

// hexHashRx — the canonical 40-hex-char pattern, reused from otc (Blob.HEX_HASH_RX).
var hexHashRx = regexp.MustCompile(otc.HexHashRxString)

// HEXHashRX reports whether s is a valid 40-digit lowercase hex blob hash
// (pattern reused from otc.HexHashRxString).
func HEXHashRX(s string) bool {
	if len(s) != HEXHashLength {
		return false
	}
	return hexHashRx.MatchString(s)
}

// ContentHash returns the hex SHA-1 of content (mirrors content_hash.js).
// Not shared upstream (otc does not port the plain-sha1 history content hash).
func ContentHash(content string) string {
	h := sha1.Sum([]byte(content))
	return hex.EncodeToString(h[:])
}

// BlobHashForBytes returns the git blob hash for raw bytes (otc.BlobHashFromBuffer).
func BlobHashForBytes(content []byte) string {
	return otc.BlobHashFromBuffer(content)
}

// BlobHash is the git blob hash for a Go string, counting UTF-8 bytes as the
// length in the git header (matches Node's Buffer.byteLength(string));
// delegates to otc.BlobHashFromString.
func BlobHash(content string) string {
	return otc.BlobHashFromString(content)
}

// emptyBlobHash — the empty-blob hash (otc, byte length 0, no content).
func emptyBlobHash() string {
	return otc.BlobHashFromBuffer(nil)
}

// init anchors that the EMPTY_HASH constant is git's sha1("blob 0\0").
func init() {
	if emptyBlobHash() != EMPTY_HASH {
		panic(`contenthash: EMPTY_HASH constant does not match sha1("blob 0\0")`)
	}
}
