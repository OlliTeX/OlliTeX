# Python runner (admin)

In-browser Python for `.py` project files. OlliTeX bundles **Pyodide**
(CPython 3.12, Pyodide 0.29.3) inside the web image — execution happens in a
browser worker on the **user's machine**, completely offline (all packages
resolve from the vendored `js/libs/pyodide` directory; nothing is fetched
from the network at runtime).

## Page

`/admin-settings/site.compilation.pythonrunner`
(hub: **Site settings → Compilation → Python runner**).

## What the admin controls

| Setting | Meaning |
| --- | --- |
| **Enabled** | Turns the `.py` runner surface on/off for the instance. |
| **Interpreter** | Read-only: pinned in the image at build time (Pyodide 0.29.3 / Python 3.12). Upgrading means rebuilding the OlliTeX web image (the bundle carries the runtime) — there is no runtime download. |
| **Allowed package policy** | The comma-separated list of Pyodide packages users may import (e.g. `numpy, pandas, matplotlib`). Packages resolve **only** against the vendored directory, so this list is a policy gate on what the bundle exposes — not a permission to fetch new packages. |

## Security model

- **No server-side Python execution.** The runner executes in the user's
  browser (WebAssembly CPython). A malicious `.py` file can only affect the
  user's own browser worker — not the instance, not other users.
- **No network at runtime.** The worker is offline; `import` succeeds only
  for vendored packages.
- The admin page exposes **no credentials and no secrets** — the only
  values are the enable flag and the package allow-list (both visible to
  users as their effective settings anyway).

## User experience

Users with an enabled runner open a `.py` file and get the embedded Python
editor surface (edit, run, console output) inside the project. Which
packages work depends on the admin's allow-list; unknown imports fail in the
console with the usual CPython message.

## Verified against

OlliTeX v26 surface (2026-10-11): admin section
`site.compilation.pythonrunner`; Pyodide 0.29.3 / Python 3.12 vendored in
the web image; offline-only package resolution; browser-worker execution.
