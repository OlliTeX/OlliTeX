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
import './editor-v2-tokens.css'

// Named export first: the webpack/babel CJS transform in this workspace is
// not trusted with `export default function` (it dropped the default
// binding — that broke the first P1 builds). Consumers use the NAME.
export function EditorMantineShell({ children }: { children: React.ReactNode }) {
  return <OlliTProvider>{children}</OlliTProvider>
}

export default EditorMantineShell
