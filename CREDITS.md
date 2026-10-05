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

- **charmbracelet** — `wish` (SSH server), `bubbletea` (TUI event loop),
  `bubbles`, `lipgloss` (styling). Used by the operator TUI in
  `go/services/toolkit`.

- **bubbletea-console-classic libraries (MIT, credited per the policy)** —
  the classic-console (midnight-commander / freebsd-installer) TUI structure
  in `go/services/toolkit` (menu bar + two-pane layout + prompt boxes +
  mouse click targets) borrows three small Charm-ecosystem libraries, used
  through their public APIs (no code copied):
  - **jejacks0n/bubbletea-menubar**
    (https://github.com/jejacks0n/bubbletea-menubar) — the top menu bar
    (`ui_menu.go`): the `File · Stack · Settings · Shells · Doctor · Backup ·
    Actions · Help` strip with hotkeys + dropdowns + its own mouse handling.
  - **rmhubbert/bubbletea-overlay**
    (https://github.com/rmhubbert/bubbletea-overlay) — the centered prompt
    boxes (`ui_dialog.go`): `Composite`/`Position` do the background/foreground
    compositing + centering of the mc-style confirm and input dialogs over the
    panes.
  - **lrstanley/bubblezone**
    (https://github.com/lrstanley/bubblezone) — zone click targets
    (`ui_view.go` / `ui_dialog.go` / `ui.go`): `zone.Mark`/`zone.Scan`/
    `zone.Get` register the master-list rows, the keystrip chips and the
    dialog buttons as hit-testable zones (the classic clickable UI).

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
