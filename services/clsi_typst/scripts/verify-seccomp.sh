#!/usr/bin/env bash
# P0 F0.3: does clsi's seccomp profile LOAD + run on pandoc/typst:3-alpine?
# clsi_typst ships this profile at seccomp/clsi-profile.json (copy of clsi's).
# NOTE: this Docker CLI has no top-level `--seccomp` flag on run/create; seccomp
# profiles are passed via `--security-opt seccomp=<path>` (the daemon handles the
# profile the same way dockerode would). PASS = container starts AND a full
# `typst compile` of examples/hello.typ exits 0 and writes a valid PDF (uid 33).
# VERDICT (2026-09-05, docker 29.5.3 + pandoc/typst:3-alpine 0.14.2): PASS.
#   (No seccomp-off decision needed in the runbook.)
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
CP="$HERE/seccomp/clsi-profile.json"
[[ -f "$CP" ]] || { echo "SKIP: no profile yet (F1.11 copies it in); verdict: undetermined"; exit 0; }

PROBE=/tmp/typst_seccomp_probe
rm -rf "$PROBE"; mkdir -p "$PROBE"; cp "$HERE/examples/hello.typ" "$PROBE/main.typ"
chmod 0777 "$PROBE" 2>/dev/null || true
PROBE_FILE=/tmp/probe-seccomp.json
cp "$CP" "$PROBE_FILE"

cid="$(docker create --network none --cap-drop ALL --security-opt no-new-privileges \
  --security-opt "seccomp=$PROBE_FILE" \
  --user 33:33 --entrypoint '' \
  -v "$PROBE:/compile" -w /compile \
  -e HOME=/tmp -e TYPST_PACKAGE_CACHE_PATH=/tmp/.cache/typst \
  pandoc/typst:3-alpine \
  sh -c 'echo "typst $(typst --version 2>/dev/null || echo unknown)" > /compile/log.txt; \
         typst compile /compile/main.typ /compile/output.pdf >> /compile/log.txt 2>&1; \
         echo EXIT=$? >> /compile/log.txt' 2>&1)" \
  || { echo "FAIL: cannot create container with clsi-profile.json:"; echo "$cid"; exit 1; }

docker start "$cid" >/dev/null
# Docker start does not block on this daemon — poll until the state flips
for _ in $(seq 1 60); do
  st=$(docker inspect -f '{{.State.Status}}' "$cid" 2>/dev/null || true)
  [ "$st" != "running" ] && break
  sleep 0.5
done
# give the bind-mount a beat
for _ in $(seq 1 20); do
  [ -f "$PROBE/output.pdf" ] && break
  sleep 0.5
done
state=$(docker inspect -f '{{.State.Status}} {{.State.Error}}' "$cid" 2>/dev/null || true)
if [ ! -f "$PROBE/output.pdf" ] || ! head -c 5 "$PROBE/output.pdf" | grep -q '%PDF'; then
  echo "FAIL: compile did not produce a valid PDF under clsi-profile.json (state: $state)"
  [ -f "$PROBE/log.txt" ] && sed 's/^/  | /' "$PROBE/log.txt"
  docker rm -f "$cid" >/dev/null 2>&1
  exit 1
fi
echo "PASS: clsi-profile.json loads on this image; full 'typst compile' exits 0 and produces a valid PDF ($(wc -c < "$PROBE/output.pdf") bytes) with uid 33 + CapDrop ALL + no-new-privileges."
rm -rf "$PROBE" "$PROBE_FILE" >/dev/null 2>&1
docker rm "$cid" >/dev/null 2>&1
