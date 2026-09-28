# junk/ — retired build artifacts

Kept for one-PR rollback; delete freely after the first month without
incident.

| file | retired (date) | replaced by | why |
| --- | --- | --- | --- |
| `Dockerfile-base-ubuntu26.04` | 2026-09-28 | `server-ce/Dockerfile-base` (alpine:3.24) | Owner cutover: alpine passed the full e2e gate and is smaller (6.43GB vs 6.7GB). Restore by copying back + `BASE_FILE=Dockerfile-base-ubuntu26.04`. |
| `images-golang-builder-amd64-ubuntu/Dockerfile` | 2026-09-28 | `images/golang-builder-amd64-alpine/Dockerfile` | Same cutover (musl builder is the default `GO_BUILDER_TAG`). Restore via `GO_BUILDER_TAG=ollitex/golang-builder-amd64:1.27.1` (rebuild the image from this Dockerfile first). |

Note: if you restore the ubuntu base, also restore the ubuntu builder —
the two are paired (glibc↔glibc vs musl↔musl cgo consistency).
