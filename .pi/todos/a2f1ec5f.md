{
  "id": "a2f1ec5f",
  "title": "Toolkit TUI: Bubble Tea wish SSH app in Docker (retire /toolkit scripts)",
  "tags": [
    "toolkit",
    "tui",
    "wish",
    "bubbletea",
    "docker",
    "configstore"
  ],
  "status": "done",
  "created_at": 1791073131550,
  "last_change": 1791507168815,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## Toolkit TUI (wish/bubbletea SSH app) — COMPLETE (verified 2026-10-06)
- go/services/toolkit: full TUI (views: status/logs/config/compose actions, hub user+project stat views (65c003a6), backups action (9a282672), migration view, logs tail, compose up/down/restart, config get/set) — committed + pushed (85419075 base / a2f1ec5f chain; final image ollitex/toolkit-tui:main on registry).
- cmd/toolkit binary + images/toolkit-amd64; toolkit/bin/docker-compose shim; live-verified driving the toolkit stack (ollitex-toolkit-*) from the TUI + SSH session smoke.
- /toolkit scripts retirement + docs (docs/wiki/installation/05-toolkit-ssh.md).
