# Toolkit data backup

One canonical place, one round, verified output:

```
${PREFIX}/data/backups/
├── mongo/<ts>.mongo.gz          # mongodump --gzip --archive (all DBs)
├── redis/<ts>.redis.rdb         # consistent RDB snapshot (SAVE then copy)
├── postgres/
│   ├── <ts>.<db>.sql.gz         # pg_dump --clean --if-exists --no-owner per DB
│   └── <ts>.all.sql.gz          # pg_dumpall (cluster-wide; PG_DUMPALL=0 skips)
└── seaweedfs/
    ├── <ts>.seaweedfs-master.tar.gz   # cluster state
    ├── <ts>.seaweedfs-volume.tar.gz   # the file data (the imported SeaweedFS
    │                                #  content — 12k objects in the 2026-10-06
    │                                #  import lands here)
    └── <ts>.seaweedfs-filer.tar.gz    # filer metadata
```

`<ts>` = UTC `YYYYMMDDTHHMMSSZ`. Keep the **N newest** per store
(`RETENTION`, default 7) — older files are pruned automatically after each
round. Every artifact is verified before it is declared OK: non-zero size,
`gzip -t` (archives/dumps), the `REDIS` header (RDB), and a `sha256` is
printed for the record.

## Run

```sh
sh toolkit/backup/backup-data.sh              # everything
sh toolkit/backup/backup-data.sh mongo pg     # a subset (mongo redis postgres seaweedfs)
```

Host-side or from the toolkit TUI (the same docker daemon and the same
`${PREFIX}/data` bind — the TUI runs this exact script), both work:

```sh
ssh <toolkit> 'sh /opt/ollitex/backup/backup-data.sh'
```

## Restore (default = SAFE: scratch target, live store untouched)

```sh
sh toolkit/backup/restore-mongo.sh      <ts>.mongo.gz                      # into a scratch DB
sh toolkit/backup/restore-mongo.sh      <ts>.mongo.gz ollitex --to-live    # into the live DB
sh toolkit/backup/restore-redis.sh      <ts>.redis.rdb --yes
sh toolkit/backup/restore-pg.sh         <ts>.overleaf.sql.gz --new-db drill
sh toolkit/backup/restore-seaweedfs.sh  <ts>.seaweedfs-volume.tar.gz                       # drill (no live change)
sh toolkit/backup/restore-seaweedfs.sh  <ts>.seaweedfs-volume.tar.gz --to-live
```

## Notes

- The content DB is the **`ollitex`** database (the new canonical app DB);
  the legacy `sharelatex` database is **not** renamed/dropped — its data
  travels in the same `mongodump` archive and is restored back-to-back, so a
  restore preserves both.
- SeaweedFS **volume** is the stateful store for the app's filestore (S3).
  Restoring `volume` + `filer` + `master` together reconstructs the object
  store; restoring only `volume` reproduces the blobs (S3 path-style keys).
- pg backups are per-database (`overleaf`, `wakapi`, `postgres`) + a
  cluster-wide `pg_dumpall`. `configdb` (the hub/toolkit config) lives in a
  per-database dump.
- `RETENTION` prunes per store, newest-first, after each successful round.
  Set `RETENTION=999` to keep everything.
- This suite is additive to (not a replacement for) the owner's existing
  `compose_cep/{overleafmongo,overleafredis}/backup.sh` — those keep working
  independently for the prod `overleafserver` plane; this one is the
  canonical-toolkit-plane backup surface (mongo/redis shared store +
  postgres + SeaweedFS).
