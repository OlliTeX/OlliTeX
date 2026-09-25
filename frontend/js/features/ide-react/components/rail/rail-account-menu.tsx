/**
 * 2026-09-09 (owner R10 #4): account menu in the project-editor rail.
 *
 * The editor is a full-bleed page — the account dropdown that lives in
 * the project-list sidebar (ds-nav-sidebar-lower) is not available there.
 * This adds the same essentials (account, project links, settings, log
 * out) as an icon-only rail action right below the Settings rail button,
 * mirroring the navbar account menu (account-menu-items.tsx).
 */
import { useTranslation } from 'react-i18next'
import {
  OLDropdownDivider,
  OLDropdownItem,
  OLDropdownMenu,
} from '@/shared/components/ol/ol-dropdown-menu'
import getMeta from '@/utils/meta'

function userEmail(): string {
  const s = getMeta('ol-userSettings') as
    | { sessionUser?: { email?: string } }
    | undefined
  return s?.sessionUser?.email ?? ''
}

export function RailAccountMenu() {
  const { t } = useTranslation()
  const email = userEmail()

  return (
    <OLDropdownMenu>
      {email && (
        <div
          className="dropdown-item"
          aria-disabled="true"
          style={{ cursor: 'default' }}
        >
          {email}
        </div>
      )}
      {email && <OLDropdownDivider />}
      {/* Owner #1 (2026-09-13 editor wave): the editor rail account menu
          follows the OlliTEX /hub structure — the legacy pages are retired;
          hub folder ids resolve to their first leaf (projects.all, ...). */}
      <OLDropdownItem href="/hub#/projects" role="menuitem">
        {t('projects')}
      </OLDropdownItem>
      <OLDropdownItem href="/hub#/library" role="menuitem">
        {t('library')}
      </OLDropdownItem>
      <OLDropdownItem href="/hub#/templates" role="menuitem">
        {t('templates')}
      </OLDropdownItem>
      <OLDropdownItem href="/hub#/mysettings" role="menuitem">
        {t('account_settings')}
      </OLDropdownItem>
      <OLDropdownItem href="/hub#/mysettings.llm" role="menuitem">
        {t('nav.aiSettings', 'AI Settings')}
      </OLDropdownItem>
      <OLDropdownDivider />
      {/* form-scoped log out (no global form on the editor page) */}
      <span className="d-block">
        <button
          type="submit"
          form="rail-logOutForm"
          className="dropdown-item"
          role="menuitem"
        >
          {t('log_out')}
        </button>
      </span>
      <form
        id="rail-logOutForm"
        method="POST"
        action="/logout"
        style={{ display: 'none' }}
      >
        <input type="hidden" name="_csrf" value={getMeta('ol-csrfToken')} />
      </form>
    </OLDropdownMenu>
  )
}
