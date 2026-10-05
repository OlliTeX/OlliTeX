#!/usr/bin/env python3
"""s3-import.py — upload legacy Overleaf filestore stores into SeaweedFS (S3).

Driven by files-import.sh (which resolves config from common.conf / .env).
Env (required):  S3_ENDPOINT   e.g. http://127.0.0.1:28888 or http://seaweedfs-s3:8333
                 BUCKET        e.g. ollitex-legacy
                 LEGACY_DATA   legacy /var/lib/overleaf/data root (READ-ONLY source)
Optional:        S3_ID, S3_KEY (empty = anonymous — the canonical S3 runs authless)
                 STORES        comma list; default: user_files,template_files,output,history
                 EXTRA         semicolon list of  <src path>=<destination key>  objects
                 SKIP_EXISTING 1 = skip objects already present with identical MD5
                 REIMPORT      1 = empty the bucket before importing
                 WORKERS       parallel upload workers (default 16)

Keys are stored VERBATIM as  <store>/<relative path>  — a faithful mirror of
the legacy FS layout under the object store.
"""
import boto3
import botocore.client
import hashlib
import os
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor


def log(msg):
    print(f"[s3-import] {msg}", flush=True)


def md5file(path, chunk=1 << 20):
    h = hashlib.md5()
    with open(path, "rb") as f:
        while True:
            b = f.read(chunk)
            if not b:
                break
            h.update(b)
    return h.hexdigest()


def bucket_objects(s3, bucket):
    p = {}
    while True:
        p = s3.list_objects_v2(Bucket=bucket, **({"ContinuationToken": p["NextContinuationToken"]} if p.get("NextContinuationToken") else {}))
        yield from p.get("Contents", [])
        if not p.get("IsTruncated"):
            break


class Stats:
    def __init__(self):
        self.lock = threading.Lock()
        self.up = self.skip = self.err = self.bytes = 0
        self.done = 0
        self.total = 0

    def add(self, kind, nbytes=0):
        with self.lock:
            self.done += 1
            if kind == "up":
                self.up += 1
                self.bytes += nbytes
            elif kind == "skip":
                self.skip += 1
            else:
                self.err += 1

    def report(self):
        with self.lock:
            return f"{self.done}/{self.total} up={self.up} skip={self.skip} err={self.err} {self.bytes/(1<<20):.0f}MiB"


def main():
    endpoint = os.environ["S3_ENDPOINT"].rstrip("/")
    bucket = os.environ["BUCKET"]
    legacy = os.environ["LEGACY_DATA"]
    skip_existing = os.environ.get("SKIP_EXISTING", "0") == "1"
    reimport = os.environ.get("REIMPORT", "0") == "1"
    workers = int(os.environ.get("WORKERS", "16"))
    stores = [s for s in os.environ.get("STORES", "user_files,template_files,output,history").split(",") if s]
    extras = []
    for pair in os.environ.get("EXTRA", "").split(";"):
        if "=" in pair:
            src, key = pair.split("=", 1)
            extras.append((src.strip(), key.strip()))

    s3 = boto3.client(
        "s3",
        endpoint_url=endpoint,
        aws_access_key_id=os.environ.get("S3_ID") or "x",
        aws_secret_access_key=os.environ.get("S3_KEY") or "x",
        config=botocore.client.Config(s3={"addressing_style": "path"}, signature_version="s3v4"),
        region_name="us-east-1",
    )
    s3.list_buckets()  # fail fast if the endpoint is unreachable
    log(f"endpoint={endpoint} bucket={bucket} stores={stores} reimport={reimport} skip_existing={skip_existing} workers={workers}")

    if reimport:
        removed = 0
        for obj in list(bucket_objects(s3, bucket)):
            s3.delete_object(Bucket=bucket, Key=obj["Key"])
            removed += 1
        log(f"reimport: removed {removed} objects from '{bucket}'")
    else:
        try:
            s3.head_bucket(Bucket=bucket)
        except Exception:
            s3.create_bucket(Bucket=bucket)
            log(f"created bucket '{bucket}'")

    if not os.path.isdir(legacy):
        log(f"ERROR: legacy data root missing: {legacy}")
        return 2

    # ---- collect the full work list (store, src) -------------------------
    jobs = []
    for store in stores:
        root = os.path.join(legacy, store)
        if not os.path.isdir(root):
            log(f"store '{store}': not present in legacy data — skipped")
            continue
        n = 0
        for r, _dirs, files in os.walk(root):
            for f in files:
                jobs.append((store, os.path.join(r, f)))
                n += 1
        log(f"store '{store}': {n} files queued")
    for src, key in extras:
        if os.path.isfile(src):
            jobs.append(("__extra__", src, key))
        else:
            log(f"extra '{src}': missing — skipped")
    stats = Stats()
    stats.total = len(jobs)
    log(f"total jobs: {stats.total}")
    t0 = time.time()
    last = [0.0]

    def work(job):
        if len(job) == 3:  # extra: explicit key
            _store, src, key = job
        else:
            store, src = job
            key = "%s/%s" % (store, os.path.relpath(src, os.path.join(legacy, store)).replace(os.sep, "/"))
        try:
            if skip_existing:
                try:
                    head = s3.head_object(Bucket=bucket, Key=key)
                    etag = (head.get("ETag") or "").strip('"').lower()
                    if head.get("ContentLength") == os.path.getsize(src) and etag == md5file(src):
                        stats.add("skip")
                        return
                except Exception:
                    pass  # not present → upload
            size = os.path.getsize(src)
            s3.upload_file(src, bucket, key)
            stats.add("up", size)
        except Exception as e:
            stats.add("err")
            log(f"  ERROR {key}: {e}")
        now = time.time()
        if now - last[0] > 20:
            last[0] = now
            log(f"progress: {stats.report()}")

    with ThreadPoolExecutor(max_workers=workers) as ex:
        list(ex.map(work, jobs))

    total = 0
    for _o in bucket_objects(s3, bucket):
        total += 1
    log(
        f"done: {stats.report()} in {time.time() - t0:.1f}s | bucket '{bucket}' now holds {total} objects"
    )
    return 1 if stats.err else 0


if __name__ == "__main__":
    sys.exit(main())
