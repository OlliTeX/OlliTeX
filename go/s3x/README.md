# `s3x` — a minimal, dependency-free S3 client

A small S3 (path-style) HTTP client for SeaweedFS's S3 gateway and other
S3-compatible object stores — **no AWS SDK dependency**. It implements exactly
the operations the Overleaf persistors and the gitbridge swap store need:

| method | purpose |
| --- | --- |
| `New(endpoint, key, secret)` | client + SigV4 signing |
| `CreateBucket` / `BucketExists` | bucket lifecycle |
| `PutObject` (returns ETag) | upload (with optional `sourceMD5`) |
| `HeadObject` → (size, etag) | existence/size check |
| `GetObject` → `*GetResult` (stream + headers) | download |
| `DeleteObject` | delete |
| `ListObjects(bucket, prefix)` | prefix listing |
| `Ping` | liveness (health gate) |
| `IsConnectionErr(err)` | the connection-error classification the callers rely on for retry semantics |

Used by: `go/libraries/persistors` (S3 persistor) and
`go/services/gitbridge/swap` (the S3 swap store).

Tests: `s3x_test.go` — the signing + request-shape contract against a fake
S3 endpoint.
