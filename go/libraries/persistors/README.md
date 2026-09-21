# `go/libraries/persistors` — object storage persisters

Go 1:1 port of **`libraries/object-persistor`** (npm
`@overleaf/object-persistor`): the **object-storage abstraction** the filestore
and docstore services use to read/write project files and document blobs, with
three backends — **filesystem**, **S3/SeaweedFS**, **GCS** — plus the
**per-project-encrypted S3** variants and the **migration** wrapper. Built
over **narrow seams** (`S3Client`, `GCSStorage`) so the whole suite is
**mock-based, no live cloud**.

## The `Persistor` interface (14 methods)
```
SendFile(location, target, source)                      error
SendStream(location, target, source io.Reader, opts)    error
GetObjectStream(location, name, opts)    (io.ReadCloser, error)
GetRedirectURL(location, name)          (string, error)
GetObjectSize(location, name, opts)     (int64, error)
GetObjectMd5Hash(location, name, opts)  (string, error)
CopyObject(location, fromName, toName, opts)            error
DeleteObject(location, name)                        error
DeleteDirectory(location, name, continuationToken)  error
CheckIfObjectExists(location, name, opts)    (bool, error)
DirectorySize(location, name, continuationToken) (int64, error)
ListDirectoryKeys(location, prefix)   ([]string, error)
ListDirectoryStats(location, prefix)  ([]DirStat, error)
```
`type BasePersistor` is the `AbstractPersistor` stand-in — every method defaults
to a `NotImplementedError` (same message the Node base throws), so a concrete
persistor only overrides what its backend supports.

## The backends
| Type | Built with | Notes |
| --- | --- | --- |
| `func NewFSPersistor(settings FSSettings) (*FSPersistor, error)` | settings | range-capable file stream, force-delete, path sanitization |
| `func NewS3Persistor(…)` | `s3_seam.S3Client` | the SeaweedFS/S3 backend (the `go/s3x` zero-dep client is a host-injected `S3Client`; keeps keys verbatim, hex-md5 → base64 `Content-MD5`) |
| `func NewGcsPersistor(…)` | `gcs_seam.GCSStorage` | the Google Cloud Storage backend |
| `func NewPerProjectEncryptedS3Persistor(…)` | S3 + `RootKeyEncryptionKey` | **per-project encryption** (SSE-C) |
| `func NewCachedPerProjectEncryptedS3Persistor(…)` | the above + a KEK cache | caches the per-project key by project |
| `func NewMigrationPersistor(…, MigrationSettings)` | two persisters | **copy-on-miss fallback** between backends (e.g. moving flat-fs → S3) |

`func Create(settings Settings, adapters Adapters) (Persistor, error)` is the
factory: dispatch on `settings.Backend`, and if a `settings.Fallback` is set,
wrap with a `MigrationPersistor` (the two-backend migration path).

## The encryption surface (per-project S3)
- `func NewRootKeyEncryptionKey(…)` (ssec.go) — the root key derivation.
- `NewNoKEKMatchedError` — no key-encryption-key matched the project.
- `ssec.go` / `projectkey.go` — the SSE-C options (`NewSSECOptions`) and the
  project-key derivation the cache keys by.
- `NewObserver` / `NewAlreadyWrittenError` — the copy-observer + the
  "already written" dedupe guard.

## Conventions / gotchas
- **The cloud is never touched.** `S3Client` and `GCSStorage` are **narrow
  seams** (interfaces); the Node `aws-sdk` / `@google-cloud/storage` are
  replaced by host-injected implementations and by **fake** ones in the tests —
  the oracle is fully mock-based.
- **`DeleteObject` (FS) is force** — a missing file is a no-op (S3 semantics),
  pinned.
- **The gunzip fix:** the Node helper uses a **gzip** stream; the Go port
  decodes gzip (an initial zlib decode was corrected, pinned in tests).
- **`NotImplementedError`** carries the unimplemented method name + args,
  matching the Node base-message, so a backend gap is a clean typed error.

## Testing & coverage
`go test ./go/libraries/persistors/ -count=1 -cover` — oracle-pinned to the Node
`object-persistor` suite, over fake `S3Client`/`GCSStorage` (FSPersistor
against a temp dir, S3/GCS against the fakes, the encryption + migration paths).
**Coverage: 88.6%** (above the 85% gate; **no live cloud required**).

## Dependencies
Standard library (`os`, `io`, `crypto`, `compress/gzip`) +
`ollitex/go/libraries/oerror`. The real `go/s3x` (zero-dep S3) client is a
host-injected `S3Client` implementation (kept out of this package to stay
dependency-free).
