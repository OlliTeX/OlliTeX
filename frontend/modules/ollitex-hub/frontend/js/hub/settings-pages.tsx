import React, { useEffect, useMemo, useState } from 'react'
import { HUB_NAV, HubNode, visibleNav, indexNav } from './nav-tree'
import { renderLeaf } from './leaves'
import { SettingsShell, SettingsSection, SettingsNavGroup } from './settings-shell'
import { UserProvider } from '../../../../../js/shared/context/user-context'
import { SSOProvider } from '../../../../../js/features/settings/context/sso-context'
import { UserSettingsProvider } from '../../../../../js/shared/context/user-settings-context'
import { SplitTestProvider } from '../../../../../js/shared/context/split-test-context'
import {
  buildThemePatch,
  cssVarsFor,
  getAppliedHubTheme,
  onAppliedHubThemeChange,
} from './hub-theme'
import {
  currentColorScheme,
  onColorSchemeChange,
} from '../../../../../js/shared/mantine/overall-theme'
import OlliTProvider from '../../../../../js/shared/mantine/provider'
import { startErrorCollector } from '../shared/error-collector'

/** the settings routes inject the same `ol-hub-admin` meta as /hub (Go
 * hub.go). Boolean meta contract (Node oracle, byte-pinned): TRUE = the
 * `content` attribute is PRESENT (value empty); FALSE = attribute absent.
 * So the hub-root `getMeta(...) === true` check is the canonical read. */
function useAdminFlag(): boolean {
  const [isAdmin, setAdmin] = useState(false)
  useEffect(() => {
    try {
      const m = document.querySelector('meta[name="ol-hub-admin"]')
      setAdmin(!!m && m.hasAttribute('content'))
    } catch {
      // jsdom / no meta
    }
  }, [])
  return isAdmin
}

/** provider wrapper mirroring hub-root's (theme patch + shared contexts). */
function SettingsProviders({ children }: { children: React.ReactNode }) {
  const [appliedTheme, setAppliedThemeState] = useState(() => getAppliedHubTheme())
  const [scheme, setScheme] = useState(() => {
    try {
      return currentColorScheme()
    } catch {
      return 'dark'
    }
  })
  useEffect(() => onAppliedHubThemeChange(t => setAppliedThemeState(t)), [])
  useEffect(() => {
    try {
      return onColorSchemeChange(() => {
        try {
          setScheme(currentColorScheme())
        } catch {
          // jsdom
        }
      })
    } catch {
      return undefined
    }
  }, [])
  useEffect(() => {
    startErrorCollector()
  }, [])
  const themePatch = useMemo(() => buildThemePatch(appliedTheme), [appliedTheme])
  const cssVars = useMemo(() => cssVarsFor(appliedTheme, scheme === 'dark'), [appliedTheme, scheme])
  return (
    <OlliTProvider themePatch={themePatch}>
      <SplitTestProvider>
        <UserSettingsProvider>
          <div
            style={
              {
                width: '100%',
                minHeight: '100vh',
                background: 'var(--mantine-color-body)',
                color: 'var(--mantine-color-text)',
                ...cssVars,
              } as React.CSSProperties
            }
          >
            {children}
          </div>
        </UserSettingsProvider>
      </SplitTestProvider>
    </OlliTProvider>
  )
}

/** flatten all leaves under the given parent ids (order preserved). */
function sectionsOf(parentIds: string[], isAdmin: boolean, skipIds: string[] = []): HubNode[] {
  const nav = visibleNav(HUB_NAV, isAdmin)
  const out: HubNode[] = []
  const skip = new Set(skipIds)
  const walk = (ns: HubNode[], inTarget: boolean) => {
    for (const n of ns) {
      const target = parentIds.includes(n.id) || inTarget
      if (!n.children || n.children.length === 0) {
        if (target && !skip.has(n.id)) out.push(n)
        continue
      }
      walk(n.children, target)
    }
  }
  walk(nav, false)
  return out
}

function byId(id: string, isAdmin: boolean): HubNode | null {
  const idx = indexNav(visibleNav(HUB_NAV, isAdmin))
  if (!id) return null
  return idx.byId.get(id) || idx.allLeaves.get(id) || null
}

const Section = ({ node, children }: { node: HubNode; children: React.ReactNode }) => (
  <UserProvider>
    <SSOProvider>{children}</SSOProvider>
  </UserProvider>
)

/* ────────────────────────────────────────────────────────────────────────────
 * AK/AJ (owner 2026-10-08): settings are now INDIVIDUAL PAGES (AJ-1).
 *   /user-settings            → landing grid (no long scroll, no subtitle)
 *   /user-settings/<id>       → ONE section, its own page
 *   /admin-settings           → landing grid
 *   /admin-settings/<id>      → ONE section, its own page
 * AJ-4: the env-owned sections are retired from the admin surface:
 *   site.services.services · site.services.grammar · site.storage.* ·
 *   site.compilation.typst · site.compilation.pandoc · site.compilation.git ·
 *   site.general.enclose ("Full site settings" card — retired).
 * AJ-5#1: admin nav re-grouped meaningfully (see ADMIN_GROUPS below).
 * ──────────────────────────────────────────────────────────────────────────── */

const USER_BASE = '/user-settings'
const ADMIN_BASE = '/admin-settings'

/** AJ-4: retired admin sections (env-owned — see toolkit/env.example). */
const ADMIN_RETIRED = new Set([
  'site.services.services',   // companion service addresses (env)
  'site.services.grammar',    // LanguageTool server URL (env)
  'site.storage.local',       // SeaweedFS endpoints/keys (env)
  'site.compilation.typst',   // clsi_typst URL + fixed on (env/off)
  'site.compilation.pandoc',  // fixed on
  'site.compilation.git',     // git bridge host/port (env)
  'site.general.enclose',     // "Full site settings" card (retired per owner)
])

/** AJ-5#1: the admin sections, meaningful groups. Order = sidebar order.
 * GitHub sync / WebDAV / Dropbox move to Integrations (owner: group them
 * meaningfully); Active projects moves with the project surfaces. */
const ADMIN_GROUPS: [string, (id: string) => boolean][] = [
  ['Overview', id =>
    id === 'overview' ||
    id === 'site.general.health' ||
    id === 'site.general.stats' ||
    id === 'site.general.misc' ||
    id === 'site.general.appearance' ||
    id === 'site.general.emailtemplates' ||
    id === 'site.general.signup'],
  ['Content & templates', id =>
    id === 'site.general.managetpl' ||
    id === 'site.general.messages' ||
    id === 'site.general.activeprojects' ||
    id === 'site.general.editor'],
  ['Projects', id => /^site\.general\.projects\./.test(id)],
  ['Users', id => /^site\.general\.users\./.test(id)],
  ['Integrations & SSO', id =>
    /^site\.integrations\./.test(id) ||
    id === 'site.compilation.github' ||
    id === 'site.compilation.webdav' ||
    id === 'site.compilation.dropbox'],
  ['Compilation', id =>
    id === 'site.compilation.sandboxed' ||
    id === 'site.compilation.linkedfiletypes'],
  ['Identity & branding', id =>
    id === 'site.services.email' ||
    id === 'site.services.branding'],
  ['LLM', id => /^site\.llm\./.test(id)],
]

function userSectionIds(isAdmin: boolean): string[] {
  return sectionsOf(['mysettings'], isAdmin).map(n => n.id)
}

function adminSectionIds(isAdmin: boolean): string[] {
  const all = [
    ...sectionsOf(['overview'], isAdmin),
    ...sectionsOf(['site'], isAdmin),
  ].filter((n, i, arr) => arr.findIndex(x => x.id === n.id) === i)
  return all.map(n => n.id).filter(id => !ADMIN_RETIRED.has(id))
}

function userNav(): SettingsNavGroup[] {
  const ids = userSectionIds(true)
  const entry = (id: string) => {
    const n = byId(id, true)
    return { id, label: n?.label || id, icon: n?.icon || 'settings' }
  }
  return [
    {
      group: 'Account',
      entries: ids.filter(id => !id.startsWith('mysettings.llm')).map(entry),
    },
    {
      group: 'LLM',
      entries: ids.filter(id => id.startsWith('mysettings.llm.')).map(entry),
    },
  ].filter(g => g.entries.length > 0)
}

function adminNav(isAdmin: boolean): SettingsNavGroup[] {
  return ADMIN_GROUPS
    .filter(([group]) => !(group === 'LLM' && !isAdmin))
    .map(([group, match]) => ({
      group,
      entries: adminSectionIds(isAdmin)
        .filter(match)
        .map(id => {
          const n = byId(id, isAdmin)
          return { id, label: n?.label || id, icon: n?.icon || 'settings' }
        }),
    }))
    .filter(g => g.entries.length > 0)
}

/** small inline SVG arrow for the landing cards (no icon-font dependency). */
function CardArrow() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" style={{ flexShrink: 0 }}>
      <path d="M5 12h14M13 6l6 6-6 6" />
    </svg>
  )
}

function CardIcon({ name }: { name: string }) {
  // deterministic inline-SVG glyph per icon name (the owner's browser renders
  // material-symbols ligatures as raw text — SVG renders everywhere)
  const paths: Record<string, string> = {
    person: 'M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z M4 20c0-4 3.6-6 8-6s8 2 8 6',
    account_tree: 'M12 8a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z M5 20c0-3.5 3-5.5 7-5.5s7 2 7 5.5',
    person_add: 'M10 11a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z M3 19c0-3 3-5 7-5 M20 8v6 M17 11h6',
    settings: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z M19 12l1.5-1 1.5 2.5-2 .8 M4.5 9.5 6 8.6 7.5 11 M12 3v2 M12 19v2 M5 20l1.5-1 M19 4 17.5 5',
    bolt: 'M13 3 5 13h5l-1 8 8-10h-5l1-8Z',
    chat: 'M4 5h16v11H9l-5 4V5Z',
    memory: 'M8 5h8v14H8V5Z M10 8h4 M10 12h4',
    monitoring: 'M4 5h16v10H4V5Z M8 20l4-3 4 3',
    auto_awesome: 'M12 3l1.8 4.8L18 9.5l-4.2 1.7L12 16l-1.8-4.8L6 9.5l4.2-1.7L12 3Z',
    link: 'M9 15l6-6 M8.5 12.5 7 14a3.5 3.5 0 0 0 5 5l1.5-1.5 M15.5 11.5 17 10a3.5 3.5 0 0 0-5-5L10.5 6.5',
    tune: 'M5 8h9 M18 8h1 M5 16h1 M10 16h9 M15 6a2 2 0 1 0 0 4 2 2 0 0 0 0-4Z M9 14a2 2 0 1 0 0 4 2 2 0 0 0 0-4Z',
  }
  return (
    <span
      style={{
        width: 22,
        height: 22,
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        borderRadius: 6,
        background: 'var(--mantine-color-blue-light)',
        color: 'var(--mantine-color-blue)',
        flexShrink: 0,
      }}
    >
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d={paths[name] || paths.settings} />
      </svg>
    </span>
  )
}

/** landing grid — one card per section; clicking goes to its page. */
function LandingGrid({
  base,
  title,
  groups,
  isAdmin,
}: {
  base: string
  title: string
  groups: { group: string; ids: string[] }[]
  isAdmin: boolean
}) {
  return (
    <SettingsShell
      title={title}
      nav={groups.map(g => ({
        group: g.group,
        entries: g.ids.map(id => {
          const n = byId(id, isAdmin)
          return { id, label: n?.label || id, icon: n?.icon || 'settings' }
        }),
      }))}
      basePath={base}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        {groups.map(g => (
          <div key={g.group}>
            <div
              style={{
                fontSize: 11,
                fontWeight: 700,
                letterSpacing: '0.08em',
                textTransform: 'uppercase',
                color: 'var(--mantine-color-dimmed)',
                margin: '0 0 6px 2px',
                lineHeight: 1.15, // 2026-10-09 (owner item C): tight labels
              }}
            >
              {g.group}
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))', gap: 10 }}>
              {g.ids.map(id => {
                const n = byId(id, isAdmin)
                return (
                  <a
                    key={id}
                    href={`${base}/${id}`}
                    data-settings-card={id}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 10,
                      padding: '10px 12px',
                      border: '1px solid var(--mantine-color-border)',
                      borderRadius: 10,
                      background: 'var(--mantine-color-body)',
                      color: 'inherit',
                      textDecoration: 'none',
                    }}
                    onMouseEnter={e => (e.currentTarget.style.borderColor = 'var(--mantine-color-blue)')}
                    onMouseLeave={e => (e.currentTarget.style.borderColor = 'var(--mantine-color-border)')}
                  >
                    <CardIcon name={n?.icon || 'settings'} />
                    <span style={{ fontSize: 14, fontWeight: 500, flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {n?.label || id}
                    </span>
                    <span style={{ color: 'var(--mantine-color-dimmed)' }}>
                      <CardArrow />
                    </span>
                  </a>
                )
              })}
            </div>
          </div>
        ))}
      </div>
    </SettingsShell>
  )
}

/** one section, one page (AJ-1). */
function SectionPage({
  base,
  title,
  activeId,
  sections,
  navGroups,
  isAdmin,
}: {
  base: string
  title: string
  activeId: string
  sections: HubNode[]
  navGroups: SettingsNavGroup[]
  isAdmin: boolean
}) {
  const node = sections.find(n => n.id === activeId)
  if (!node) {
    return (
      <SettingsProviders>
        <SettingsShell title={title} nav={navGroups} basePath={base} activeId={activeId} backHref={base}>
          <div style={{ padding: '40px 8px' }}>
            <div style={{ fontSize: 18, fontWeight: 700, marginBottom: 8 }}>Section not found</div>
            <div style={{ color: 'var(--mantine-color-dimmed)', marginBottom: 16 }}>
              “{activeId}” is not a section on this page.
            </div>
            <a href={base} style={{ color: 'var(--mantine-color-blue)' }}>← Back to {title}</a>
          </div>
        </SettingsShell>
      </SettingsProviders>
    )
  }
  return (
    <SettingsProviders>
      <SettingsShell title={title} nav={navGroups} basePath={base} activeId={activeId} backHref={base} backLabel={title}>
        <SettingsSection id={node.id} node={node}>
          <Section node={node}>{renderLeaf(node)}</Section>
        </SettingsSection>
      </SettingsShell>
    </SettingsProviders>
  )
}

export function UserSettingsLanding() {
  const navg = userNav()
  const groups = navg.map(g => ({ group: g.group, ids: g.entries.map(e => e.id) }))
  return (
    <SettingsProviders>
      <LandingGrid base={USER_BASE} title="User settings" groups={groups} isAdmin={false} />
    </SettingsProviders>
  )
}

export function UserSettingsSection({ id }: { id: string }) {
  const navGroups = userNav()
  return (
    <SectionPage
      base={USER_BASE}
      title="User settings"
      activeId={id}
      sections={sectionsOf(['mysettings'], true)}
      navGroups={navGroups}
      isAdmin={false}
    />
  )
}

export function AdminSettingsLanding() {
  const isAdmin = useAdminFlag()
  if (!isAdmin) {
    return <Restricted />
  }
  const navg = adminNav(isAdmin)
  const groups = navg.map(g => ({ group: g.group, ids: g.entries.map(e => e.id) }))
  return (
    <SettingsProviders>
      <LandingGrid base={ADMIN_BASE} title="Site settings" groups={groups} isAdmin={isAdmin} />
    </SettingsProviders>
  )
}

export function AdminSettingsSection({ id }: { id: string }) {
  const isAdmin = useAdminFlag()
  if (!isAdmin) {
    return <Restricted />
  }
  const navGroups = adminNav(isAdmin)
  return (
    <SectionPage
      base={ADMIN_BASE}
      title="Site settings"
      activeId={id}
      sections={sectionsOf(['overview'], true).concat(sectionsOf(['site'], true))}
      navGroups={navGroups}
      isAdmin={isAdmin}
    />
  )
}

function Restricted() {
  return (
    <SettingsProviders>
      <div data-testid="admin-settings-restricted" style={{ padding: '96px 24px', maxWidth: 720, margin: '0 auto' }}>
        <h2>Site settings are restricted</h2>
        <p style={{ color: 'var(--mantine-color-dimmed)' }}>
          This page is for site administrators.
        </p>
        <a href="/hub">← Back to hub</a>
      </div>
    </SettingsProviders>
  )
}

/* ── back-compat: the old one-page exports (retained for any residual
 * import; they now render the landing grid — no long scroll). ── */
export function UserSettingsPage() {
  return <UserSettingsLanding />
}
export function AdminSettingsPage() {
  return <AdminSettingsLanding />
}
