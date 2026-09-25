/**
 * DS page account menu — the down-left sidebar account menu (".ds-nav-sidebar-lower")
 * the golden /admin/site page renders (modules/admin-tools site-settings),
 * packaged as a STANDALONE widget so any DS-nav page (LLM admin/settings,
 * notification preferences) can mount the exact same menu:
 *   * Shared AccountMenuItems (email, Account settings, theme toggle, log out)
 *   * Same providers as the golden (SplitTest + UserSettings)
 *   * Same ace.overallTheme write path (ThemeToggle -> POST /user/settings)
 * The CE+ badge is intentionally NOT rendered (removed by owner decision R).
 * 2026-09-03 (P, owner): /user/llm-settings, /admin/llm/settings and
 * /user/notification-preferences were missing the down-left user menu.
 */
import React from 'react'
import { Dropdown } from 'react-bootstrap'
import { User as UserIcon } from '@phosphor-icons/react'
import { useTranslation } from 'react-i18next'
import useWaitForI18n from '@/shared/hooks/use-wait-for-i18n'
import withErrorBoundary from '@/infrastructure/error-boundary'
import { AccountMenuItems } from '@/shared/components/navbar/account-menu-items'
import { SplitTestProvider } from '@/shared/context/split-test-context'
import { UserSettingsProvider } from '@/shared/context/user-settings-context'
import getMeta from '@/utils/meta'

export default function DsPageAccountMenu() {
  const { isReady, error } = useWaitForI18n()
  const { t } = useTranslation()

  // Hook order note: useWaitForI18n() + useI18nTranslation() are the ONLY
  // unconditional hooks; the early return is after both, as required.
  if (!isReady) {
    if (error) throw error
    return null
  }

  const meta = (getMeta('ol-navbar') ?? {}) as {
    sessionUser?: { email?: string }
  }
  const sessionUser = meta?.sessionUser
  if (!sessionUser?.email) {
    return null
  }

  return (
    <div className="ds-nav-sidebar-lower">
      <nav className="d-flex flex-row gap-3 mb-2" aria-label="account help">
        <Dropdown className="ds-nav-icon-dropdown" role="menu">
          <Dropdown.Toggle role="menuitem" aria-label={t('Account')}>
            <div>
              <UserIcon size={24} />
            </div>
          </Dropdown.Toggle>
          <Dropdown.Menu
            as="ul"
            role="menu"
            align="end"
            popperConfig={{
              modifiers: [{ name: 'offset', options: { offset: [-50, 5] } }],
            }}
          >
            <AccountMenuItems
              sessionUser={sessionUser}
              showThemeToggle={true}
            />
          </Dropdown.Menu>
        </Dropdown>
      </nav>
    </div>
  )
}

const Wrapped = withErrorBoundary(DsPageAccountMenu)

export function DsPageAccountMenuWithProviders({ rootId }: { rootId?: string }) {
  return (
    <SplitTestProvider>
      <UserSettingsProvider>
        <Wrapped key={rootId} />
      </UserSettingsProvider>
    </SplitTestProvider>
  )
}
