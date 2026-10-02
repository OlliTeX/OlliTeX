# clsi-typst local / live E2E harness

`e2e.mjs` proves the **crown jewel — Synctex for Typst** — end-to-end against a
**running** `clsitypst` (typst clsi) service and a **real docker compile** with the
**patched** `ollitex/typst` image. The same script works **locally** and on the **live**
stack, so it doubles as the owner's M1 live-E2E check.

## What it asserts (4 signals)

| Signal | Endpoint | PASS means |
|---|---|---|
| compile | `POST /project/{p}/user/{u}/compile` | `status` = `success`/`complete` **and** `outputFiles` has `output.pdf` **(and `output.sourcemap.json` for typst)** |
| click-to-source | `GET .../sync/code?file&line&column&buildId` | returns a PDF box `{page,h,v,width,height}` (matches the frontend `HighlightData`) |
| click-to-PDF | `GET .../sync/pdf?page&h&v&buildId` | returns `code[0] = {file, line}` (an in-range source line) |
| wordcount | `GET .../wordcount?file=main.typ` | `texcount.textWords > 0` |

A unique project id is derived per run so it never reuses stale `typst-project-*`
container state.

## Config (env)

| Var | Default | Purpose |
|---|---|---|
| `CTY_BASE_URL` | `http://127.0.0.1:3014` | The service base URL (local, or the live `:3014`) |
| `CTY_COMPILER` | `typst` | `typst` (default; sidecar asserted) or other |
| `CTY_DOC` | built-in | Path to the `.typ` root file to compile |
| `CTY_LINE` | `5` | The 1-based source line of the unique phrase to locate |
| `CTY_TIMEOUT_MS` | `300000` | Per-request timeout |

**Exit code:** `0` iff ALL 4 signals pass, else `1`. Safe to use as a gate.

## Run locally (bring up the service first)

The harness does **not** start the service or touch docker — the service does. Bring
up `clsitypst` on `127.0.0.1:3014` with the **patched** image, then:

```bash
node tests/tools/clsi-typst/e2e.mjs
```

## Run LIVE (M1, psintern — owner-gated)

```bash
CTY_BASE_URL=http://<psintern>:3014 node tests/tools/clsi-typst/e2e.mjs
```

This is the "live typst compile + synctex + wordcount" half of M1's E2E. Pair it with a
manual TeX compile (no-regression) in the browser.

## Note on the image (crown jewel)

Synctex **requires** the patched `ollitex/typst` image (emits `output.sourcemap.json`).
If `TYPST_IMAGE` is the vanilla `pandoc/typst:latest-alpine`, compile still succeeds but
the **sidecar + both synctex signals FAIL** — that is the expected, correct behavior, and
the harness reports it clearly. For the live stack, run `make build-typst` (which tags the **canonical `ollitex/typst:main`**) and set
`TYPST_IMAGE=ollitex/typst:main` at deploy (see `images/main-amd64/runit/clsi_typst-overleaf/run`).
