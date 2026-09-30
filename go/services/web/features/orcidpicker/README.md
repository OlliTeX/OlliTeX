# `orcidpicker` — web feature package

The ORCID picker surface (P6.7): OrcidPickerRouter + OrcidService — the ORCID OAuth/link routes.

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).

## SSRF guard (outbound fetch — `ssrf.go` + `client.go`)

`safefetch` resolves each hop with `checkHostNotPrivate` (reject if ANY DNS
record is non-public) and re-checks on every manual redirect hop (≤ 5 hops,
10 s hop timeout) — matching the Node `safefetch` behavior.

**Accepted risk (audit part B B3):** this is a check-then-use guard using the
system resolver, not a pinned dialer with a post-dial IP re-check. An
attacker-controlled nameserver can rebind a name between the lookup (decision)
and the actual dial — the classic DNS-rebinding TOCTOU. Mitigations in this
port: per-hop re-check (tiny rebind window), fixed hop timeout, and the same
residual window the Node oracle has (parity). A stronger mitigation
(`resolver + dialcontext` with a pinned IP and dialer IP re-check) is
deliberately not implemented to avoid diverging from the Node oracle.
