#!/usr/bin/env python3
"""AG-era mongo-bootstrap helper (owner 2026-10-09).

Clears the WiredTiger files of the `local` database (the replica-set
bookkeeping: system.replset, oplog.rs, startup_log, ...) from a MONGO data
dir, identified via the _mdb_catalog.wt namespace→file mapping. EVERY
other database's files are left in place (users/projects/docs are in
sharelatex / overleaf / ollitex — never local.*).

Usage: clear-local-wt.py <catalog-file> <data-dir>
Idempotent and best-effort: any parse issue leaves files untouched.
"""
import os
import re
import sys


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: clear-local-wt.py <catalog-file> <data-dir>")
        return 2
    catalog, base = sys.argv[1], sys.argv[2]
    try:
        d = open(catalog, "rb").read()
    except OSError as e:
        print(f"[mongo-bootstrap] cannot read catalog: {e}")
        return 1
    files = set(os.listdir(base))
    removed = 0
    # Catalog entry anatomy (verified against the live catalog):
    #   b'\x02ns\x00' <ns-bytes...> ... 'collection-<uuid36>'
    # where the ns bytes are the namespace ("local.startup_log9n",
    # "local.system.replset", ...) followed (within ~120 bytes) by the
    # collection file's uuid.
    for m in re.finditer(rb"\x02ns\x00", d):
        seg = d[m.start(): m.start() + 140]
        if b"collection-" not in seg:
            continue
        fm = re.search(rb"collection-([a-f0-9-]{36})", seg)
        if not fm:
            continue
        hdr = seg[: seg.rfind(b"collection-")]
        pos = hdr.rfind(b"ns")
        if pos < 0:
            continue
        tail = hdr[pos + 2:]
        cut = tail.find(b"\x00")
        if cut >= 0:
            tail = tail[:cut]
        ns = tail.decode("utf-8", "replace")
        fname = "collection-" + fm.group(1).decode() + ".wt"
        if ns.startswith("local.") and fname in files:
            try:
                os.remove(os.path.join(base, fname))
                removed += 1
                print(f"[mongo-bootstrap] cleared local RS state file: {fname} (ns={ns})")
            except OSError as e:
                print(f"[mongo-bootstrap] could not remove {fname}: {e}")
    print(f"[mongo-bootstrap] local.* WT files cleared: {removed}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
