# LanguageTool (grammar checking) in OlliTeX

OlliTeX ships a LanguageTool integration: the editor highlights grammar
suggestions inline (per-user on/off in the IDE, admin-wide switch in the
hub). This page covers the **service** side: the LanguageTool container
and its language models (n-grams).

## The pieces

| Piece | Owner |
|-------|-------|
| `languagetool` container (`lib/docker-compose.languetag… `.yml`) | toolkit |
| N-gram language models (several GB per language) | `bin/languagetool-ngrams` |
| Endpoint URL + enable/disable (run-time, no re-deploy) | **/hub → Admin → Site → Grammar** |
| User preference (inline suggestions on/off) | **/hub → My settings** + IDE settings |

## Enable it (one-time, ~two commands)

```sh
# 1. choose the languages (official set: en de es fr nl), then download
bin/languagetool-ngrams --languages en,de     # ~10 GB for en+de; see sizes below
#    (or edit LANGUAGE_TOOL_NGRAM_LANGUAGES in config/overleaf.rc first)

# 2. switch the service on
sed -i 's/^LANGUAGE_TOOL_ENABLED=false/LANGUAGE_TOOL_ENABLED=true/' config/overleaf.rc

# 3. start (or restart) the stack
bin/up
```

Verify: `docker exec languagetool curl -s localhost:8010/v2/languages`
(or the health indicator in `bin/doctor` / the TUI stack pane), then
/hub → Admin → Site → Grammar — the URL defaults to
`http://languagetool:8010` (override with `LANGUAGETOOL_URL` in
`config/variables.env`), enable it for all users or per user.

## N-gram model files

- Official downloads (dated archives):
  `https://languagetool.org/download/ngram-data/`
- `bin/languagetool-ngrams` saves stable per-language copies under
  `data/languagetool/ngrams/` (extracted `<lang>/` dirs + `ngrams-<lang>.zip`),
  exactly the layout the `erikvl87/languagetool` image expects — the same
  layout the production compose_cep stack runs.
- Approximate zip sizes (2026-09-13): **en ≈ 9 GB · de ≈ 1.7 GB ·
  nl ≈ 1.2 GB · es ≈ 1 GB · fr ≈ 1 GB** — check disk space first.
- To add a language later: rerun the tool with `--languages en,de,fr,nl`;
  already-downloaded languages are skipped.

## Why env + hub both exist

`LANGUAGETOOL_URL` is a bootstrap env (needed before any admin UI can
point elsewhere); everything above it (enable/disable, endpoint override,
per-user preference) is hub-managed and takes effect next boot / instantly
for the toggle — no toolkit edits.
