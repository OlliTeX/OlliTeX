{
  "id": "7b0cc7fc",
  "title": "Audit + retire if safe: dockerfiles/, tools/capture-p3c-raw, tools/capture-p620-raw, tools/service-parity",
  "tags": [
    "retirement",
    "tools",
    "dockerfiles",
    "owner-directive"
  ],
  "status": "done",
  "created_at": 1791239943423,
  "last_change": 1791506969718,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

DONE + pushed (35da2485). Audit verdicts (evidence-based, per-pattern git-mv-to-junk/RETIRED.md):
- dockerfiles/cypress/    KEEP — live build context of build-images/test/docker-compose(.native).yml (e2e cypress image; tests/e2e/stack-up.sh drives it).
- tools/capture-p620-raw  KEEP — oracle captures of the launchpad P620 probes (referenced by go/services/web/views/pages_data_p620.go "Do not edit" + launchpad README).
- tools/capture-p3c-raw   RETIRED → junk/tools-capture-p3c-raw (historical P3C probe HTML/JSON, zero references).
- tools/service-parity    RETIRED → junk/tools-service-parity (node/go env-diff probes; superseded by tests/e2e parity suites; only historical plan docs mention it).
go ./services/web/... builds clean after the moves.
