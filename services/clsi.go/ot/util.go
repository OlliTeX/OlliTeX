package ot

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"unicode/utf16"
)

// EmptyHash is the hash of empty content, the same for every project: git's
// hash of an empty blob as computed by BlobHashFromString (a property of the
// content, so it can be recognised without looking anything up).
// File.emptyFileHash is the same value.
const EmptyHash = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"

// utf16Len returns the length of s in UTF-16 code units, matching the JS
// str.length. Go's range iterates in code points (runes); non-BMP characters
// are two UTF-16 code units (a surrogate pair), not one rune.
func utf16Len(s string) int {
	if s == "" {
		return 0
	}
	return len(utf16.Encode([]rune(s)))
}

// containsNonBmpChars reports whether s contains any character outside the
// basic multilingual plane. Port of util.js:
//
//	/[\uD800-\uDBFF]/.test(str)
//
// On JSON-decoded input (all CLSI content paths), that regex matches exactly
// strings containing runes > 0xFFFF (the high surrogate of each non-BMP
// pair); strings arriving through encoding/json never carry lone surrogates.
func containsNonBmpChars(s string) bool {
	for _, r := range s {
		if r > 0xFFFF {
			return true
		}
	}
	return false
}

// byteLength mirrors Buffer.byteLength(str) on the Node oracle: the number of
// bytes in the UTF-8 encoding of the string.
func byteLengthOf(s string) int {
	return len([]byte(s))
}

// blobHashFromString computes the git blob hash of content:
//
//	sha1("blob " + byteLength + "\x00" + content)
//
// with byteLength the UTF-8 byte length of content.
func blobHashFromString(content string) string {
	header := "blob " + fmt.Sprintf("%d", byteLengthOf(content)) + "\x00"
	h := sha1.Sum([]byte(header + content))
	return hex.EncodeToString(h[:])
}

// maxStringLength is the cap on the resulting length of a text edit, in UTF-16
// code units (TextOperation.MAX_STRING_LENGTH = 3 * 1024^2).
const maxStringLength = 3 * 1024 * 1024

// utf16EncodeStr encodes a Go string to UTF-16 code units, mirroring a JS
// string (which is an array of 16-bit units).
func utf16EncodeStr(s string) []uint16 {
	return utf16.Encode([]rune(s))
}

// utf16DecodeString decodes UTF-16 code units to a Go string.
func utf16DecodeString(units []uint16) string {
	return string(utf16.Decode(units))
}

// utf16DecodeUnits is an alias kept for call-site clarity in normalize.
func utf16DecodeUnits(units []uint16) string {
	return utf16DecodeString(units)
}

// jsonString mirrors JSON.stringify(v) for the wire shapes produced by
// encoding/json. Used in "Invalid ScanOp <...>" UnprocessableError messages.
func jsonString(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
