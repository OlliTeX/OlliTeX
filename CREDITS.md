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

- **Moby (Docker)** — `github.com/moby/moby/client` (official Go client) for all
  daemon operations (container list/logs/images, exec attach for interactive
  shells, restart). The `stdcopy` stream demultiplexing in
  `go/services/toolkit/shell.go` transcribes moby's documented frame format
  (8-byte header: stream + 3 pad + BE32 size).

- **SeaweedFS** — `chrislusf/seaweedfs` image, used as the local S3 object
  storage backend for filestore + docstore.

- **LanguageTool** — `erikvl87/languagetool` image plus the owner-maintained
  ngram/languagemodel packs mounted read-only at `/ngrams`.

## n-gram / word2vec model plumbing & offline-server extension recipe

- `nschang/languagetool-101` (https://github.com/nschang/languagetool-101) —
  the step-by-step recipe we mirror in `go/services/toolkit/actions.go` (n-gram
  official + untested tiers under the owner's stable `ngrams-<lang>.zip` naming,
  word2vec en/de/pt, SSD caution). Borrowed: the recipe/structure. Re-implemented:
  the Go action code (not a copy of any script).
