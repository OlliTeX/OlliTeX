# `sitesettings` — web feature package

The admin "Manage Site" SiteSettings leaf (P3.6 flip unit): the five admin-tools site-settings routes incl. the CTR/HKDF cipher (sitesettings/cipher.go).

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
