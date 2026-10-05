#!/usr/bin/env bash
# RETIRED — 2026-10-06 (owner directive): the tools/migrations chain (the
# upstream OSL Node migrations, incl. one-shot destructive ones like
# reset_hardcoded_admin_password) RAN ON EVERY BOOT and could re-execute
# against production databases any time the migrate state collection reset
# or the Mongo was replaced. This fork's web plane is Go (go/services/web)
# and owns its own schema (config store via PG; Mongo indexes created by the
# Go code paths), so the boot-time migration chain is retired.
#
# The stub stays (numbered-init contract: the 900 slot must exist and pass)
# and logs loudly so a boot trace shows the retirement instead of silence.
set -euo pipefail
echo "900: web migrations RETIRED (2026-10-06, owner directive) — tools/migrations is out of the image; the Go web plane owns its schema. Nothing runs."
