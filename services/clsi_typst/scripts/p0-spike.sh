#!/usr/bin/env bash
# P0 spike (TYPST_PHASES.md F0.1): compile examples/hello.typ inside
# pandoc/typst:3-alpine using the EXACT container options clsi_typst will ship with
# (Entrypoint cleared, uid 33:33, network none, CapDrop ALL, SecurityOpt +
# seccomp when available). Verifies the PDF magic and the typst version line that
# clsi_typst's TypstRunner writes into output.log.
set -euo pipefail

HERE="$(cd "$(dirname "$0")/.." && pwd)"
IMAGE="${TYPST_DOCKER_IMAGE:-pandoc/typst:3-alpine}"
WORKDIR="${1:-/tmp/typst_spike}"
mkdir -p "$WORKDIR"
cat "$HERE/examples/hello.typ" > "$WORKDIR/main.typ"   # clsi_typst ResourceWriter analogue (P1 wires the real one)
# uid 33 (container user) must be able to create output.log/output.pdf here;
# real clsi runs its compile dirs owned by www-data — the P1 DockerRunner
# replicates that. For the spike: best-effort chown, else world-writable.
chown -R 33:33 "$WORKDIR" 2>/dev/null || chmod 0777 "$WORKDIR"

# NOTE: seccomp is verified separately in verify-seccomp.sh (docker run can't take
# --seccomp; clsi_typst sets it via dockerode `create` at P1). P0 spike proves the
# compile contract (uid/entrypoint/network/capdrop/env + PDF + version line).

docker run --rm \
  --entrypoint "" \
  --user 33:33 \
  --network none \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --memory 1g \
  -v "${WORKDIR}:/compile" \
  -w /compile \
  -e HOME=/tmp \
  -e TYPST_PACKAGE_CACHE_PATH=/tmp/.cache/typst \
  "$IMAGE" \
  sh -c 'echo "typst $(typst --version 2>/dev/null || echo unknown)" > "$3"; typst compile "$1" "$2" >> "$3" 2>&1; exit 0' -- \
  main.typ output.pdf output.log

echo "--- output.log ---"
cat "$WORKDIR/output.log"
echo "--- assertions ---"
head -c 5 "$WORKDIR/output.pdf" | grep -q '%PDF' || { echo "FAIL: output.pdf is not a PDF"; exit 1; }
grep -q '^typst ' "$WORKDIR/output.log" || { echo "FAIL: missing typst version line"; exit 1; }
echo "OK: PDF produced ($(wc -c < "$WORKDIR/output.pdf") bytes), typst version line present in output.log"
