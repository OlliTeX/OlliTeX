import { useActiveOverallTheme } from './use-active-overall-theme'
import { useUserSettingsContext } from '../context/user-settings-context'
import getMeta from '@/utils/meta'
import { themes as VENDORED_THEMES } from '../../features/source-editor/themes/cm6/index.json'

// J (2026-10-09): the theme picker can name a vendored editor theme
// directly (saved as overallTheme). That explicit choice wins over the
// paired editorLightTheme/editorDarkTheme settings.
//
// AD (owner 2026-10-09 audit): the gate previously only knew the 29-theme
// index.json, so ANY other legitimate theme name saved as overallTheme
// (dracula/monokai/github/vscode/nord/… — 42 more in the Go allowlist,
// incl. legacy: eclipse/xcode/textmate/overleaf) silently fell through to
// the paired settings and rendered the WRONG theme (measured: overleaf_dark
// in place of github). Now: an explicit pick is valid if it is in the
// vendored index OR in the page's theme allowlists (ol-editorThemes /
// ol-legacyEditorThemes — the same lists loadSelectedTheme honors). On
// pages without those metas (hub) the set stays the 29, as before.
let allowedNames: Set<string> | null = null
function themeNamesAllowed(): Set<string> {
  if (allowedNames) return allowedNames
  const s = new Set<string>(VENDORED_THEMES.map(t => t.name))
  try {
    for (const key of ['ol-editorThemes', 'ol-legacyEditorThemes'] as const) {
      const list = (getMeta(key) as Array<{ name?: string }> | undefined) || []
      for (const t of list) {
        if (t && typeof t.name === 'string' && t.name) s.add(t.name)
      }
    }
  } catch {
    // metas unavailable (SSR/hub page) — vendored-only set is the fallback
  }
  allowedNames = s
  return s
}

function isVendoredTheme(v: string | undefined): v is string {
  return typeof v === 'string' && themeNamesAllowed().has(v)
}

/**
 * The editor code theme always follows the ACTIVE overall theme:
 * dark UI → editorDarkTheme, light UI → editorLightTheme (system mode
 * resolves via the OS preference, like the rest of the chrome). The
 * legacy single `editorTheme` field remains only as a fallback for
 * profiles that have neither paired setting.
 *
 * Owner 2026-09-13 editor wave (#7 "zotero.bib unreadable, not tied to
 * the editor theme" · #15 appearance not used): before, ANY explicit
 * overall theme (including the default dark) forced the legacy single
 * editorTheme no matter what the appearance settings said — so a dark UI
 * could render a light code theme (e.g. textmate), making text inside
 * linked files like zotero.bib unreadable. The paired settings (the
 * "Editor theme" light/dark selects in My settings) are now the source
 * of truth, exactly as their Settings UI labels them.
 */
export const useActiveEditorTheme = () => {
  const activeOverallTheme = useActiveOverallTheme()
  const {
    userSettings: {
      editorTheme,
      editorLightTheme,
      editorDarkTheme,
      overallTheme,
    },
  } = useUserSettingsContext()
  if (isVendoredTheme(overallTheme)) return overallTheme
  const paired =
    activeOverallTheme === 'dark' ? editorDarkTheme : editorLightTheme
  return paired || editorTheme
}
