/**
 * rail-menus.tsx — the v2 editor's File/Edit/Insert/View/Format/Help menus,
 * anchored DIRECTLY to the rail icons (Mantine Menu, target = the icon).
 *
 * 2026-10-09 (owner item AC: "the /editor menu situation is still not fixed
 * at all — some are not working at all and some are showing the full menu
 * instead of only the relevant part (e.g. the file dropdown for file,
 * without the menu 'header' with the other Edit, Help…)"): the previous
 * mechanism (float the whole 6-item menu bar over the rail + delegated
 * proxy-click on the react-bootstrap toggles) produced state desync — the
 * react-bootstrap Toggle computes `!show` from render-time context state
 * (uncontrollable boundary + root-close mousedown races + no ESC handler),
 * so menus flakily stayed closed, re-opened, or left the WHOLE 6-header bar
 * floating with no dropdown open (the owner's "full menu" symptom).
 *
 * This module replaces that mechanism for the v2 rail:
 *   • each rail icon IS the target of its own dropdown — icon click opens
 *     ONLY that menu (no header bar — exactly what the owner asked for),
 *   • open/close/ESC/outside-click are Mantine-native (deterministic),
 *   • one-open-at-a-time is the rail's single `active` state,
 *   • item content = the SAME command registry the old menu bar used
 *     (command ids are the contract; handlers are registered by the
 *     still-mounted ToolbarMenuBar, which in v2 runs headless —
 *     registrations + modal hosts, no visible menu bar),
 *   • IMPORT-GRAPH RULE (why this file exists separately, 2026-10-07
 *     lesson): this module must NOT import `./menu-bar` (ToolbarMenuBar
 *     imports `useRailContext` from editor-v2 → a rail → menu-bar →
 *     rail-context cycle that broke app startup). Callbacks that need
 *     the rail (keyboard-shortcuts/contact-us modals) are passed as
 *     props from the rail component.
 */

import { ActionIcon, Menu } from '@mantine/core'
import { TFunction } from 'i18next'
import React, { useCallback, useEffect, useState, ReactNode } from 'react'
import {
  formatShortcut,
  useCommandRegistry,
} from '@/features/ide-react/context/command-registry-context'
import { useProjectSettingsContext } from '@/features/ide-settings/context/project-settings-context'
import { useEditorAnalytics } from '@/shared/hooks/use-editor-analytics'
import { NestableDropdownContextProvider } from '@/shared/context/nestable-dropdown-context'
import { useEditorOpenDocContext } from '@/features/ide-react/context/editor-open-doc-context'
import { getFileExtension } from '@/features/source-editor/utils/file'
import getMeta from '@/utils/meta'
import ChangeLayoutOptions from './change-layout-options'
import ReviewModeOptions from './review-mode-options'
import importOverleafModules from '../../../../../macros/import-overleaf-module.macro'

/* ------------------------------------------------------------------ *
 *  row primitives (Bootstrap-class markup — styled by the global     *
 *  Bootstrap CSS the app already loads; zero react-bootstrap imports) *
 * ------------------------------------------------------------------ */

const RailRow = ({
  title,
  onClick,
  disabled,
  leadingIcon,
  trailingIcon,
  externalHref,
  eventKey,
}: {
  title: string
  onClick?: () => void
  disabled?: boolean
  leadingIcon?: ReactNode
  trailingIcon?: ReactNode
  externalHref?: string
  eventKey?: string
}) => {
  const { sendEvent } = useEditorAnalytics()
  const handle = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation()
      if (disabled) return
      if (eventKey) {
        sendEvent('menu-bar-option-click', { key: eventKey })
      }
      if (externalHref) {
        window.open(externalHref, '_blank', 'noopener')
        return
      }
      onClick?.()
    },
    [disabled, eventKey, sendEvent, externalHref, onClick],
  )
  return (
    <button
      type="button"
      role="menuitem"
      className={
        'dropdown-item ol-v2-rail-menu-item' + (disabled ? ' disabled' : '')
      }
      disabled={disabled}
      onClick={handle}
    >
      {leadingIcon && (
        <span className="dropdown-item-leading-icon" aria-hidden="true">
          {leadingIcon}
        </span>
      )}
      <span className="ol-v2-rail-menu-item-label">{title}</span>
      {trailingIcon && (
        <span
          className="dropdown-item-trailing-icon"
          style={{ marginLeft: 'auto' }}
        >
          {trailingIcon}
        </span>
      )}
    </button>
  )
}

const RailMenuItem = ({ id }: { id: string }) => {
  const { registry, shortcuts } = useCommandRegistry()
  const cmd = registry.get(id)
  if (!cmd) {
    return null
  }
  const sc = shortcuts?.[id]
  return (
    <RailRow
      key={id}
      title={cmd.menuLabel ?? cmd.label}
      eventKey={id}
      disabled={cmd.disabled}
      leadingIcon={cmd.leadingIcon}
      externalHref={cmd.href || undefined}
      trailingIcon={
        sc ? (
          <span style={{ fontSize: 11, opacity: 0.65 }}>
            {formatShortcut(sc[0])}
          </span>
        ) : undefined
      }
      onClick={() => cmd.handler?.({ location: 'menu-bar' })}
    />
  )
}

const RailDivider = () => <div className="dropdown-divider" role="separator" />

const RailHeader = ({ children }: { children: ReactNode }) => (
  <h6 className="dropdown-header">{children}</h6>
)

/**
 * nested submenu (Download ▸, Math ▸, Figure ▸, PDF zoom ▸, AI Generate ▸)
 *
 * 2026-10-09 (owner item AC, second round — "the sub drop down section
 * are broken or empty"): the previous nested Mantine `<Menu>` inside the
 * parent Mantine `<Menu.Dropdown>` was unreliable inside the portal
 * (parent outside-click logic vs. child open, portal stacking), and the
 * Bootstrap-flyout variant rendered empty/covered in real browsers.
 * Now ALL rail nested groups expand INLINE inside the same dropdown body
 * with zero z-index/position/hover-gap failure modes.
 *
 * 2026-10-09 (owner round-3): "keep the PDF Zoom and Editing mode
 * unfolded" / "keep Math and figure always unfolded" — groups are now
 * UNFOLDED BY DEFAULT and STAY unfolded (no hover-away auto-close);
 * collapse is an explicit target click only (or the parent menu closing).
 */
const RailSubmenu = ({ label, children }: { label: ReactNode; children: ReactNode }) => {
  const [open, setOpen] = useState(true)
  return (
    <li role="none" className="ol-v2-nested-group">
      <button
        type="button"
        className="ol-v2-rail-sub-target"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(o => !o)}
      >
        <span className="ol-v2-rail-menu-item-label">{label}</span>
        <span className={`ol-v2-rail-sub-chevron${open ? ' open' : ''}`}>
          <ChevronRight />
        </span>
      </button>
      {open && <div className="ol-v2-nested-inline">{children}</div>}
    </li>
  )
}

/* ------------------------------------------------------------------ *
 *  the six menu structures — same command ids + section layout as    *
 *  the ToolbarMenuBar in menu-bar.tsx (that file is the reference)   *
 * ------------------------------------------------------------------ */

const insertMenuSections = importOverleafModules('insertMenuSections') as {
  import: {
    default: Array<{
      id: string
      title?: string | ReactNode
      children: Array<
        | string
        | { id: string; title: string | ReactNode; children: Array<string> }
      >
    }>
  }
}[]

/**
 * Module-contributed Insert sections (modules/llm "AI Generate",
 * overleaf-lab).
 *
 * Shape (MenuSectionStructure): section { id, title?, children: Entry[] }
 * with Entry = command-id string | group { id, title, children: id[] }.
 * The owner-pasted empty target on 2026-10-09 came from reading this
 * two-level structure flat (section.title was undefined → empty label,
 * section.children were group objects, not command ids → all rows null).
 *
 * LLM GATING (owner: "This should be gated by the LLM availability"): a
 * group renders only if at least one of its command ids is registered in
 * the command registry (the llm module registers llm_generate_* only when
 * the LLM feature is available) — exactly the graceful degradation the
 * v1 CommandDropdown did. Unavailable LLM → the whole group is absent,
 * never an empty target.
 */
const RailInsertModuleSections = () => {
  const { registry } = useCommandRegistry()
  const nodes: ReactNode[] = []
  for (const { import: { default: sections } } of insertMenuSections) {
    for (const section of sections) {
      if (section.title) {
        nodes.push(
          <RailHeader key={`h-${section.id}`}>{section.title}</RailHeader>
        )
      }
      for (const entry of section.children) {
        if (typeof entry === 'string') {
          if (registry.has(entry)) {
            nodes.push(<RailMenuItem key={entry} id={entry} />)
          }
          continue
        }
        const ids = (entry.children as string[]).filter(
          id => typeof id === 'string' && registry.has(id)
        )
        if (ids.length === 0) {
          continue // LLM not available → no group at all
        }
        nodes.push(
          <RailSubmenu key={entry.id} label={entry.title as ReactNode}>
            {ids.map(id => (
              <RailMenuItem key={id} id={id} />
            ))}
          </RailSubmenu>
        )
      }
    }
  }
  if (nodes.length === 0) {
    return null
  }
  return <>{nodes}</>
}

const Check = ({ on }: { on: boolean }) =>
  on ? (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m4 12.5 5 5L20 6.5" />
    </svg>
  ) : (
    <span style={{ width: 14, display: 'inline-block' }} />
  )

const ChevronRight = () => (
  <svg
    width="15"
    height="15"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden="true"
  >
    <path d="m9 5 7 7-7 7" />
  </svg>
)

const fileMenu = (t: TFunction) => (
  <>
    <RailMenuItem id="new_file" />
    <RailMenuItem id="new_folder" />
    <RailMenuItem id="upload_file" />
    <RailMenuItem id="copy_project" />
    <RailMenuItem id="rename_project" />
    <RailDivider />
    <RailMenuItem id="show_version_history" />
    <RailMenuItem id="word_count" />
    <RailDivider />
    <RailMenuItem id="submit-project" />
    <RailMenuItem id="manage-template" />
    <RailDivider />
    <RailSubmenu label={t('download')}>
      <RailMenuItem id="download-as-source-zip" />
      <RailMenuItem id="download-pdf" />
      <RailMenuItem id="export-as-docx" />
      <RailMenuItem id="export-as-markdown" />
      <RailMenuItem id="export-as-html" />
    </RailSubmenu>
    <RailMenuItem id="llm_select_model" />
    <RailDivider />
    <RailMenuItem id="open-settings" />
    <RailMenuItem id="share_project" />
  </>
)

const editMenu = (t: TFunction) => (
  <>
    <RailMenuItem id="undo" />
    <RailMenuItem id="redo" />
    <RailDivider />
    <RailMenuItem id="find" />
    <RailMenuItem id="select-all" />
  </>
)

const insertMenu = (t: TFunction) => (
  <>
    <RailSubmenu label={t('math')}>
      <RailMenuItem id="insert-inline-math" />
      <RailMenuItem id="insert-display-math" />
    </RailSubmenu>
    <RailMenuItem id="insert-symbol" />
    <RailSubmenu label={t('figure')}>
      <RailMenuItem id="insert-figure-from-computer" />
      <RailMenuItem id="insert-figure-from-project-files" />
      <RailMenuItem id="insert-figure-from-another-project" />
      <RailMenuItem id="insert-figure-from-url" />
    </RailSubmenu>
    <RailMenuItem id="insert-table" />
    <RailMenuItem id="insert-citation" />
    <RailMenuItem id="insert-link" />
    <RailMenuItem id="insert-cross-reference" />
    <RailDivider />
    <RailMenuItem id="comment" />
    {/* 2026-10-09 (owner round-3): the module-contributed Insert sections
        (modules/llm "AI Generate") render through a registry-gated
        component: groups whose command ids are NOT registered (LLM not
        available) render NOTHING — the previous flat mapping rendered an
        empty-label target (owner-pasted DOM: empty .ol-v2-rail-menu-
        item-label), because it treated the two-level section→group shape
        as flat and took title/children off the section. */}
    <RailInsertModuleSections />
  </>
)

const viewMenu = (t: TFunction) => {
  const { mathPreview, setMathPreview, breadcrumbs, setBreadcrumbs, editorTabs, setEditorTabs } =
    useProjectSettingsContext()
  return (
    <>
      <ChangeLayoutOptions />
      <ReviewModeOptions />
      <RailDivider />
      <RailHeader>{t('editor_settings')}</RailHeader>
      <RailRow
        title={t('show_breadcrumbs')}
        leadingIcon={<Check on={!!breadcrumbs} />}
        onClick={() => setBreadcrumbs(!breadcrumbs)}
        eventKey="show_breadcrumbs"
      />
      <RailRow
        title={t('show_editor_tabs')}
        leadingIcon={<Check on={!!editorTabs} />}
        onClick={() => setEditorTabs(!editorTabs)}
        eventKey="show_editor_tabs"
      />
      <RailRow
        title={t('show_equation_preview')}
        leadingIcon={<Check on={!!mathPreview} />}
        onClick={() => setMathPreview(!mathPreview)}
        eventKey="show_equation_preview"
      />
      <RailDivider />
      <RailMenuItem id="command-palette" />
      <RailHeader>{t('pdf_preview')}</RailHeader>
      <RailMenuItem id="view-pdf-presentation-mode" />
      <RailSubmenu label={t('pdf_zoom')}>
        <RailMenuItem id="view-pdf-zoom-in" />
        <RailMenuItem id="view-pdf-zoom-out" />
        <RailMenuItem id="view-pdf-fit-width" />
        <RailMenuItem id="view-pdf-fit-height" />
      </RailSubmenu>
    </>
  )
}

const formatMenu = (t: TFunction) => (
  <>
    <RailMenuItem id="format-bold" />
    <RailMenuItem id="format-italics" />
    <RailDivider />
    <RailMenuItem id="format-bullet-list" />
    <RailMenuItem id="format-numbered-list" />
    <RailMenuItem id="format-increase-indentation" />
    <RailMenuItem id="format-decrease-indentation" />
    <RailDivider />
    <RailHeader>{t('paragraph_styles')}</RailHeader>
    <RailMenuItem id="format-style-normal" />
    <RailMenuItem id="format-style-section" />
    <RailMenuItem id="format-style-subsection" />
    <RailMenuItem id="format-style-subsubsection" />
    <RailMenuItem id="format-style-paragraph" />
    <RailMenuItem id="format-style-subparagraph" />
  </>
)

export type RailMenuHelpers = {
  onKeyboardShortcuts?: () => void
  onContactUs?: () => void
}

const helpMenu = (t: TFunction, helpers: RailMenuHelpers) => {
  const showDocumentation = getMeta('ol-wikiEnabled')
  const showSupport = getMeta('ol-showSupport')
  return (
    <>
      <RailRow
        title={t('keyboard_shortcuts')}
        onClick={() => helpers.onKeyboardShortcuts?.()}
        eventKey="keyboard_shortcuts"
      />
      {showDocumentation && (
        <RailRow
          title={t('documentation')}
          externalHref="/learn"
          eventKey="documentation"
        />
      )}
      {showSupport && (
        <>
          <RailDivider />
          <RailRow
            title={t('contact_us')}
            onClick={() => helpers.onContactUs?.()}
            eventKey="contact_us"
          />
        </>
      )}
    </>
  )
}

/* ------------------------------------------------------------------ *
 *  the anchored menu + the six menus (top cluster + Help bottom)     *
 * ------------------------------------------------------------------ */

export const RailMenuDropdown = ({
  label,
  t,
  glyph,
  opened,
  onActiveChange,
  content,
  disabled = false,
}: {
  label: string
  t: TFunction
  glyph: ReactNode
  opened: boolean
  onActiveChange: (active: string | null) => void
  content: ReactNode
  /** 2026-10-09 (owner round-3): View/Insert are disabled for non
      tex/typst files — render a dimmed, non-interactive icon (the v1
      menu bar behaves the same for text-dependent menus). */
  disabled?: boolean
}) => {
  if (disabled) {
    return (
      <ActionIcon
        variant="subtle"
        className="ol-v2-rail-menu-entry ol-v2-rail-menu-entry-disabled"
        style={{ width: 40, height: 40, opacity: 0.35, cursor: 'default' }}
        aria-label={`${label} (disabled — available for .tex/.typst files only)`}
        aria-disabled="true"
        title="Available for .tex / .typst files only"
        tabIndex={-1}
      >
        {glyph}
      </ActionIcon>
    )
  }
  return (
  <Menu
    opened={opened}
    onOpen={() => onActiveChange(label)}
    onClose={() => onActiveChange(null)}
    position="bottom-start"
    withinPortal
    width={250}
  >
    <Menu.Target>
      <ActionIcon
        variant={opened ? 'filled' : 'subtle'}
        className="ol-v2-rail-menu-entry"
        style={{ width: 40, height: 40 }}
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={opened}
      >
        {glyph}
      </ActionIcon>
    </Menu.Target>
    <Menu.Dropdown className="ol-v2-rail-menu-panel">
      {/* MenuBarOption inside ChangeLayoutOptions/ReviewModeOptions needs
          this context (it throws otherwise). id is unique per menu. */}
      <NestableDropdownContextProvider
        id={'rail-menu-' + label.toLowerCase()}
        inline
      >
        {content}
      </NestableDropdownContextProvider>
    </Menu.Dropdown>
  </Menu>
  )
}

/**
 * RailMenuCluster — one cluster of anchored menus (top cluster:
 * File/Edit/Insert/View/Format; bottom cluster: Help beside home, per
 * owner item a39c8a3a). Open state is OWNED BY THE RAIL (shared `active`)
 * so that opening Help closes File automatically (one open at a time).
 */
/**
 * owner 2026-10-09 round-3: "Menu View and Insert needs to be correctly
 * disabled for non tex/typst files" — the active document's extension
 * (chrome-level EditorOpenDocContext) decides: View + Insert are usable
 * only for .tex / .typst (and .latex) source files.
 */
const TEXT_SOURCE_EXTENSIONS = new Set(['tex', 'typst', 'latex'])

const useActiveDocExtension = (): string | null => {
  const { openDocName } = useEditorOpenDocContext()
  const ext = getFileExtension(openDocName || '')
  return ext ? ext.toLowerCase() : null
}

export const RailMenuCluster = ({
  t,
  glyphFor,
  helpers,
  labels,
  active,
  onActiveChange,
}: {
  t: TFunction
  glyphFor: (label: string) => ReactNode
  helpers: RailMenuHelpers
  labels: string[]
  active: string | null
  onActiveChange: (active: string | null) => void
}) => {
  const activeExt = useActiveDocExtension()
  const isMenuEnabled = (label: string) => {
    if (label !== 'View' && label !== 'Insert') {
      return true
    }
    return !!activeExt && TEXT_SOURCE_EXTENSIONS.has(activeExt)
  }
  // If the active file switches away from tex/typst while View/Insert is
  // open, close it (stale open state = owner-visible inconsistency).
  useEffect(() => {
    if (active && !isMenuEnabled(active)) {
      onActiveChange(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeExt, active])
  const contentFor = (label: string): ReactNode => {
    switch (label) {
      case 'File':
        return fileMenu(t)
      case 'Edit':
        return editMenu(t)
      case 'Insert':
        return insertMenu(t)
      case 'View':
        return viewMenu(t)
      case 'Format':
        return formatMenu(t)
      case 'Help':
        return helpMenu(t, helpers)
      default:
        return null
    }
  }
  return (
    <div
      className="ol-v2-rail-menus"
      role="menubar"
      aria-label={t('menu_bar', 'Menu bar')}
    >
      {labels.map(label => (
        <RailMenuDropdown
          key={label}
          label={label}
          t={t}
          glyph={glyphFor(label)}
          opened={active === label}
          onActiveChange={onActiveChange}
          content={contentFor(label)}
          disabled={!isMenuEnabled(label)}
        />
      ))}
    </div>
  )
}
