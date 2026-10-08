/**
 * editor-v2 P2 — the renovated rail ICON CHROME (Mantine), in place.
 *
 * P2 discipline (hard lesson): the rail pane participates in a
 * react-resizable-panels PanelGroup whose layout assumes the legacy DOM
 * skeleton (TabContainer > nav > panel). Re-wrapping that skeleton in a
 * fresh component tree collapses the pane (width 0). So the v2 rail swaps
 * only the ICON LIST — the exact node the legacy rail.tsx renders inside
 * its unchanged <nav> landmark — for the Mantine-framed list:
 *
 *   tabs      → Mantine ActionIcon (+ Tooltip), same data/handlers/refs
 *   shortcuts → v2 first-class entry (setActiveModal('keyboard-shortcuts'),
 *               the same surface the legacy help dropdown opens — matrix
 *               row "rail-shortcuts")
 *   actions   → simple actions: Mantine ActionIcon; dropdown actions
 *               (help · account · overflow): the legacy self-managed
 *               RailActionElement verbatim — its OLDropdown owns the open
 *               state, and the dropdown nodes (RailHelpDropdown ·
 *               RailAccountMenu · RailOverflowDropdown) only render
 *               through it.
 *
 * Everything else — RailPanel, resize handle, rail modals,
 * module popovers, TabContainer context — is UNCHANGED and shared with
 * /project. The legacy rail.tsx gets a single runtime branch choosing this
 * chrome for the /editor route; /project keeps the byte-identical legacy
 * list.
 */
import { useEffect, useState } from 'react'
import { ActionIcon, Tooltip } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import MaterialIcon from '@/shared/components/material-icon'
import classNames from 'classnames'
import RailActionElement from '@/features/ide-react/components/rail/rail-action-element'
import { useLayoutContext } from '@/shared/context/layout-context'
import { shouldIncludeElement } from '@/features/ide-react/util/rail-utils'
import type { RailElement } from '@/features/ide-react/util/rail-types'

function V2RailTabButton({
  tab,
  open,
  onClick,
  refEl,
}: {
  tab: RailElement
  open: boolean
  onClick: () => void
  refEl?: React.Ref<HTMLButtonElement>
}) {
  const { t } = useTranslation()
  const label = tab.title || t('untitled')
  return (
    <Tooltip label={label} position="right" withArrow withinPortal>
      <ActionIcon
        ref={refEl}
        variant={open ? 'filled' : 'subtle'}
        aria-label={label}
        aria-pressed={open}
        disabled={tab.disabled}
        onClick={onClick}
        className={classNames('ol-v2-rail-tab', {
          'ol-v2-rail-tab-active': open,
          'has-indicator': !!tab.indicator,
        })}
        style={{ width: 40, height: 40, fontSize: 20, position: 'relative' }}
      >
        <MaterialIcon type={tab.icon} aria-hidden />
        {tab.indicator}
      </ActionIcon>
    </Tooltip>
  )
}

export function MantineRailNavChrome({
  tabs,
  moreOptions,
  actions,
  selectedTab,
  isOpen,
  onTabKey,
  tabWrapperRef,
}: {
  tabs: RailElement[]
  moreOptions: { key: string; icon: string; title: string; hide?: boolean; dropdown: React.ReactNode }
  actions: Array<{ key: string; icon: string; title: string; action?: () => void; dropdown?: React.ReactNode; hide?: boolean; ref?: React.Ref<HTMLButtonElement> }>
  selectedTab: string
  isOpen: boolean
  onTabKey: (key: string) => void
  tabWrapperRef?: React.Ref<HTMLDivElement>
}) {
  const { t } = useTranslation()
  const { isHistoryView, focusMode } = useLayoutContext()

  // 2026-10-07 owner item Y: the menu bar (File/Edit/Insert/View/Format/Help) lives in the
  // rail now — a 'menu' icon + tooltip ABOVE the file tree. Implementation note (lessons
  // learned): a STATIC import of menu-bar into this rail module changed the webpack module
  // graph badly enough to break app startup, so the rail only TOGGLES a body class; the
  // ToolbarMenuBar stays mounted in mantine-toolbar.tsx (unchanged import graph) and
  // editor-v2-tokens.css floats the existing .ide-redesign-toolbar-menu-bar node above the
  // rail while .ol-v2-menubar-open is set.
  const [menuOpen, setMenuOpen] = useState(false)
  useEffect(() => {
    const el = document.body
    if (menuOpen) {
      el.classList.add('ol-v2-menubar-open')
      return () => el.classList.remove('ol-v2-menubar-open')
    }
    el.classList.remove('ol-v2-menubar-open')
    return undefined
  }, [menuOpen])
  useEffect(() => {
    if (!menuOpen) return
    const onClick = (e: MouseEvent) => {
      const tEl = e.target as HTMLElement | null
      if (tEl && (tEl.closest('.ol-v2-rail-menu-bar-entry') || tEl.closest('.ide-redesign-toolbar-menu-bar'))) return
      setMenuOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMenuOpen(false)
    }
    document.addEventListener('click', onClick)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('click', onClick)
      document.removeEventListener('keydown', onKey)
    }
  }, [menuOpen])

  // (Owner 2026-10-07, toolbar/rail strip: the keyboard-shortcuts entry and
  //  the account/actions cluster are GONE from the rail — the actions area
  //  now carries the logo/home entry, exactly the owner's markup. The
  //  shortcuts surface remains in Help → Keyboard shortcuts in the menu bar.)

  return (
    <nav
      className={classNames('ide-rail ol-v2-rail', {
        hidden: isHistoryView || focusMode,
      })}
      aria-label={t('sidebar')}
      data-ol-v2-rail=""
    >
      <div className="ide-rail-tabs-nav ol-v2-rail-nav">
        {/* Owner item Y: menu-bar entry (toggles the floating ToolbarMenuBar;
            styled/positioned in editor-v2-tokens.css) — above the file tree. */}
        <Tooltip
          label={t('menu_bar', 'Menu bar')}
          position="right"
          withArrow
          withinPortal
        >
          <ActionIcon
            variant={menuOpen ? 'filled' : 'subtle'}
            aria-label={t('menu_bar', 'Menu bar')}
            aria-expanded={menuOpen}
            aria-haspopup="menu"
            className="ol-v2-rail-menu-bar-entry"
            style={{ width: 40, height: 40, fontSize: 20 }}
            onClick={() => setMenuOpen(o => !o)}
          >
            <MaterialIcon type="menu" aria-hidden />
          </ActionIcon>
        </Tooltip>
        <div className="ide-rail-tabs-wrapper" ref={tabWrapperRef as never}>
          {tabs
            .filter(shouldIncludeElement)
            .map(tab => {
              // module-provided tab components keep the legacy rendering
              // verbatim (same as /project)
              if ((tab as { tab?: unknown }).tab) {
                const Component = (tab as { tab: React.ComponentType<Record<string, unknown>> }).tab
                return (
                  <Component
                    open={isOpen && selectedTab === tab.key}
                    key={tab.key}
                    eventKey={tab.key}
                    icon={tab.icon as never}
                    indicator={tab.indicator}
                    title={tab.title}
                    disabled={tab.disabled}
                    ref={tab.ref as never}
                  />
                )
              }
              return (
                <V2RailTabButton
                  key={tab.key}
                  tab={tab}
                  open={isOpen && selectedTab === tab.key}
                  onClick={() => onTabKey(tab.key)}
                  refEl={tab.ref as React.Ref<HTMLButtonElement> | undefined}
                />
              )
            })}
          {!moreOptions.hide && (
            <RailActionElement action={moreOptions as never} />
          )}
        </div>
                {/* AK-1 (owner 2026-10-08): the home entry retires the raw-text
            material-symbols `home` ligature AND takes the EXACT style of
            the rail's Menu-bar ActionIcon entry (same 40x40 geometry, same
            --ai-* hover contract) — the owner's exact target markup. The
            Overleaf logo is the entry icon (keeps its aria-label). */}
        <ActionIcon
          variant="subtle"
          type="button"
          className="ol-v2-rail-home-entry ol-v2-rail-menu-bar-entry"
          aria-label="OlliTeX home — go to the hub"
          style={
            Object.assign(
              {
                '--ai-bg': 'transparent',
                '--ai-hover': 'var(--mantine-color-ollitex-light-hover)',
                '--ai-color': 'var(--mantine-color-ollitex-light-color)',
                '--ai-bd': 'calc(0.0625rem * var(--mantine-scale)) solid transparent',
              },
              { width: 40, height: 40, fontSize: 20 },
            )
          }
          onClick={() => { window.location.href = '/hub#/projects' }}
        >
          <span className="ol-v2-rail-home-logo" aria-label="Overleaf logo" />
        </ActionIcon>
      </div>
    </nav>
  )
}

export default MantineRailNavChrome
