package ot

// Port of blob.js and blob_store_base.js.

import (
	"encoding/json"
	"fmt"
)

// MaxEditableByteLengthBound is the size of the largest file that we'll read
// to determine whether we can edit it or not, in bytes. Mirrors
// `Blob.MAX_EDITABLE_BYTE_LENGTH_BOUND = 3 * TextOperation.MAX_STRING_LENGTH`.
const MaxEditableByteLengthBound = 3 * maxStringLength

// EmptyBlobHash is the hash of empty content, which is the same for every
// project. This duplicates EmptyHash as a second name, mirroring
// `Blob.EMPTY_HASH` and `File.EMPTY_HASH` both equal in the JS lib.
const EmptyBlobHash = EmptyHash

// Blob is the metadata record for the content of a file.
type Blob struct {
	hash         string
	byteLength   int
	stringLength *int // nil = undefined
}

// NewBlob constructs a Blob, validating hash format and byteLength >= 0.
// Mirrors Blob constructor + check-types assertions.
func NewBlob(hash string, byteLength int, stringLength *int) *Blob {
	if !matchHexHash(hash) {
		panic(fmt.Errorf("bad hash"))
	}
	if byteLength < 0 {
		panic(fmt.Errorf("bad byteLength"))
	}
	return &Blob{hash: hash, byteLength: byteLength, stringLength: stringLength}
}

// BlobFromRaw mirrors `Blob.fromRaw(raw)`: nil when raw is nil, else a new
// Blob. Mirrors the JS `if (raw)` guard for truthy/falsy raw.
func BlobFromRaw(hash string, byteLength int, stringLength *int) *Blob {
	if hash == "" {
		return nil
	}
	return NewBlob(hash, byteLength, stringLength)
}

// GetHash returns the hex hash.
func (b *Blob) GetHash() string { return b.hash }

// SetHash mutates the hash (after validation).
func (b *Blob) SetHash(hash string) {
	if !matchHexHash(hash) {
		panic(fmt.Errorf("bad hash"))
	}
	b.hash = hash
}

// GetByteLength returns the size of the blob in bytes.
func (b *Blob) GetByteLength() int { return b.byteLength }

// SetByteLength mutates the size if valid.
func (b *Blob) SetByteLength(byteLength int) {
	if byteLength < 0 {
		panic(fmt.Errorf("bad byteLength"))
	}
	b.byteLength = byteLength
}

// GetStringLength returns the utf-8 string length, if known.
func (b *Blob) GetStringLength() *int { return b.stringLength }

// SetStringLength mutates the string length, allowing nil.
func (b *Blob) SetStringLength(stringLength *int) {
	b.stringLength = stringLength
}

// ToRaw mirrors toRaw.
func (b *Blob) ToRaw() map[string]any {
	var sl any
	if b.stringLength != nil {
		sl = *b.stringLength
	}
	return map[string]any{
		"hash":         b.hash,
		"byteLength":   b.byteLength,
		"stringLength": sl,
	}
}

// matchHexHash mirrors `Blob.HEX_HASH_RX` = /^[0-9a-f]{40,40}$/.
func matchHexHash(hash string) bool {
	if len(hash) != 40 {
		return false
	}
	for i := 0; i < 40; i++ {
		c := hash[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// --- blob_store_base.js ---

// BlobStore mirrors BlobStoreBase plus a concrete fetchString. The zero value
// of FetchString (nil) mirrors the JS `fetchString` that throws "does not
// implement fetchString(hash)".
type BlobStore struct {
	// FetchString is what this blob store actually reads. When nil, GetString
	// (for hashes other than EMPTY_HASH) returns an error mirroring the
	// unimplemented abstract method.
	FetchString func(hash string) (string, error)
}

// GetString fetches a blob's content as a string. When the hash is EMPTY_HASH
// return ” without a fetch (mirrors BlobStoreBase.getString).
func (s *BlobStore) GetString(hash string) (string, error) {
	if hash == EmptyBlobHash {
		return "", nil
	}
	if s.FetchString != nil {
		return s.FetchString(hash)
	}
	return "", fmt.Errorf("BlobStore does not implement fetchString(hash)")
}

// GetObject fetches a blob holding JSON and deserializes it.
func (s *BlobStore) GetObject(hash string) (any, error) {
	str, err := s.GetString(hash)
	if err != nil {
		return nil, err
	}
	// JSON.parse: mirror: empty string fails.
	var v any
	if err := json.Unmarshal([]byte(str), &v); err != nil {
		return nil, err
	}
	return v, nil
}
