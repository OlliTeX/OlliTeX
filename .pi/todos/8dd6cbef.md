{
  "id": "8dd6cbef",
  "title": "Step 0: project prep for Mantine 9.6.0 (React 19.2 + deps + theme + gates)",
  "tags": [
    "mantine",
    "react19",
    "foundation"
  ],
  "status": "done",
  "created_at": 1788698295848,
  "last_change": 1788782387812,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## Step 0 — foundation
- bump services/web: react ^18.3.1→^19.2.0, react-dom→^19.2.0, @types/react→^19.2.x, @types/react-dom→^19.2.x
- add: @mantine/core@9.6.0, @mantine/hooks@9.6.0, @mantine/form@9.6.0, @mantine/notifications@9.6.0
- check no hard react@18 dep (PnP dual-React risk); `yarn why react` after install
- OlliTeX Mantine theme (palette/radius/font from brand; dark bridge from body[data-theme])
- shared mantine entry (Provider + styles.css imports) wired for hub pages
- GATES: make all green; vitest; E2E full suite (editor canary hard gate)
- commit (own commit, bisectable)
