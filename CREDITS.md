# CREDITS

Third-party work the OlliTeX monorepo borrows, per owner policy ("if we take
something from their code we put them into CREDITS.md").

- **Docker Autoheal** — `github.com/willfarrell/autoheal` (Nikos via willfarrell;
  Apache-2.0 lineage). We borrowed the *operating model* (not a copy): poll the
  daemon for `health=unhealthy` containers → restart with a per-container stop
  timeout (`autoheal.stop.timeout` label), skip null-named + already-restarting
  containers, and fire a post-restart notification hook. Implemented natively in
  Go in `go/services/toolkit/docker.go` (`Healer`), with one added safety layer
  autoheal does not have: a per-container cooldown window to break tight
  restart loops on top of docker's own restart policy.

- **charmbracelet** — `wish` (SSH server), `log` (structured logging).
  Used by the operator TUI service in `go/services/toolkit`.

- **rivo — the retained-mode TUI stack (MIT/Apache-2.0 dual, credited per the
  policy)** — the operator TUI in `go/services/toolkit` was ported to this
  stack on 2026-10-07 (owner item AI): `rivo/tview` provides the retained-mode
  widget tree (Application/Grid/List/TextView/Modal/InputField/Button +
  QueueUpdateDraw), and `gdamore/tcell` (v2) the terminal engine (terminfo
  screens, events, styles — the session-screen path runs the TUI over the
  SSH pty through a `tcell.Tty` bound to the wish session's io in
  `tui.go`). The classic console look (menu strip + two panes + status/
  keystrip footer + centered confirm/prompt dialogs + the j/k/number key map)
  is reproduced with the public tview API (no code copied).
  - **rivo/tview** (https://github.com/rivo/tview) — retained-mode TUI
    widgets + event loop.
  - **gdamore/tcell** (https://github.com/gdamore/tcell) — the terminal
    screen/event layer under tview.
  - **rivo/uniseg** (https://github.com/rivo/uniseg) — Unicode segmentation
    used by tview/tcell text measurement.

- **retired 2026-10-07 (the bubbletea era)** — the previous TUI runtime
  (`charmbracelet/bubbletea` + `lipgloss` + `bubbles`, with the three helper
  libraries `jejacks0n/bubbletea-menubar`, `rmhubbert/bubbletea-overlay`,
  `lrstanley/bubblezone`) is no longer linked by this codebase; the port
  replaced its full-frame redraw (the "ultra slow over SSH" wall) with the
  retained-mode tview screen. The credits above are kept for provenance of
  the classic layout the tview port preserves.
  - jejacks0n/bubbletea-menubar (https://github.com/jejacks0n/bubbletea-menubar)
  - rmhubbert/bubbletea-overlay (https://github.com/rmhubbert/bubbletea-overlay)
  - lrstanley/bubblezone (https://github.com/lrstanley/bubblezone)

- **Moby (Docker)** — `github.com/moby/moby/client` (official Go client) for all
  daemon operations (container list/logs/images, exec attach for interactive
  shells, restart). The `stdcopy` stream demultiplexing in
  `go/services/toolkit/shell.go` transcribes moby's documented frame format
  (8-byte header: stream + 3 pad + BE32 size).

- **SeaweedFS** — `chrislusf/seaweedfs` image, used as the local S3 object
  storage backend for filestore + docstore.

- **LanguageTool** — `erikvl87/languagetool` image plus the owner-maintained
  ngram/languagemodel packs mounted read-only at `/ngrams`.

- **Prometheus ecosystem (D22 observability)** — `prom/prometheus`
  (v2.53.5), `grafana/grafana-oss` (11.6.0), `prom/node-exporter` (v1.9.1),
  `percona/mongodb_exporter` (0.43.0), `oliver006/redis_exporter`
  (v1.58.0-alpine) —
  pinned: prometheus/grafana/node-exporter match `server-ce/docker-compose.yml`
  d22 pins; the exporters use the current upstream tags (the prom
  mongodb-exporter mirror was retired upstream). Used by (a) the opt-in d22
  profile of the main server compose, (b) the toolkit's opt-in monitoring
  overlay
  (`toolkit/lib/docker-compose.monitoring.yml`) and (c) the hub instance
  stats collector (which consumes no third-party code — it reads local
  metrics only).
- **Grafana dashboard borrowing** — `toolkit/lib/monitoring/grafana/
  dashboards/ollitex-overview.json` is a copy of
  `server-ce/grafana/dashboards/ollitex-overview.json` (same repo, same
  owner — listed here for provenance per the credits policy), as are the
  provisioning files `datasources/prometheus.yml` +
  `dashboards/ollitex.yml`.
- **ayaka-notes — texlive-full (the TeXLive 2026 default sandbox image)** —
  `https://github.com/ayaka-notes/texlive-full` (MIT, per its LICENSE). Vendor
  dir: `images/texlive-full-amd64/` (owner-added 2026-10-10), built by the
  root Makefile targets `build-texlive-base` + `build-texlive` →
  `olpsint/texlive-full:2026.1`, now the project's **default** texlive
  sandbox compile image (default in `toolkit/compose.yaml` ALL_TEX_LIVE_* +
  `TEX_LIVE_DOCKER_IMAGE`, `go/services/web/features/sitesettings/seeds.go`
  fallbacks, `go/services/clsitex/config/config.go` fallback list). We use
  its 2026 Dockerfiles verbatim (base = `texlive/Base/Dockerfile.2026`, full
  = `texlive/2026/Dockerfile`) + the tlnet snapshot cache image published by
  the same project (`ghcr.io/ayaka-notes/tlnet-cache:2026`); the
  `texlive/Base/extrafonts` fonts come from
  `https://github.com/ayaka-notes/overleaf-fonts` (pinned with the upstream
  tree). No code was copied into `go/` or `frontend/` — only the image build
  sources + the default-image config point to it.

## Git-bridge suspended-user PAT rejection (2026-10-11)

- **Upstream git source:** `origin <https://github.com/saveli/overleaf-cep.git>`, branch `fix/git-bridge-token-suspended`, commit `ea4005235ad7973be4ad0bd6d4d21be9371a88d6` ("git-bridge: reject personal access tokens of suspended users").
- **What was ported:** the PAT user-validity check in the web git-bridge feature (`tokenResolved`: the shared PAT lookup behind `/oauth/token/info` and the `/api/v0/docs` token checks) now also treats a token whose owner is **suspended** as an unknown token (same projection extension + early-return shape as the upstream `GitBridgePATManager.getUserId` patch), so a suspended user's git-bridge PATs 401 instead of still cloning/pulling/pushing. The web login gate already blocks suspended users; only the token surface was bypassing it.
- **Adopted (2026-10-11)**: gap confirmed in our tree (`grep -ri suspended go/services/gitbridge` = 0 hits; the tokenResolved user check decoded only `_id`). Single choke point — both auth surfaces use `tokenResolved`; session-owned PAT management (list/create/revoke) is correctly unaffected. Go build + feature tests green; the suspended-token round-trip e2e leg rides the test wave (682370f6 #7).

## File-view SVG preview (2026-10-11)

- **Upstream git source:** `origin <https://github.com/MarkQuach12/overleaf.git>`, branch `svg-file-preview`, commit `eb84ef11b386d7652047b4a8682e154218b38fc8` (upstream issue #1458, "[web] add SVG file preview to the editor file view").
- **What was ported:** `FileViewSvg` (fetch blob → re-wrap as `image/svg+xml` → object URL → `<img>`, 2 MB cap, 60 s fetch timeout, abort-controller cleanup), the `isSvgFile` routing branch in `FileView` (SVGs previously fell to "no preview" since `imageExtensions` excluded them), the `svgFile` test fixture, and the component + routing specs. Rendering goes through `<img>`, so SVG content is treated as an image rather than executable DOM (script-bearing SVGs cannot run) — this is inherited from the upstream approach and is the intended anti-XSS posture.
- **Adopted (2026-10-11)**: 7/7 file-view specs green (vitest/jsdom TypstT1 project, where this tree's mocha-idiom FE specs run; the socket mock gained the socket.io-legacy `removeListener` alias the editor-context unmount paths call). NOTE: the owner-authored `toast-svg`/`toast-image` editor modules (AD 2026-10-07) are untouched — their file-editing modals are a separate capability from this preview.

## Review-panel comment edited timestamp (2026-10-11)

- **Upstream git source:** `origin <https://github.com/BetterAndBetterII/overleaf.git>`, branch `cursor/comment-edited-timestamp-cf24`, commit `3ca22077e14bc35f46c4f8a83623af884239e36d` (upstream issue #1509).
- **What was ported:** the message header now shows the **edited** time (wire `edited_at`, epoch millis) with the "(edited)" marker (existing `edited` locale string; chat-panel marker pattern), the socket `edit-message` handler patches the `edited_at` field (not content-only), the track-changes `edit-message` socket emit carries the edited-at value (Go `hEditMessage`), `ReviewPanelCommentThreadMessage` gained `edited_at`, and a display spec was added. The chat service's `edited_at` persistence (`$set` on edit) already existed and is unchanged; the original timestamp remains the creation-order sort key.
- **Adopted (2026-10-11)**: bug reproduced here on both FE legs (display + socket); port = FE legs + the Go emit field. Component spec green (vitest/jsdom, TypstT1 project); Go build+test green.

## n-gram model plumbing & offline-server extension review

- `nschang/languagetool-101` (https://github.com/nschang/languagetool-101) —
  the step-by-step recipe reviewed for the grammar-model action set
  (`go/services/toolkit/actions.go`): n-gram official tier + the owner's listed
  untested tier, under the owner's stable `ngrams-<lang>.zip` naming, with the
  SSD caution. Its word2vec recipe was reviewed and deliberately NOT adopted:
  LanguageTool removed the `--word2vecmodel`/`--neuralnetworkmodel` options
  (unmaintained features, per languagetool-standalone CHANGES). FastText
  language-detection logged as an owner-gated candidate (needs the LT binary +
  lid.176.bin). Re-implemented: all Go action code (no copy of the recipe's
  shell steps).

## Editor color themes (J, 2026-10-09)

- **react-codemirror theme palettes (the "Editor themes" of the theme
  picker)** — 29 color themes vendored from the reference repo
  `/data_1/image_mining/the_diff/react-codemirror/themes` (the
  `@uiw/codemirror-theme-*` family of the react-codemirror project;
  per-theme color palettes and highlight-style tag maps). Their palettes were
  converted to this editor's cm6 registry format
  (`frontend/js/features/source-editor/themes/cm6/<name>.json`,
  `{ theme, highlightStyle, dark }`) by the generated
  `generate-react-codemirror-themes.mjs` in that directory (the conversion
  script is committed alongside the output; re-run it after upstream theme
  updates). Names colliding with the pre-existing OL cm6 themes
  (`dracula`, `eclipse`, `github`, `gruvbox`, `monokai`, `xcode`) keep the
  existing OL versions; the generic `theme/` library source (no palette) was
  not vendored. The themes are listed in the admin/user settings theme picker
  (hub) and in the editor's theme registry (`ol-editorThemes` meta,
  `go/services/web/features/editorpages/pinned.go`), per owner request J:
  "list the color themes from that repo as additional options in the theme
  toggle; each new option must correctly switch the editor theme on one
  surface; CREDITS.md gets the credit for the source themes."

## Source-editor list-item continuation (2026-10-11)

- **overleaf** (upstream keymap feature, closes overleaf#1384) — source-editor
  keymap that continues LaTeX lists: pressing Enter after an `\item` in the
  source editor inserts a new `\item` (a `\item[]` with the cursor inside the
  brackets for `description` lists, mirroring the item's optional argument),
  preserving indentation and not carrying trailing whitespace onto the new
  item; Shift-Enter always inserts a plain newline; no continuation for empty
  lists, and none inside verbatim/math. Ported from the upstream git source
  `origin <https://github.com/MarkQuach12/overleaf.git>`, branch
  `feature/source-editor-list-item-continuation`, commits
  `448ff7d6e76c847cc94464d395f2151b2f0754ac` +
  `908673981519680d9ca6280d809f8cec9e6d71b5`, to
  `frontend/js/features/source-editor/languages/latex/list-continuation.ts`,
  wired into `languages/latex/index.ts`; behavior spec (vitest+jsdom, this
  repo's live FE harness — the mocha/cypress test:frontend harness is retired
  in this tree) committed at
  `frontend/test/frontend/features/source-editor/languages/latex/`. We reuse
  our own `createListItem` / `ancestorOfNodeWithType` / lezer-latex terms
  (same signatures this code already depended on upstream).
