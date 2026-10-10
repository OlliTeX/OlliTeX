# TeX Live 2026 sandbox images (installation)

Since the 2026-10-10 owner directive, **TeX Live 2026 is the default
sandboxed-compile image** for OlliTeX (build image, test stack, and the
instance default).

## The default

| Image | Role |
| --- | --- |
| **`olpsint/texlive-full:2026.1`** | Default compile sandbox — full TeX Live 2026 (built from the ayaka-notes `texlive-full` recipe, 2026 release). Used by `clsitex` unless `TEXLIVE_IMAGE` overrides it. |
| `olpsint/texlive-base:2026` (base-2026) | The ayaka-notes base the full image is layered on. |
| `olpsint/tlnet-cache:2026` | TeX Live network (package) cache image used at build time — not a runtime sandbox. |

The instance default is set in the site-setting seed
(`olpsint/texlive-full:2026.1`, "TeXLive 2026") and the config fallback; an
explicit `TEXLIVE_IMAGE` env var or site-setting value wins over the default.

## TeX Live 2026 vs older releases

- 2026 is a full release-line change, not a point update: package set and
  kernel versions differ from the 2024/2025 lines. Documents that compiled
  under 2024 still compile under 2026 for the classic formats; expect the
  usual release-drift edge cases (macro packages renamed/updated) and verify
  unusual class/files combinations after a TeX Live line change.
- The image remains a **sandboxed compile container**: same isolation rules
  as before (see [04-security-sandbox.md](04-security-sandbox.md)), and the
  seccomp profile has been audited against the 2026 line (the live profile
  includes `faccessat2`, which the 2026 toolchain needs — 179 syscalls).

## Building / rebuilding the 2026 full image

The full-image Dockerfile lives at
`images/texlive-full-amd64/texlive/2026/Dockerfile` and **requires the
version directory itself as the build context** (it references sibling
`Base/` and `TlnetCache/` recipes inside that tree — unlike the other image
Dockerfiles, which build from the repo root):

```
cd images/texlive-full-amd64/texlive/2026
docker build -t olpsint/texlive-full:2026.1 .
```

Layers come from the ayaka-notes recipe images
(`ghcr.io/ayaka-notes/texlive-full:base-2026`,
`ghcr.io/ayaka-notes/tlnet-cache:2026`).

### Credit

The 2026 line is built from **ayaka-notes' TeX Live 2026 images**
(`texlive-full`, base + tlnet cache) — the versioned recipe tree under
`images/texlive-full-amd64/` (LICENSE and README kept at the tree root).
Attribution is retained in `CREDITS.md`.

## Related sandbox images (compile pipeline)

| Image | Role |
| --- | --- |
| `olpsint/ollitex-builder:*` | Compile-orchestration sidecar (Go). Built with `--build-arg GO_BUILDER_TAG=golang:1.27.1-bookworm` (glibc base for the Go runtime). |
| `olpsint/png2pdf:*` | Raster→PDF post-processor (viewer images). |

## Verification gate after a TeX Live line change

1. Compile a fixture that exercises the changed packages.
2. Run the e2e compile spec (Typst + LaTeX matrix).
3. Confirm the seccomp profile still permits the toolchain's syscalls
   (a profile regression shows as sandboxed compiles failing on an
   `ENOSYS`-class path).

## Verified against

OlliTeX v26 surface (2026-10-11): default `olpsint/texlive-full:2026.1`
live on the dev instance; build-tree layout `texlive/2026` + `Base/` +
`TlnetCache/`; seccomp profile source
`go/services/clsitex/config/seccomp/clsi-profile.json` (embedded, 179
syscalls including `faccessat2`).
