# OlliTeX — legacy → canonical import / migration toolkit

Import the **legacy Overleaf** data into the **canonical OlliTeX** stores, and
re-point the OlliTeX web plane at a **new `ollitex` database** — without ever
renaming, dropping, or mutating the legacy `sharelatex` database.

## Owner model (2026-10-06)

* The legacy `sharelatex` MongoDB database is the old Overleaf install's data.
  We **never** rename or destroy it — a future Overleaf install may still own it.
* We **create a new `ollitex` database** in the canonical `overleafmongo` and
  populate it by restoring the legacy `sharelatex` dump (rename-on-restore).
* The legacy **filestore content goes to SeaweedFS** (the canonical S3 plane) —
  the new stack has **no fs filestore** (retired, G2 STOR-1: the filestore
  service is S3-only). It lands in bucket `${S3_IMPORT_BUCKET:-ollitex-legacy}`
  with the legacy layout kept verbatim as object keys.
* The legacy source tree under `testdata/` is opened **read-only** and never
  modified.
* The whole thing is oriented at the **canonical `../compose.yaml`** stack
  (the production system), configured through `../.env`.

## Layout

| File              | Role                                                        |
|-------------------|-------------------------------------------------------------|
| `common.conf`     | shared config + helpers. Precedence: env > `../.env` > defaults. |
| `mongo-import.sh` | legacy `sharelatex` dump → NEW `ollitex` DB (rename-on-restore). |
| `files-import.sh` | legacy filestore → **SeaweedFS S3** bucket (objects, verbatim keys). |
| `s3-import.py`    | the S3 uploader/verifier used by `files-import.sh` (boto3).  |
| `redis-import.sh` | **optional** legacy RDB → canonical Redis (ephemeral; needs `--yes`). |
| `run-all.sh`      | orchestrates mongo → files → verify in a safe order.        |
| `verify.sh`       | proves the import landed (DB + S3 bucket + app health).     |

## Usage

```sh
cd toolkit/migration

# 1) see the plan, change nothing
./run-all.sh --dry-run

# 2) do the import (mongo + files→SeaweedFS; safe defaults; redis left alone)
./run-all.sh

# 3) re-do from scratch (drops the NEW ollitex DB + resets the import bucket)
./run-all.sh --reimport

# 4) also import the ephemeral build leftovers (cache/, compiles/)
./run-all.sh --with-ephemeral

# 5) also replace the canonical Redis RDB (ephemeral; destructive by design)
./run-all.sh --with-redis --yes

# 6) prove it landed
./verify.sh
```

Individual steps are runnable on their own (`./mongo-import.sh`,
`./files-import.sh --help`, etc.).

## What the import does, concretely

1. **Mongo** — `mongorestore --db ollitex <legacy>/sharelatex`
   inside `overleafmongo`. All legacy collections land in the **new** `ollitex`
   DB. `sharelatex` is verified unchanged (collection count before/after).
2. **Files (→ SeaweedFS)** — the legacy filestore stores are uploaded as S3
   objects into the canonical SeaweedFS plane:

   | legacy store (read-only)      | imported by default | object key layout          |
   |-------------------------------|---------------------|----------------------------|
   | `user_files/<pid>/...`        | yes (often empty — source files had moved to git/docstore) | `user_files/...`   |
   | `template_files/...`          | yes                 | `template_files/...`       |
   | `output/<pid>/...`            | yes (generated output) | `output/...`            |
   | `history/...`                 | yes (doc history — the bulk of the data) | `history/...` |
   | `cache/`, `compiles/`         | **no** (ephemeral build leftovers; `--with-ephemeral`) | — |
   | `<root>/*.zip` (ex-exports)   | yes, as `exports/<name>.zip` | `exports/...` |

   Anonymous S3 (the canonical plane runs without auth); creds are optional
   (`S3_IMPORT_ID`/`S3_IMPORT_KEY`).
3. **Redis** — *not* touched by default. Redis holds only ephemeral state
   (sessions/pub-sub/queue); import it only if you specifically need the old
   sessions.

## Safety guarantees

* `LEGACY_DB != OLLITEX_DB` is enforced (`preflight_safety`); the tool refuses
  to run if they are equal (i.e. it can never mutate the legacy store).
* `--reimport` only ever affects the **new** `ollitex` DB and the **import
  bucket**, never the legacy `sharelatex` DB or the `testdata/` sources.
* The S3 target is a dedicated import bucket — the app's own future buckets
  are never touched.
* A full mongorestore log is kept at `${PREFIX}/data/mongorestore-ollitex-last.log`.

## Config (from `../.env`, overridable in the shell)

| Key                     | Meaning                                            | Default (this box)                          |
|-------------------------|----------------------------------------------------|---------------------------------------------|
| `PREFIX`                | canonical data root                                | `/data_1/test_x/opt/ollitex`                |
| `OLLITEX_DB`            | NEW target DB name                                 | `ollitex`                                   |
| `LEGACY_DB`             | source DB name in the dump (read-only)             | `sharelatex`                                |
| `LEGACY_ROOT`           | root of the provided legacy testdata               | `/data_1/image_mining/the_diff/testdata`    |
| `SEAWEEDFS_S3_PORT`     | canonical SeaweedFS S3 host port (stack param)     | `28888` (normal boxes: `8333`)              |
| `S3_IMPORT_BUCKET`      | import bucket name (dedicated)                     | `ollitex-legacy`                            |
| `S3_IMPORT_ID/KEY`      | optional S3 creds (empty = anonymous)              | *(empty)*                                   |
