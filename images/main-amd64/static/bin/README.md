# Vendored binaries — provenance

## tex-fmt v0.5.7 — official x86_64-alpine (musl, static-pie) build
Source: https://github.com/WGUNDERWOOD/tex-fmt/releases/tag/v0.5.7
Asset: `tex-fmt-x86_64-alpine.tar.gz` (released 2026-03)
SHA256: 3a92a5d50a464c53f4611ce4e2ef2e07efe58067df5a5df3ed9f9de74d1c62f6
Installed into the web server image at /usr/local/bin/tex-fmt (Dockerfile COPY).
Used by the tex-autoformatter module (POST /api/format-tex) for .tex/.cls/.sty files.

WHY alpine: the previous vendored copy was a glibc-dynamic x86_64 binary that
cannot execute on the Alpine (musl) stack (`/lib64/ld-linux-x86-64.so.2`
missing → spawn ENOENT → every /api/format-tex 500'd). The official alpine
asset is static-pie musl and also runs on glibc hosts, so it is the portable
choice regardless of which base image builds from this Dockerfile.

## texcount (TeX Live archive `texcount.tar.xz`, r79618 era = 1.99)
Source: https://mirror.ctan.org/systems/texlive/tlnet/archive/texcount.tar.xz
Files: `texcount.pl` (the perl script) + `texcount` (sh wrapper that execs
it co-located, so PATH invocations work).
SHA256:
  texcount     bac995079e6547036c5ac8862318c23878bf0670dabf48b26adadf59c365e7
  texcount.pl  563049292e565808e78df75703a966c0064b9a956cb07edb65236eee0007fb90
Installed at /usr/bin/texcount + /usr/bin/texcount.pl (Dockerfile COPY).
Used by CLSI for the editor word-count feature (GET /projects/:id/wordcount
runs `texcount -nocol -inc <file>`). texcount.pl only needs perl core modules
(Encode, Text::Wrap, Term::ANSIColor), so it runs on the Alpine perl without
a TeX Live install.
