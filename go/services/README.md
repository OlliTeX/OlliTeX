# `go/services` — the Go microservice ports (Node `services/*`)

Each sub-folder is a 1:1 Go drop-in of one Node service: same HTTP API, same
MongoDB collections, same cross-service behaviour. Ports landed via the
GO_CUTOVER_PLAN Phase D (commit `8090d454fb`) — see each folder's README for
its own 1:1 mapping table.

| service | port | Node original | note |
| --- | --- | --- | --- |
| `chat` | 3010 | `services/chat` | AI-chat rooms/messages + notification writes |
| `datamanipulator` | internal (token-protected) | `services/datamanipulator` | data import/export workers |
| `docstore` | 3016 | `services/docstore` | document history store |
| `dropboxinterface` | internal (token-protected) | `services/dropbox-interface` | dropbox OAuth + file sync |
| `filestore` | 3009 | `services/filestore` | blob storage (GCS/S3/SeaweedFS) |
| `githubinterface` | internal (token-protected) | `services/github-interface` | github import/export + webhooks |
| `linked-url-proxy` | 3066 | `services/linked-url-proxy` | linked-URL import/preview |
| `notifications` | 3042 | `services/notifications` | notification centre API |
| `webdavinterface` | internal (token-protected) | `services/webdav-interface` | webdav sync + sync engine |
| `gitbridge` | 8000 | Go service (Java `services/git-bridge` deleted, D14) | the Git Bridge (JGit → `git` CLI port; `GIT_BRIDGE_PORT`) |
| `web` | 4000 Node / 4010 Go | `services/web` | the monolithic web backend (the P1…P6 cutover) |

Two cross-cutting packages at the `go/` level support these services:
`s3x` (minimal S3 client) and `pbhttp` (protobuf-over-HTTP client for the
filestore gRPC-style API). Shared building blocks live in `go/libraries`
(see `../libraries/README.md`) and the top-level `../README.md`.

Build/test: from the repo root, `go build ./go/services/<svc> && go test
./go/services/<svc>/...`. Each service also has a live e2e/parity gate under
`tests/e2e/specs/parity/`.
