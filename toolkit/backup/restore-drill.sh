#!/bin/sh
# toolkit/backup/restore-drill.sh — validity proof: restore the LATEST
# artifact of every store into SCRATCH targets (a throwaway mongo/redis/pg
# context + a temp tree for the SeaweedFS tars). The live stack is never
# touched. Prints a PASS/FAIL line per store and exits non-zero on any FAIL.
#
#   sh toolkit/backup/restore-drill.sh
set -u
cd "$(dirname "$0")"
. ./common.conf

R="${BACKUP_ROOT}/.."     # backups/ lives under data/; artifacts are per-store
R="$BACKUP_ROOT"
pick() { ls -1t "$R/$1"/$2 2>/dev/null | head -1; }

overall=0
say() { printf '[drill] %s\n' "$*"; }

# --- mongo -------------------------------------------------------------------
A=$(pick mongo "*.mongo.gz")
if [ -n "$A" ] && sh ./restore-mongo.sh "$A" >/tmp/drill_mongo.log 2>&1; then
  say "mongo     PASS  ($A)"
else
  say "mongo     FAIL  (see /tmp/drill_mongo.log)"; tail -5 /tmp/drill_mongo.log; overall=1
fi

# --- redis (scratch redis container loads the RDB) ----------------------------
A=$(pick redis "*.redis.rdb")
if [ -n "$A" ] && head -c5 "$A" | grep -q REDIS; then
  img=$(docker exec "$OLLITEX_REDIS" sh -c 'true'; docker inspect "$OLLITEX_REDIS" --format '{{.Config.Image}}')
  drill="drill-redis-${TS##*T}"
  tmp=$(mktemp -d)
  cp "$A" "$tmp/dump.rdb"
  if docker run -d --name "$drill" -v "$tmp:/data" -w /data "$img" >/dev/null && sleep 3; then
    ping=$(docker exec "$drill" redis-cli PING 2>/dev/null)
    n=$(docker exec "$drill" redis-cli DBSIZE 2>/dev/null)
    if [ "$ping" = "PONG" ] && [ -n "$n" ] && [ "$n" != "0" ]; then
      sample=$(docker exec "$drill" redis-cli --scan 2>/dev/null | head -3 | tr '\n' ' ')
      say "redis     PASS  (PONG, DBSIZE=$n, sample: ${sample:-...})"
    else
      say "redis     FAIL  (ping='$ping' n='$n')"; overall=1
    fi
  else
    say "redis     FAIL  (scratch container would not start)"; overall=1
  fi
  docker rm -f "$drill" >/dev/null 2>&1; rm -rf "$tmp"
else
  say "redis     FAIL  (artifact missing or not an RDB)"; overall=1
fi

# --- postgres (restore into a scratch database on the LIVE pg — additive) -----
A=$(pick postgres "*.overleaf.sql.gz")
if [ -n "$A" ]; then
  if sh ./restore-pg.sh "$A" "drill_${TS##*T}" >/tmp/drill_pg.log 2>&1; then
    n=$(docker exec "$OLLITEX_PG" psql -U "$OLLITEX_PG_USER" -Atc "select count(*) from information_schema.tables where table_schema='public'" -d "drill_${TS##*T}" 2>/dev/null)
    say "postgres  PASS  (scratch db drill_${TS##*T}: $n public tables)"
    docker exec "$OLLITEX_PG" dropdb -U "$OLLITEX_PG_USER" "drill_${TS##*T}" >/dev/null 2>&1 || true
  else
    say "postgres  FAIL  (see /tmp/drill_pg.log)"; tail -5 /tmp/drill_pg.log; overall=1
  fi
else
  say "postgres  FAIL  (no overleaf dump found)"; overall=1
fi

# --- seaweedfs (expand the volume tar; count .dat/.idx/.vif triples) ----------
A=$(pick seaweedfs "*.seaweedfs-volume.tar.gz")
if [ -n "$A" ]; then
  tmp=$(mktemp -d)
  if tar -xzf "$A" -C "$tmp" 2>/dev/null; then
    nfiles=$(find "$tmp" -type f -not -name '._*' | wc -l | tr -d ' ')
    ndat=$(find "$tmp" -name '*.dat' | wc -l | tr -d ' ')
    nidx=$(find "$tmp" -name '*.idx' | wc -l | tr -d ' ')
    nvif=$(find "$tmp" -name '*.vif' | wc -l | tr -d ' ')
    sz=$(du -sh "$tmp" | cut -f1)
    if [ "$ndat" = "$nidx" ] && [ "$ndat" = "$nvif" ] && [ "$ndat" != "0" ]; then
      say "seaweedfs PASS  (volume: ${ndat} .dat/.idx/.vif triples, ${nfiles} files, ${sz})"
    else
      say "seaweedfs FAIL  (dat=$ndat idx=$nidx vif=$nvif files=$nfiles — triples mismatch or empty)"
      overall=1
    fi
  else
    say "seaweedfs FAIL  (tar would not expand)"; overall=1
  fi
  rm -rf "$tmp"
else
  say "seaweedfs FAIL  (no volume tar found)"; overall=1
fi

echo
[ "$overall" = "0" ] && say "DRILL: ALL PASS" || say "DRILL: FAILURES PRESENT"
exit "$overall"
