package otc

import (
	"regexp"
	"strconv"
)

// Mirrors libraries/overleaf-editor-core/lib/blob.js.

// hexHashRx matches a 40-hex-char hash.
var hexHashRx = regexp.MustCompile(`^[0-9a-f]{40,40}$`)

const (
	// HexHashRxString is the hash pattern (a git/sha1 hex digest).
	HexHashRxString = `^[0-9a-f]{40,40}$`
	// MaxEditableByteLengthBound bounds the byte length of a file that might be
	// editable: 3 * MaxStringLength (a BMP char is at most 3 UTF-8 bytes).
	MaxEditableByteLengthBound = 3 * MaxStringLength
	// EmptyHash is the git hash of an empty blob.
	EmptyHash = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
)

// RawBlob is the wire shape consumed/produced by BlobFromRaw/ToRaw.
type RawBlob struct {
	Hash         string
	ByteLength   int64
	StringLength *int64
}

// Blob is a metadata record for the content of a file.
type Blob struct {
	Hash         string
	ByteLength   int64
	StringLength *int64 // nil == not known / not editable text (Node undefined)
}

// NewBlob mirrors the Blob constructor (setHash validates the hash; byteLength
// must be a non-negative integer; stringLength may be nil).
func NewBlob(hash string, byteLength int64, stringLength *int64) *Blob {
	newBlob(hash, byteLength)
	return &Blob{Hash: hash, ByteLength: byteLength, StringLength: stringLength}
}

// newBlob mirrors setHash + setByteLength's assertions, panicking a typed
// *typeError (the Node check-types TypeError) on a bad value.
func newBlob(hash string, byteLength int64) {
	if !hexHashRx.MatchString(hash) {
		panic(newTypeError("blob: bad hash " + hash))
	}
	if byteLength < 0 {
		panic(newTypeError("blob: bad byteLength " + strconv.FormatInt(byteLength, 10)))
	}
}

// BlobFromRaw mirrors Blob.fromRaw (nullish raw → nil).
func BlobFromRaw(raw *RawBlob) *Blob {
	if raw == nil {
		return nil
	}
	return NewBlob(raw.Hash, raw.ByteLength, raw.StringLength)
}

// ToRaw mirrors Blob.toRaw.
func (b *Blob) ToRaw() *RawBlob {
	return &RawBlob{Hash: b.Hash, ByteLength: b.ByteLength, StringLength: b.StringLength}
}

// GetHash returns the hex hash.
func (b *Blob) GetHash() string { return b.Hash }

// GetByteLength returns the length in bytes.
func (b *Blob) GetByteLength() int64 { return b.ByteLength }

// GetStringLength returns the UTF-8 length, or nil when not known.
func (b *Blob) GetStringLength() *int64 { return b.StringLength }

// BlobNotFoundError mirrors Blob.NotFoundError ("blob <hash> not found",
// info {hash}).
type BlobNotFoundError struct{ Hash string }

func (e *BlobNotFoundError) Error() string { return "blob " + e.Hash + " not found" }

func NewBlobNotFound(hash string) *BlobNotFoundError {
	return &BlobNotFoundError{Hash: hash}
}
