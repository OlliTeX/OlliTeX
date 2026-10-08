import useSetOverallTheme from '@/features/ide-settings/hooks/use-set-overall-theme'
import MaterialIcon from '@/shared/components/material-icon'
import OLTooltip from '@/shared/components/ol/ol-tooltip'
import { useUserSettingsContext } from '@/shared/context/user-settings-context'
import getMeta from '@/utils/meta'
import { OverallThemeMeta } from '@ol-types/project-settings'
import { useTranslation } from 'react-i18next'

const getIcon = (theme: OverallThemeMeta) => {
  switch (theme.val) {
    case 'light-':
      return 'light_mode'
    case 'system':
      return 'computer'
    default:
      return 'dark_mode'
  }
}

export default function ThemeToggle() {
  const {
    userSettings: { overallTheme },
  } = useUserSettingsContext()
  const setOverallTheme = useSetOverallTheme()
  const overallThemes = getMeta('ol-overallThemes')
  const { t } = useTranslation()

  // AJ-6 (owner 2026-10-08): the theme BUTTONS (three icon radios —
  // dark/light/system) are RETIRED in favour of ONE dropdown. Per-user +
  // global semantics unchanged (useSetOverallTheme persists the user
  // setting — the same store the /editor Appearance tab reads).
  return (
    <fieldset className="dropdown-item theme-toggle">
      <legend>{t('theme')}</legend>
      <select
        className="theme-toggle-select"
        aria-label={t('theme')}
        value={overallTheme}
        onChange={e => {
          const chosen = overallThemes?.find(x => x.val === e.target.value)
          if (chosen) setOverallTheme(chosen.val)
        }}>
        {overallThemes?.map(theme => (
          <option key={theme.val} value={theme.val}>
            {theme.name}
          </option>
        ))}
      </select>
    </fieldset>
  )
}