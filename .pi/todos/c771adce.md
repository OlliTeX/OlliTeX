{
  "id": "c771adce",
  "title": "013: Download dialog missing pandoc options (Word/MD)",
  "tags": [
    "live-audit",
    "psintern",
    "editor",
    "download",
    "pandoc"
  ],
  "status": "completed",
  "created_at": 1790773203437,
  "last_change": 1790856346315,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## 013 — Download dialog missing pandoc options (Word/MD) — DONE (live E2E verified 2026-10-01)

Commit `502857cc15`, pushed to main.

### What was missing
The Go web plane had no `/download/conversion` routes, the meta gates were off, and the CLSI had no way to serve the staged conversion artifact.

### Implemented
- **Web Go** (`go/services/web/features/compile/exportconv.go`, new):
  - `GET /project/:pid/download/conversion/{docx|markdown|html}[?responseFormat=json[&rootResourcePath=…]]`
    → CLSI `POST /project/:pid/user/:uid/download/project-to-document` (resources built with the
    same pinned helpers as compile.go; buildId generated web-side like Node's
    OutputCacheManager.generateBuildId `{hex-hex}-{hex16}`).
  - `GET /project/:pid/download/conversion/{conversionId}/{type}/build/{buildId}/output/{file}`
    → streams the staged artifact back with Node's attachment name
    (`{safeProjectName}.{docx|zip}`) + correct Content-Type + nosniff.
  - Routes registered in compile `Feature()` (Node ProjectDownloadsController 1:1).
- **Web Go** (`sitesettings/pandoc.go`, new): `PandocConversionsEnabled(a, ctx)` —
  env `ENABLE_PANDOC_CONVERSIONS` + site_settings `pandoc.enabled`
  (live config already had `pandoc.enabled: true`).
Feeds `ol-ExposedSettings.enablePandocConversions` (editorpages + hub) and flips
`export-docx/export-markdown/export-html` split-test flags to "enabled"
(frontend requires exactly "enabled" via isSplitTestEnabled).
- **CLSI Go** (`conversioncontroller.GetOutputFile`, new + apps/routes.go): serves
`OutputDir/{project}/generated-files/{build}/{file}` with a traversal guard.
  Registered under `/download/build/{build_id}/output/{filepath...}` — Go's ServeMux
  would let the existing literal `output/output.zip` compile routes shadow a wildcard
  (markdown/html were being served by the wrong handler — found by live E2E).
- **CLSI Go** (`requestparser/buildid_format_test.go`, new): pins the one-dash
  buildId contract (`^[0-9a-f]+-[0-9a-f]+$`; a UUIDv4 is rejected).
- **runit** (`images/main-amd64/runit/clsi-overleaf/run`): exports
  `ENABLE_PANDOC_CONVERSIONS=true` (baked; also applied live).
- Live: `docker tag ollitex/pandoc:main ollitex/pandoc:latest` (config default is
  the `:latest` tag; only `main` existed).

### Live E2E evidence (psintern, frog project `6abd00e37a29eb9a30056209`)
- `ol-ExposedSettings.enablePandocConversions: true`; all three export flags "enabled".
- File → Download menu shows: Export as Word document (.docx) / (.md) / (.html)
  (alongside Download as PDF + source (.zip)).
- Click "Export as Word document (.docx)" in the live UI → browser download
  `Test_2.docx`, 110,905 B, PK zip magic (valid OOXML container).
- API: create docx/markdown/html → 200 + downloadUrl; downloads:
  docx 110,906 B (`application/vnd.openxmlformats-officedocument.wordprocessingml.document`,
  attachment `Test_2.docx`); markdown zip 98,573 B + html zip 100,735 B
  (both `application/zip`, attachment `Test_2.zip`).
- Content verified: markdown zip contains `main.md` (correct prose of the frog
  project) + `frog.jpg`; html zip contains `main.html`.
- Test suite: compile/editorpages/sitesettings/hub/conversioncontroller/apps/
  requestparser/core all green; gofmt clean; go vet clean; static binaries hot-swapped
  (web + clsitex); services up (web-overleaf / clsi-overleaf).

