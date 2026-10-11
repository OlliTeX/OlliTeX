{
  "id": "1efc5ff7",
  "title": "[AD] color themes only flip light/dark — do the vendored themes carry real per-theme color values?",
  "tags": "psintern, editor, themes, AD, colors",
  "status": "done",
  "created_at": 1791549314865,
  "last_change": 1791572823551,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

AUDIT VERDICT (2026-10-09, v19): the themes ARE populated with real per-theme color values and DO apply — verified with computed-style evidence, not class names. One genuine resolution bug was found and fixed.

## 1) Data audit (frontend/js/features/source-editor/themes/cm6/*.json, 71 themes + index of 29)
Distinct real palettes per theme (samples from the generated JSON):
- tokyo-night: bg #1a1b26, fg #787c99, keyword #bb9af7 (12 token rules)
- dracula:    bg #282a36, fg #f8f8f2, keyword #ff79c6 (10 token rules)
- monokai:    bg #272822, fg #F8F8F2, keyword #F92672 (11 token rules)
- atomone:    bg #272C35, variable hsl(207,82%,66%) (10 token rules)
- github:     bg #fff,  fg #000, string #D14 (light)
- dawn:       bg #F9F9F9 (light); white: bg #fff (light)
Allowlist cross-check (go/services/web/features/editorpages/pinned.go): all 69 names in ol-editorThemes (36) + ol-legacyEditorThemes (32) have JSON files; zero missing. `one_dark.json` is the only file not offered (intentional).

## 2) Live computed-style evidence (v19, /editor/6ac9175e…, fresh loads)
- tokyo-night (pair path): .cm-editor bg rgb(26,27,38)=#1a1b26; tk kw rgb(187,154,247)=#bb9af7; comment rgb(68,75,106)
- dracula (pair path): bg rgb(40,42,54)=#282a36; kw rgb(255,121,198)=#ff79c6
- white (picker/overall path): bg WHITE, fg black → full light switch incl. chrome (bodyBg white)
- tokyo-night-day (overall): bg rgb(225,226,231), fg rgb(55,96,191), kw rgb(0,113,151) — light + distinct
⇒ different themes produce different, correct surfaces + token colors.

## 3) BUG FOUND + FIXED (shipped v19 `olpsint/overleaf:g-2026-10-09-v19`)
shared/hooks/use-active-editor-theme.tsx: `isVendoredTheme()` gated on the 29-theme index.json only. Any OTHER legitimate name stored as overallTheme (42 allowlist names: dracula, monokai, github, vscode, nord, material, eclipse, xcode, textmate, …) silently fell through to the paired settings → wrong theme rendered (measured: overleaf_dark — kw #ff79c6 — standing in for github). Fix: the gate now accepts the vendored 29 ∪ the page's `ol-editorThemes` + `ol-legacyEditorThemes` allowlists (same lists loadSelectedTheme honors; hub pages without the metas keep the old 29-set behavior). VERIFIED v19: overallTheme='dracula' → bg #282a36 + kw #ff79c6 (was overleaf_dark); overallTheme='monokai' → bg #272822 + kw #F92672.

## 4) Why it LOOKED like "only light/dark flips" (owner-facing explanation)
- 22 of the 29 picker themes are dark. Between dark themes the editor SURFACE is a subtly different dark (e.g. #1a1b26 vs #282a36 vs #272822) — easy to read as "no change" — while the real per-theme differences live in the TOKEN colors (keywords/strings/types per palette).
- When a LIGHT theme is picked (White, Tokyo Night Day, Solarized, Bbedit, Duotone, Noctis Lilac, Quiet Light) the WHOLE app flips to light (chrome + editor), which is the big visible difference.
- Plus: before v19, any theme name outside the 29 (e.g. chosen via My settings → Appearance editor selects, which list all 69) silently fell back — now fixed.

GATES: tsc scoped clean; hub vitest green (5/5 nav; the 3 hub-shell admin failures are the known pre-existing ones, stash-verified before this work); Storybook 296+7 GREEN. Probe user's settings restored (overallTheme='') after the test.
