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
import { ActionIcon, Group, Tooltip } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import MaterialIcon from '@/shared/components/material-icon'
import classNames from 'classnames'
import RailActionElement from '@/features/ide-react/components/rail/rail-action-element'
import { useRailContext } from '@/features/ide-react/context/rail-context'
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

function V2RailAction({ action }: { action: { icon: string; title: string; action: () => void } }) {
  return (
    <Tooltip label={action.title} position="right" withArrow withinPortal>
      <ActionIcon
        variant="subtle"
        aria-label={action.title}
        className="ol-v2-rail-action"
        style={{ width: 40, height: 40, fontSize: 20 }}
        onClick={() => action.action()}
      >
        <MaterialIcon type={action.icon as never} aria-hidden />
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
  const { setActiveModal } = useRailContext()
  const { isHistoryView, focusMode } = useLayoutContext()

  const shortcutsAction = {
    key: 'shortcuts',
    icon: 'keyboard',
    title: t('keyboard_shortcuts', 'Keyboard shortcuts'),
    action: () => setActiveModal('keyboard-shortcuts'),
  }

  // Owner #3 (2026-09-13 editor wave): the renovated rail carries the hotkeys
  // button as a first-class action, so the legacy Help DROPDOWN is dropped here
  // (its only editor-relevant entry was "Keyboard shortcuts"). The /project
  // legacy rail keeps the Help button untouched (no standalone shortcut there).
  const visibleActions = actions.filter(a => a.key !== 'support')

  return (
    <nav
      className={classNames('ide-rail ol-v2-rail', {
        hidden: isHistoryView || focusMode,
      })}
      aria-label={t('sidebar')}
      data-ol-v2-rail=""
    >
      <div className="ide-rail-tabs-nav ol-v2-rail-nav">
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
        <Group
          as="nav"
          gap={2}
          className="ol-v2-rail-actions"
          aria-label={t('help_editor_settings', 'Help and settings')}
        >
          <V2RailAction action={shortcutsAction} />
          {visibleActions
            .filter(shouldIncludeElement)
            .map(action =>
              'dropdown' in action && action.dropdown ? (
                <span key={action.key} className="ol-v2-rail-dropdown-wrap">
                  <RailActionElement action={action as never} />
                </span>
              ) : (
                <V2RailAction key={action.key} action={action as never} />
              )
            )}
        </Group>
      </div>
    </nav>
  )
}

export default MantineRailNavChrome
