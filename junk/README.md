# junk/ — retired build artifacts

Kept for one-PR rollback; delete freely after the first month without
incident.

| file | retired (date) | replaced by | why |
| --- | --- | --- | --- |
| `Dockerfile-base-ubuntu26.04` | 2026-09-28 | `server-ce/Dockerfile-base` (alpine:3.24) | Owner cutover: alpine passed the full e2e gate and is smaller (6.43GB vs 6.7GB). Restore by copying back + `BASE_FILE=Dockerfile-base-ubuntu26.04`. |
| `images-golang-builder-amd64-ubuntu/Dockerfile` | 2026-09-28 | `images/golang-builder-amd64-alpine/Dockerfile` | Same cutover (musl builder is the default `GO_BUILDER_TAG`). Restore via `GO_BUILDER_TAG=ollitex/golang-builder-amd64:1.27.1` (rebuild the image from this Dockerfile first). |

Note: if you restore the ubuntu base, also restore the ubuntu builder —
the two are paired (glibc↔glibc vs musl↔musl cgo consistency).

## go-services-history-v1 + go-services-document-updater (2026-09-28, otc-retirement stage 1)
Uncommitted local Go port attempts (never in the committed tree):
- `go-services-history-v1` — early Go H1 port (66 files, "FULL GATE GREEN" 2026-09-21),
  orphaned when the D41/S4 pivots shipped the thin `go/services/historyv1` (live, :3100).
- `go-services-document-updater` — Go DU port (62 files, Phases 1-7), orphaned when
  D41 retired DU's content plane to docstore-direct (DU retirement = D41-DU S3, d5dd23dd).
Both consumed `go/libraries/otc`; removed to shrink the otc consumer surface for the
otc retirement (35ed23bd, blocked on d5dd23dd for the live H1/PH consumers).
Restore: `mv junk/<tree> go/services/<name>` (module: root go.mod `ollitex`).
