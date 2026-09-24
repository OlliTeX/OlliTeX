# `go/services/web/views` — the baked-HTML view layer

How the Go web renders pages byte-identically to Node: the static HTML that
Node's pug templates emit is **captured once from the live Node service**,
saved as Go string constants with per-request values replaced by slot
markers, and rendered per request by swapping the slots back in (the
`webviews-gen-*.py` generators in `tools/` produce and verify the captures —
reverse-render against the raw capture is part of the gate evidence).

| file | what |
| --- | --- |
| `pageslots.go` | the shared slot model for React page shells (editor P5.1a, hub P6.1): static `…_template.go` + per-request slot values |
| `editor.go` / `editor_template.go` | the editor page (P5.1a): `GET /editor/:id` + legacy `/Project/:id`; template captured from the Node oracle |
| `detached_template.go` | the detached ide-react view (P5.1b) |
| `hub_template.go` | the `/hub` shell (P6.1) |
| `pages.go` | `PageData` + the non-React page renderers (login/register/reset/500/404/403-restricted, the launchpad `LaunchpadAdminPage`/`LaunchpadFreshPage`, …) — each pins its CSP preset (see `core/headers.go`) and slot finalization |
| `pages_data.go` | the byte-exact static skeletons (CSRF/NONCE slots) for the classic pages |
| `pages_data_p2.go` / `pages_data_p3c.go` / `pages_data_p620.go` / `pages_data_p65.go` | additional captured page families (P2 auth pages, P3c, P6.20 launchpad fresh/admin, P6.5) |
| `pages_invite.go` / `pages_invite_data.go` | the P4.10b invitation surface (invite show / not-valid / generic 403) |

Conventions:
- Slot markers are control-byte pairs built on `\x01` (written in the Go
  sources as `\x01NAME…`/`\u0001NAME…` escapes, closing byte per family — the
  `slot*` consts in `pages.go` are the source of truth) and never occur in
  the captured HTML; finalizers in `pages.go`/`pageslots.go` own the
  per-family slot set (CSRF, NONCE, and feature-specific ones like
  `LPUID`/`LPADM`/`CANMGTPL`).
- The renderers set the same headers the Node page emitted (CSP `cspReact`
  for React shells; the restrictive baseline for 302/401/403 paths), and weak
  ETags (`core.EtagWeakBody`) where Node sets them.
- When the Node view changes: re-capture with the matching generator, re-verify
  the reverse-render, and re-run the parity gate.
