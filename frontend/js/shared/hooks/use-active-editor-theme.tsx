import { useActiveOverallTheme } from './use-active-overall-theme'
import { useUserSettingsContext } from '../context/user-settings-context'

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
    userSettings: { editorTheme, editorLightTheme, editorDarkTheme },
  } = useUserSettingsContext()
  const paired =
    activeOverallTheme === 'dark' ? editorDarkTheme : editorLightTheme
  return paired || editorTheme
}
