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

// -- Owner item Y (2026-10-08 final shape): the File/Edit/Insert/View/Format/
//    Help menu bar as SIX rail icons + tooltips, above the file tree. --
// Inline SVG glyphs (stroke=currentColor) -- the owner's browser renders raw
// material-symbols ligature text, so new chrome uses inline SVG.
const RAIL_MENU_PATHS: Record<string, string> = {
  // page sheet
  File: 'M6 2.5h7.2L17.5 6v15.5H6zM13 2.5V6h4.5',
  // pencil
  Edit: 'M4 20l1-4L15.5 5.5a2.1 2.1 0 013 3L8 19zM14 7l3 3',
  // plus tray (insert)
  Insert: 'M5 5h14v14H5zM12 9v6M9 12h6',
  // eye (view)
  View: 'M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12zm9.5 3a3 3 0 100-6 3 3 0 000 6z',
  // paragraph lines (format)
  Format: 'M4 6h16M4 10h16M4 14h10M4 18h7',
  // help ring
  Help: 'M12 21a9 9 0 110-18 9 9 0 010 18zM9.8 9.6a2.4 2.4 0 113.6 2.1c-.8.6-1.4 1.1-1.4 2.2M12 17.2v.2',
}

function RailMenuGlyph({ label }: { label: string }) {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <path d={RAIL_MENU_PATHS[label] || RAIL_MENU_PATHS.File} />
    </svg>
  )
}

function MenubarIcons({
  t,
  activeMenu,
  onOpen,
}: {
  t: (key: string, fallback?: string) => string
  activeMenu: string | null
  onOpen: (label: string) => void
}) {
  const menus = ['File', 'Edit', 'Insert', 'View', 'Format', 'Help']
  return (
    <div className="ol-v2-rail-menus" role="menubar" aria-label={t('menu_bar', 'Menu bar')}>
      {menus.map(label => (
        <Tooltip key={label} label={label} position="right" withArrow withinPortal>
          <ActionIcon
            variant={activeMenu === label ? 'filled' : 'subtle'}
            aria-label={label}
            aria-haspopup="menu"
            aria-expanded={activeMenu === label}
            className="ol-v2-rail-menu-entry"
            style={{ width: 40, height: 40 }}
            onClick={() => onOpen(label)}
          >
            <RailMenuGlyph label={label} />
          </ActionIcon>
        </Tooltip>
      ))}
    </div>
  )
}

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
  // Owner item Y final shape (2026-10-08): the menu bar is the SIX top menus —
  // File/Edit/Insert/View/Format/Help — as icons + tooltips above the file
  // tree. Each icon opens the matching top menu of the already-mounted
  // ToolbarMenuBar (delegated click on its own trigger — the proven Mantine
  // dropdown does the rest; no menu-bar import-graph change).
  const [activeMenu, setActiveMenu] = useState<string | null>(null)
  const closeMenuChrome = () => {
    setMenuOpen(false)
    setActiveMenu(null)
  }
  const openRailMenu = (label: string) => {
    const next = !menuOpen || activeMenu !== label
    if (!next) {
      // clicking the same icon again just closes everything (the dropdown's
      // own outside-click handler would do this too; be deterministic here)
      document.body.classList.remove('ol-v2-menubar-open')
      closeMenuChrome()
      return
    }
    setActiveMenu(label)
    setMenuOpen(true)
    document.body.classList.add('ol-v2-menubar-open')
    // double rAF: let the floating panel become visible (display:flex) first
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        const bar = document.querySelector('.ide-redesign-toolbar-menu-bar')
        if (!bar) return
        const btn = Array.from(bar.querySelectorAll('button')).find(
          b => (b.textContent || '').trim() === label
        ) as HTMLButtonElement | undefined
        if (btn) btn.click()
      })
    })
  }
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
      if (tEl && (tEl.closest('.ol-v2-rail-menu-entry') || tEl.closest('.ide-redesign-toolbar-menu-bar'))) return
      setMenuOpen(false)
      setActiveMenu(null)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setMenuOpen(false)
        setActiveMenu(null)
      }
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
        {/* Owner item Y (final form 2026-10-08): the File/Edit/Insert/View/
            Format/Help menu bar lives in the rail — six icons + tooltips,
            ABOVE the file tree. Inline SVG glyphs (the owner's browser shows
            raw material-symbols ligature text; inline SVG is the standing
            rule for new chrome). Each icon opens the matching top menu of
            the mounted ToolbarMenuBar via openRailMenu (delegated click). */}
        <MenubarIcons t={t} activeMenu={activeMenu} onOpen={openRailMenu} />
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
          aria-label="Home — go to the hub"
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
          {/* 2026-10-09 (owner): clean house icon (20px, currentColor stroke,
              same family as the other rail icons) replaces the old brand logo. */}
          <svg
            className="ol-v2-rail-home-logo"
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M3 10.5 12 3l9 7.5" />
            <path d="M5 9.5V21h5v-6h4v6h5V9.5" />
          </svg>
        </ActionIcon>
      </div>
    </nav>
  )
}

export default MantineRailNavChrome
