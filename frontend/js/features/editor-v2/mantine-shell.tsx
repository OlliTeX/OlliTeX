/**
 * editor-v2 — Mantine shell for the renovated editor (/editor route only).
 *
 * This module is a DYNAMIC-IMPORT CHUNK (see variant.tsx): the /Project
 * bundle resolves it only when the page is actually /editor. It bundles:
 *   - the OlliT Mantine provider (shared/mantine/provider.tsx) with the
 *     OlliTeX brand theme (OL green / OL neutral, Noto Sans, 8px radius)
 *     AND its CSS (@mantine/core/styles.css) — so Mantine styling + the
 *     provider context are both available to the renovated surfaces;
 *   - the token-bridge stylesheet (editor-v2-tokens.css): scoped under
 *     `.ol-editor-mantine`, it maps the legacy app variables onto the
 *     Mantine tokens so legacy chrome and Mantine chrome share one palette
 *     inside the renovated editor.
 */
import React from 'react'
import OlliTProvider from '@/shared/mantine/provider'
// Material Symbols webfont: every legacy MaterialIcon in the editor page
// (menu rows from change-layout-options/viewing-mode-options, shortcut
// glyphs, file-tree icons) relies on this @font-face. The hub page imports
// the same file itself (hub.tsx); the v2 editor chunk must do the same or
// every ligature renders as raw text (owner-reported on AC). 2026-10-09.
import '../../../fonts/material-symbols/material-symbols.css'
import './editor-v2-tokens.css'

// Named export first: the webpack/babel CJS transform in this workspace is
// not trusted with `export default function` (it dropped the default
// binding — that broke the first P1 builds). Consumers use the NAME.
export function EditorMantineShell({ children }: { children: React.ReactNode }) {
  return <OlliTProvider>{children}</OlliTProvider>
}

export default EditorMantineShell
