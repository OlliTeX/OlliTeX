package persistors

import (
	"io"
	"strconv"
)

// GCS seam — the narrow @google-cloud/storage slice GcsPersistor.js uses.
// Node injects `new Storage(options)`; tests stub it per bucket/file. The Go
// seam mirrors the exact call surface:
//
//	storage.bucket(name).file(key[, {generation}]).
//	  createWriteStream(opts) / createReadStream(opts) / getMetadata() /
//	  delete() / copy(dest) / setMetadata(map) / exists()
//	storage.bucket(name).getFiles({prefix, autoPaginate})
//
// Adapters: tests use fakeGCS; a runtime adapter wraps cloud.google.com/go/
// storage (or the repo's existing GCS plumbing) behind this interface.

// GCSWriteOptions mirrors the createWriteStream options object.
type GCSWriteOptions struct {
	Resumable  bool
	Validation string            // 'md5' when sourceMd5 is supplied
	Metadata   map[string]string // md5Hash / contentType / contentEncoding
}

// GCSReadOptions mirrors createReadStream options ({decompress:false, ...opts}).
type GCSReadOptions struct {
	Decompress bool
	Start      *int64
	End        *int64
}

// GCSReadStream is the createReadStream result with the observable response
// (Node: the stream's `response` event: 200 / 206 / 404 / other).
type GCSReadStream struct {
	Status          int
	ContentEncoding string
	Body            io.ReadCloser
}

// GCSFileMetadata mirrors the file metadata object (size/md5Hash are
// STRINGS in the GCS API; eventBasedHold for the unlock-before-delete path).
type GCSFileMetadata struct {
	Size           string
	MD5Hash        string
	EventBasedHold *bool
}

// GCSFile mirrors storage.bucket().file() — one object.
type GCSFile interface {
	CreateWriteStream(opts GCSWriteOptions) (io.WriteCloser, error)
	CreateReadStream(opts GCSReadOptions) (*GCSReadStream, error)
	GetMetadata() (*GCSFileMetadata, error)
	Delete() error
	Copy(dest *GCSFile) error
	SetMetadata(map[string]any) error
	Exists() (bool, error)
}

// GCSListedFile is one getFiles entry ({name, metadata}).
type GCSListedFile struct {
	Name     string
	Metadata GCSFileMetadata
}

// GCSQuery is the getFiles query ({prefix, autoPaginate, nextQuery}).
type GCSQuery struct {
	Prefix       string
	AutoPaginate bool
	NextQuery    *GCSQuery // the page iterator (Node: [files, nextQuery])
}

// GCSBucket mirrors storage.bucket().
type GCSBucket interface {
	File(name string, generation ...int64) GCSFile
	GetFiles(q GCSQuery) ([]GCSListedFile, *GCSQuery, error)
}

// GCSStorage mirrors new Storage(options).
type GCSStorage interface {
	Bucket(name string) GCSBucket
}

// GCSStorageFactory — the constructor seam (Node: `new Storage(opts)`).
// Adapters build the real client from GCSStorageOptions.
type GCSStorageFactory func(options map[string]any) (GCSStorage, error)

// GCSFileOpts mirrors the file options ({generation: 0}).
type GCSFileOpts struct {
	Generation int64
}

// GCSListed... done above. GCSStatusError carries the numeric err.code
// (404/412) Node's GCS errors use.
type GCSStatusError struct {
	ErrorCode int // numeric error code (404/412), as String
	Message   string
	Err       error
}

func (e *GCSStatusError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "gcs error"
}

// Code / StatusCode implement the codedErr / statusErr seams (Node GCS
// errors: err.code === 404).
func (e *GCSStatusError) Code() string {
	if e.ErrorCode == 0 {
		return ""
	}
	return strconv.Itoa(e.ErrorCode)
}
func (e *GCSStatusError) StatusCode() int { return e.ErrorCode }
func (e *GCSStatusError) Unwrap() error   { return e.Err }
