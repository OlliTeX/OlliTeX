import { useState } from 'react'
import { OLDropdownItem } from '@/shared/components/ol/ol-dropdown-menu'
import { useTranslation } from 'react-i18next'
import getMeta from '@/utils/meta'
import type {
  NavbarLinkItemData,
  NavbarSessionUser,
} from '@/shared/components/types/navbar'
import DropdownListItem from '@/shared/components/dropdown/dropdown-list-item'
import NavDropdownDivider from './nav-dropdown-divider'
import NavDropdownLinkItem from './nav-dropdown-link-item'
import NavSectionLabel from './nav-section-label'
import { useDsNavStyle } from '@/features/project-list/components/use-is-ds-nav'
import { CaretRight, SignOut } from '@phosphor-icons/react'
import ThemeToggle from '@/features/project-list/components/sidebar/theme-toggle'
import { ConnectionOutageTracker } from '@/features/ide-react/editor/connection-outage-tracker'

/**
 * "Manage instance" accordion (owner UX 2026-10-06 redesign): the inline
 * accordion pattern from the 2026-08-28 rounds (flyout did not unfold),
 * now with a functional item order — overview first, then site, users,
 * content, and the flag-gated diagnostics last:
 *
 *   Dashboard (opt) → Manage Site → Manage Extensions → Site LLM
 *   → Manage Users → Manage Projects (opt) → Manage template gallery (opt)
 *   → Manage Feature Flags / Surveys / Script Logs (flag-gated)
 *
 * (The historical names are kept — parity + muscle memory — only the
 * GROUPING and ORDER changed, per the owner's "functional menu grouping"
 * ask. All hrefs/flags are unchanged.)
 */
function ManageInstanceMenu({
  canManageProjects,
  canDisplayInstanceStats = false,
  showGallery,
  canDisplaySplitTestMenu,
  canDisplaySurveyMenu,
  canDisplayScriptLogMenu,
}: {
  canManageProjects: boolean
  canDisplayInstanceStats?: boolean
  showGallery: boolean
  canDisplaySplitTestMenu?: boolean
  canDisplaySurveyMenu?: boolean
  canDisplayScriptLogMenu?: boolean
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const items = [{ href: '/admin/panel', label: 'Manage Site' }]
  // overleaf-lab: explicit entry for the site LLM settings (the /admin
  // "LLM Configuration" tab remains as well).
  items.push(
    { href: '/admin/llm/settings', label: 'Site LLM' },
    { href: '/admin/site', label: 'Manage Extensions' },
    { href: '/admin/user', label: 'Manage Users' },
  )
  if (canManageProjects) items.push({ href: '/admin/project', label: 'Manage Projects' })
  if (showGallery) items.push({ href: '/templates/manage', label: t('Manage template gallery') })
  if (canDisplaySplitTestMenu) items.push({ href: '/admin/split-test', label: 'Manage Feature Flags' })
  if (canDisplaySurveyMenu) items.push({ href: '/admin/survey', label: 'Manage Surveys' })
  if (canDisplayScriptLogMenu) items.push({ href: '/admin/script-logs', label: 'View Script Logs' })
  if (canDisplayInstanceStats) items.unshift({ href: '/admin/instance-stats', label: 'Dashboard' })

  return (
    <>
      <li role="none" className="nav-section" aria-hidden="true">
        <span className="nav-section-label">Manage instance</span>
      </li>
      <li role="none">
        <button
          type="button"
          role="menuitem"
          className="dropdown-item manage-submenu-toggle d-flex align-items-center justify-content-between"
          aria-haspopup="true"
          aria-expanded={open}
          onClick={() => setOpen(o => !o)}
        >
          <span>Admin</span>
          <CaretRight
            size={14}
            weight="bold"
            className={`manage-menu-caret ${open ? 'is-open' : ''}`}
          />
        </button>
      </li>
      {open ? items.map(item => (
        <li key={item.href} role="none">
          <a href={item.href} role="menuitem" className="dropdown-item manage-menu-link">
            {item.label}
          </a>
        </li>
      )) : null}
    </>
  )
}

function useNavExtraItems(sessionUser: NavbarSessionUser | undefined) {
  const items = getMeta('ol-navbar')?.items ?? []
  const suppressNavContentLinks =
    getMeta('ol-navbar')?.suppressNavContentLinks ?? false
  return items.filter((item): item is NavbarLinkItemData => {
    if (!('url' in item)) return false
    if (item.only_when_logged_in && item.only_when_logged_out) return false
    if (item.only_when_logged_in && !sessionUser) return false
    if (item.only_when_logged_out && sessionUser) return false
    if (item.only_content_pages) return !suppressNavContentLinks
    return true
  })
}

/**
 * Account menu — functional grouping (owner UX 2026-10-06):
 *
 *   Workspace      Projects · Library · Templates
 *   Personal       Account settings (+ AI settings flag) · (theme flag)
 *   Manage inst.   accordion (all admin links, functional order)
 *   (divider)      Sign out
 *
 * The top navbar keeps its two content links + Admin dropdown unchanged;
 * only this menu (the one that scrolled) was re-grouped.
 */
export function AccountMenuItems({
  sessionUser,
  showThemeToggle = false,
}: {
  sessionUser: NavbarSessionUser
  showThemeToggle?: boolean
}) {
  const { t } = useTranslation()
  const logOutFormId = 'logOutForm'
  const dsNavStyle = useDsNavStyle()
  const hasOverallThemes = Boolean(getMeta('ol-overallThemes'))
  const navExtraItems = useNavExtraItems(sessionUser)
  const nav = (getMeta('ol-navbar') ?? {}) as {
    canDisplayAdminMenu?: boolean
    canDisplayProjectUrlLookup?: boolean
    canDisplayAdminRedirect?: boolean
    canDisplaySplitTestMenu?: boolean
    canDisplaySurveyMenu?: boolean
    canDisplayScriptLogMenu?: boolean
    adminUrl?: string
    canDisplayInstanceStats?: boolean
  }
  const isAdmin =
    Boolean(nav.canDisplayAdminMenu) ||
    Boolean(nav.canDisplayProjectUrlLookup) ||
    Boolean(nav.canDisplayAdminRedirect)
  const showGallery = Boolean(getMeta('ol-ExposedSettings')?.canManageTemplatesMenu)
  const showFlags =
    Boolean(nav.canDisplaySplitTestMenu) ||
    Boolean(nav.canDisplaySurveyMenu) ||
    Boolean(nav.canDisplayScriptLogMenu)

  return (
    <>
      <OLDropdownItem as="li" disabled role="menuitem">
        {sessionUser.email}
      </OLDropdownItem>
      <li role="none" className="nav-section" aria-hidden="true">
        <span className="nav-section-label">Workspace</span>
      </li>
      <NavDropdownLinkItem href="/hub#/projects.all">{t('projects')}</NavDropdownLinkItem>
      {navExtraItems.map((item, index) => (
        <NavDropdownLinkItem key={index} href={item.url}>
          {item.translatedText || item.text}
        </NavDropdownLinkItem>
      ))}
      <li role="none" className="nav-section" aria-hidden="true">
        <span className="nav-section-label">Personal</span>
      </li>
      <NavDropdownLinkItem href="/user/mysettings">
        {t('account_settings')}
      </NavDropdownLinkItem>
      {getMeta('ol-ExposedSettings')?.llmAllowUserSettings ? (
        <NavDropdownLinkItem href="/hub#/mysettings.llm.general">
          {t('ai_settings', 'AI Settings')}
        </NavDropdownLinkItem>
      ) : null}
      {showThemeToggle && hasOverallThemes && (
        <DropdownListItem>
          <ThemeToggle />
        </DropdownListItem>
      )}
      {(isAdmin || showGallery || showFlags) && (
        <>
          <NavDropdownDivider />
          <ManageInstanceMenu
            canManageProjects={Boolean(nav.canDisplayProjectUrlLookup)}
            canDisplayInstanceStats={Boolean(nav.canDisplayInstanceStats)}
            showGallery={showGallery}
            canDisplaySplitTestMenu={nav.canDisplaySplitTestMenu}
            canDisplaySurveyMenu={nav.canDisplaySurveyMenu}
            canDisplayScriptLogMenu={nav.canDisplayScriptLogMenu}
          />
          {nav.canDisplayAdminRedirect && nav.adminUrl ? (
            <NavDropdownLinkItem href={nav.adminUrl}>
              Switch to Admin
            </NavDropdownLinkItem>
          ) : null}
        </>
      )}
      <NavDropdownDivider />
      <DropdownListItem>
        {
          // The button is outside the form but still belongs to it via the
          // form attribute. The reason is that if the button is inside the
          // form, screen readers will not count it in the total number of
          // menu items
        }
        <OLDropdownItem
          as="button"
          type="submit"
          form={logOutFormId}
          role="menuitem"
          className="d-flex align-items-center justify-content-between"
        >
          <span>{t('log_out')}</span>
          {dsNavStyle && <SignOut size={16} />}
        </OLDropdownItem>
        <form
          id={logOutFormId}
          method="POST"
          action="/logout"
          onSubmit={() => {
            ConnectionOutageTracker.clearAll()
          }}
        >
          <input type="hidden" name="_csrf" value={getMeta('ol-csrfToken')} />
        </form>
      </DropdownListItem>
    </>
  )
}
