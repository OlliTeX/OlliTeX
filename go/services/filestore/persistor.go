package filestore

import (
	"io"
)

// Store is the persistor abstraction the filestore handlers programme against.
// One backend implements it (G2 STOR-1, owner-approved S3-only durable backend):
//
//   - s3xStore — S3Persistor on a SeaweedFS/S3 gateway (BACKEND='s3')
//
// (The CE-default FSPersistor/'fs' backend was retired with G2: durable
// object storage is S3-only; the fs path of libraries/object-persistor parity
// no longer exists in-tree. Working-set FS — upload staging, CLSI workspace,
// git worktree, caches — is a separate plane and stays local.)
//
// Semantics honoured (1:1 with the Node S3Persistor):
//   - "location" is the bucket NAME; keys are handler-level keys (may contain
//     '/') and are stored verbatim (S3 does not flatten).
//   - missing key → fseNotFound (404); deleting a missing key is a no-op.
type Store interface {
	open(location, key string, reqUseSub bool) (io.ReadCloser, error)
	objectSize(location, key string, reqUseSub bool) (int64, error)
	objectMd5(location, key string, reqUseSub bool) (string, error)
	exists(location, key string, reqUseSub bool) bool
	sendStream(location, key string, r io.Reader, reqUseSub bool, sourceMd5 string) error
	sendFile(location, key, source string, reqUseSub bool) error
	copyObject(location, from, to string, reqUseSub bool) error
	deleteObject(location, key string, reqUseSub bool) error
	// deleteDirectory sweeps everything under the key prefix — S3 lists with
	// Prefix=key (1:1 with Node S3Persistor.#listDirectory `Prefix: key`, no
	// trailing slash).
	deleteDirectory(location, key string, reqUseSub bool) error
	listFiles(location, key string, reqUseSub bool) []string
}
