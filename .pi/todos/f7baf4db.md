{
  "id": "f7baf4db",
  "title": "Add the 4 SeaweedFS containers to tools/toolkit (start-first, default storage)",
  "tags": [
    "overleaf",
    "seaweedfs",
    "setup",
    "deferred"
  ],
  "status": "done",
  "created_at": 1789256353077,
  "last_change": 1790704069503,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

DONE 2026-09-29 — commits 9340119402 + correction b5e2a6c757 (owner: this is DEFAULT storage, not optional — pushed). The 4 verified CEP seaweedfs containers (master -\u003e volume/filer -\u003e s3) are toolkit-native: lib/docker-compose.seaweedfs.yml (shared default network, container-name DNS, data under ${OVERLEAF_DATA_PATH}/seaweedfs, ports 9333/19333/18080/8888/18888/8333), bin/docker-compose set_seaweedfs_vars hook, bin/doctor flag print. lib/default.rc: SEAWEEDFS_ENABLED=true DEFAULT (set false to revert to local-fs). When on, the ollitex app is seeded with the S3 storage env: filestore OVERLEAF_FILESTORE_BACKEND=s3 + endpoint http://seaweedfs-s3:8333 + buckets template/blobs/global (auto-created at boot); docstore BACKEND=s3, BUCKET_NAME=archives, AWS_S3_ENDPOINT. Verified: bash -n; docker compose config merge valid (services/order/env/volumes); live topology on this machine (owner CEP stack, 2 weeks uptime): S3 ListBuckets 200, filer 200, 4 containers Up.
