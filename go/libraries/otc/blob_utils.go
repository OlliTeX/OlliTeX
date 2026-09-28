package otc

import (
	"os"

	"ollitex/go/libraries/otpure"
)

// blob_utils.go — the pure hash / string-length surface moved to
// ollitex/go/libraries/otpure (d5dd23dd S1a). Kept here: BlobForFile,
// which returns the OT Blob model (stays with the model) over the otpure
// hash primitives.

// BlobForFile mirrors blobForFile: the hash, byte length, and (when
// editable) string length of a local file.
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
	hashStr, err := otpure.BlobHashFromStream(byteLength, f)
	f.Close()
	if err != nil {
		return nil, err
	}

	stringLength, err := otpure.GetStringLengthOfFile(byteLength, pathname)
	if err != nil {
		return nil, err
	}
	return NewBlob(hashStr, byteLength, stringLength), nil
}

func int64Ptr(n int) *int64 {
	v := int64(n)
	return &v
}
