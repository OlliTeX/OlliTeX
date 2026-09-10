# clsi_typst

Typst compile service for the Overleaf fork. `clsi_typst` is a
**clsi-lookalike** HTTP service: it speaks the same compile contract as
`services/clsi` (same routes, same response envelope, same docker sandboxing
model) so that the rest of the stack (web ClsiManager → ClsiURLHelpers → lb)
can route `.typ` projects to it without clsi changes.

Plan: [`TYPST_INTEGRATION_PLAN.md`](../../TYPST_INTEGRATION_PLAN.md) §3 — this
service implements §3.1 (layout), §3.3 (routes), §3.4 (compile flow) and
§3.5 (TypstRunner).

## Differences from clsi

| | clsi | clsi_typst |
|---|---|---|
| compiler | pdflatex / xelatex / lualatex (latexmk) | typst (`typst compile`) |
| image | quay.io/sharelatex/texlive-* | `pandoc/typst:3-alpine` |
| container user | `www-data` | `33` (numeric — no www-data in the image) |
| synctex | yes (`/sync/code`, `/sync/pdf`) | **no** (no Typst equivalent, §7.5) |
| output | output.pdf + .log + … | output.pdf + output.log |
| feature gate | — | `COMPILE_TYPEST_ENABLED` (default off → 501) |

`app.js`, `CompileController`, `CompileManager`, `RequestParser` are the
service-local thin wrappers (compiler difference). All compile plumbing
(LockManager, OutputCacheManager, ResourceWriter, DockerRunner…) lives in
clsi (P0-approved direct import; the `libraries/clsi-core` extraction is the
P1b follow-up — see plan §3.2).

Two files are intentionally copies of clsi files (kept in sync manually):

- `app/js/DockerRunner.mjs` — clsi minus the synctex/latexmk branches, plus
  `Entrypoint: []` (the pandoc/typst image has pandoc's own entrypoint;
  verified `F0.1`) and `typst-project-…` container naming.
- `app/js/TypstRunner.js` — the single compiler-specific module (mirrors
  clsi `LatexRunner`).

## Local run

```sh
yarn install            # at the monorepo root
cd services/clsi_typst

# required env (mirrors clsi's docker runner mode)
export SANDBOXED_COMPILES=true
export TYPST_DOCKER_IMAGE=pandoc/typst:3-alpine
export COMPILE_TYPEST_ENABLED=true
export CLSI_TYPST_PORT=3014
export CLSI_TYPST_COMPILES_PATH=$PWD/compiles
export CLSI_TYPST_OUTPUT_PATH=$PWD/output
export CLSI_TYPST_CACHE_PATH=$PWD/cache

# seccomp profile (clsi's, verified against this image in P0)
docker info --format '{{.SecurityOptions}}'   # confirm seccomp is available

yarn start
```

Acceptance (docker only, no redis/mongo needed for compile):

```sh
# env — sandbox mode (the docker socket runs the compile containers)
export SANDBOXED_COMPILES=true
export TYPST_DOCKER_IMAGE=pandoc/typst:3-alpine
export COMPILE_TYPEST_ENABLED=true
export CLSI_TYPST_PORT=3014

# app-side compile/output/cache dirs (app fs)
export CLSI_TYPST_COMPILES_PATH=$PWD/compiles
export CLSI_TYPST_OUTPUT_PATH=$PWD/output
export CLSI_TYPST_CACHE_PATH=$PWD/cache

# host dirs the compile containers bind-mount (uid 33 writes into these)
export SANDBOXED_COMPILES_HOST_DIR_COMPILES=$PWD/compiles
export SANDBOXED_COMPILES_HOST_DIR_OUTPUT=$PWD/output
export SANDBOXED_COMPILES_HOST_DIR_CACHE=$PWD/cache

# image allowlist (clsi parity: ALLOWED_IMAGES — must include the default)
export ALLOWED_IMAGES=$TYPST_DOCKER_IMAGE

# hard-fail gate: a .typ compile must produce an output.pdf
yarn test:acceptance
```

Unit (no docker needed):

```sh
yarn test:unit
```

Tests: `test/acceptance/js/TypstBasicCompileTests.js` mirrors clsi's
acceptance setup (`ClsiApp`/`Client` helpers) — see
`test/acceptance/js/helpers/`.

## HTTP surface

| Method | Path | Notes |
|---|---|---|
| POST | `/project/:pid/compile` | clsi compile contract |
| POST | `/project/:pid/compile/stop` | |
| DELETE | `/project/:pid` | clears compile dir + cache |
| GET | `/project/:pid/wordcount?file=main.typ` | `{ texcount: { … } }` |
| GET/POST | `/project/:pid/status` | `OK` |
| GET | `/project/:pid/build/:build_id/output/output.zip` | |
| … | `…/user/:uid/…` | per-user variants of the above |
| `/status` | | liveness (`clsi_typst is alive`) |

No `sync/*` routes. Every route returns **501** while
`COMPILE_TYPEST_ENABLED` is not `true`.
