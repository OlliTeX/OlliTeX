# `llmsettings` — web feature package

The LLM settings surface (P6.4a): services/web/modules/llm — user + admin provider/model/rate/budget settings + usage, incl. the instance-LLM admin card data.

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
