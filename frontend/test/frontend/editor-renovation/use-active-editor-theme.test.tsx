import { describe, expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'

/**
 * useActiveEditorTheme — owner #7 (2026-09-13 editor wave, "zotero.bib
 * unreadable, not tied to the editor theme") + #15: the editor code theme
 * must follow the ACTIVE overall theme. Dark UI → editorDarkTheme, light
 * UI → editorLightTheme; the legacy single `editorTheme` is only a
 * fallback when the paired setting is empty.
 *
 * (This is the runnable vitest twin of the mocha spec at
 * test/frontend/shared/hooks/use-active-editor-theme.spec.tsx, which the
 * current Node22/yarn-PnP mocha harness cannot execute.)
 */
let overall = 'dark'
let userSettings = {}
vi.mock('@/shared/hooks/use-active-overall-theme', () => ({
  useActiveOverallTheme: () => overall,
}))
vi.mock('@/shared/context/user-settings-context', () => ({
  useUserSettingsContext: () => ({ userSettings }),
}))
import { useActiveEditorTheme } from '@/shared/hooks/use-active-editor-theme'

// User-settings defaults (user-settings-context): editorDarkTheme
// 'overleaf_dark', editorLightTheme 'textmate'.
const DARK = 'overleaf_dark'
const LIGHT = 'textmate'

const cases: Array<{
  overall: 'dark' | 'light'
  settings: Record<string, string>
  want: string
}> = [
  // paired setting present -> it wins (both overall themes)
  {
    overall: 'dark',
    settings: { editorTheme: 'textmate', editorDarkTheme: 'cobalt', editorLightTheme: '' },
    want: 'cobalt',
  },
  {
    overall: 'light',
    settings: { editorTheme: 'textmate', editorDarkTheme: 'dracula', editorLightTheme: 'textmate' },
    want: LIGHT,
  },
  {
    overall: 'light',
    settings: { editorTheme: 'textmate', editorDarkTheme: '', editorLightTheme: DARK },
    want: DARK,
  },
  // paired empty -> legacy single editorTheme fallback
  {
    overall: 'dark',
    settings: { editorTheme: 'textmate', editorDarkTheme: '', editorLightTheme: '' },
    want: 'textmate',
  },
  {
    overall: 'light',
    settings: { editorTheme: 'eclipse', editorDarkTheme: '', editorLightTheme: '' },
    want: 'eclipse',
  },
  // the owner's reported case: dark UI + legacy light editor + paired dark
  // set -> dark editor (dark on dark, readable linked-file text)
  {
    overall: 'dark',
    settings: { editorTheme: 'textmate', editorDarkTheme: 'overleaf_dark', editorLightTheme: 'textmate' },
    want: 'overleaf_dark',
  },
  // completely empty config -> '' (the real app never hits this:
  // defaultSettings fill overleaf_dark/textmate)
  {
    overall: 'dark',
    settings: { editorTheme: '', editorDarkTheme: '', editorLightTheme: '' },
    want: '',
  },
]

describe('useActiveEditorTheme (#7/#15 wave 2026-09-13)', () => {
  for (const c of cases) {
    it(
      `overall=${c.overall} settings=${JSON.stringify(c.settings)} -> ${c.want || '(empty)'}`,
      () => {
        overall = c.overall
        userSettings = c.settings
        const { result } = renderHook(() => useActiveEditorTheme())
        expect(result.current).toBe(c.want)
      },
    )
  }
})
