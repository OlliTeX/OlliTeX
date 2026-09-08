import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Group, Text, Title, Anchor } from '@mantine/core'
import { getJSON } from '@/infrastructure/fetch-json'
import getMeta from '@/utils/meta'
import Icon from '../shared/icons'
import ThemeToggle from '../shared/theme-toggle'
import Rail from './rail'
import SectionBoundary from './section-boundary'
import { UserProvider } from '../../../../../frontend/js/shared/context/user-context'
import { SSOProvider } from '../../../../../frontend/js/features/settings/context/sso-context'
import { renderLeaf } from './leaves'
import { HUB_NAV, HubNode, indexNav, visibleNav } from './nav-tree'
import { accordionState } from './accordion-state'
import { startErrorCollector } from '../shared/error-collector'
import { onHubNavigate, SHORTCUT_ALIASES } from './navigate'
import {
  buildThemePatch,
  cssVarsFor,
  getAppliedHubTheme,
  onAppliedHubThemeChange,
} from './hub-theme'
import OlliTProvider from '../../../../../frontend/js/shared/mantine/provider'
import { UserSettingsProvider } from '../../../../../frontend/js/shared/context/user-settings-context'
import { SplitTestProvider } from '../../../../../frontend/js/shared/context/split-test-context'
import {
  currentColorScheme,
  onColorSchemeChange,
} from '../../../../../frontend/js/shared/mantine/overall-theme'

function parseHash(): string {
  try {
    // #8: the hash may carry leaf-state params (`#/a?q=x`) — only the id
    // before `?` is a navigational key; strip the query part here so every
    // consumer (selection, reconciler, titles) sees the leaf id alone.
    return (window.location.hash || '').replace(/^#\/?/, '').split('?', 2)[0]
  } catch {
    return ''
  }
}

function isAdminUser(): boolean {
  try {
    const m = (getMeta as any)('ol-hub-admin')
    return m === true || m === 'true'
  } catch {
    return false
  }
}

/** Standalone classic pages for the header "Open full page" link (kept for
 * templates only — projects/library links removed per owner review 2026-09-07). */
const STANDALONE: Record<string, { href: string; label: string }> = {
  'templates.all': { href: '/templates', label: 'Full template gallery' },
  'templates.*': { href: '/templates', label: 'Full template gallery' },
}

const TITLES: Record<string, { title: string; subtitle?: string }> = {
  'projects.all': { title: 'Projects', subtitle: 'Everything you work on — create, open, and manage projects.' },
  'projects.owned': { title: 'My projects', subtitle: 'Projects you own.' },
  'projects.shared': { title: 'Shared with you', subtitle: 'Projects others have shared with you.' },
  'projects.archived': { title: 'Archived projects', subtitle: 'Projects you have archived.' },
  'projects.tags.tags': { title: 'Organize Tags', subtitle: 'Your project tags — create, pick, and clear.' },
  'projects.tags.new': { title: 'New tag', subtitle: 'Create a tag for your projects.' },
  'templates.all': { title: 'Templates', subtitle: 'Start a new project from a shared template.' },
  library: { title: 'Reference library', subtitle: 'Your personal bibliography, citable from any project.' },
  overview: { title: 'Overview & activity', subtitle: 'Instance health, storage, and recent activity.' },
  'site.general.health': { title: 'Hub health', subtitle: 'Live diagnostics: server core, endpoint probes, and captured client errors.' },
}

export default function HubRoot() {
  const admin = useMemo(() => isAdminUser(), [])
  const [cats, setCats] = useState<HubNode[] | null>(null)

  // overleaf-lab #14 (2026-09-08): start the runtime error ring buffer once,
  // at hub root, so the Hub health leaf can surface anything that escaped the
  // sections (the PG-TO-1 class of crash included).
  useEffect(() => {
    startErrorCollector()
  }, [])
  const [path, setPath] = useState<string>(() => parseHash())

  // live template categories (nav_structure.md §2: Templates accordion)
  useEffect(() => {
    let alive = true
    getJSON('/api/template/categories')
      .then((data: any) => {
        if (!alive) return
        const list = Array.isArray(data) ? data : Array.isArray(data?.categories) ? data.categories : []
        const nodes: HubNode[] = [
          { id: 'templates.all', label: 'All templates', icon: 'layers', render: 'template-cat', category: 'none' },
        ]
        list
          .filter((c: any) => c && typeof c.key === 'string' && c.key !== 'all' && c.key !== 'none')
          .forEach((c: any) =>
            nodes.push({
              id: `templates.${c.key}`,
              label: c.name || c.key,
              icon: 'auto_stories',
              render: 'template-cat',
              category: c.key,
            })
          )
        setCats(nodes)
      })
      .catch(() => {
        if (alive) setCats([])
      })
    return () => {
      alive = false
    }
  }, [])

  const nav = useMemo<HubNode[]>(() => {
    const base = visibleNav(HUB_NAV, admin)
    if (!cats) return base
    const merge = (ns: HubNode[]): HubNode[] =>
      ns.map(n => {
        if (n.id === 'templates') return { ...n, children: cats }
        const c = { ...n }
        if (c.children?.length) c.children = merge(c.children)
        return c
      })
    return merge(base)
  }, [cats, admin])

  const idx = useMemo(() => indexNav(nav), [nav])

  const select = useCallback(
    (id: string) => {
      // folder ids resolve to their first leaf, so deep links and cross-section
      // navigation (Overview shortcuts) always land on renderable content
      let leaf = id
      if (!idx.allLeaves.has(id)) {
        const start = idx.byId.get(id) || (idx.ancestors.get(id) ? null : null)
        const findFirstLeaf = (n: HubNode | undefined | null): string | null => {
          if (!n) return null
          if (!n.children || n.children.length === 0) return n.id
          for (const c of n.children) {
            const f = findFirstLeaf(c)
            if (f) return f
          }
          return null
        }
        const f = findFirstLeaf(start || idx.byId.get(id))
        if (f) leaf = f
      }
      setPath(leaf)
      try {
        // #1: guarded pushState — rail clicks create a normal history entry
        // (back/forward finally works), but we never re-push when we are
        // already there (popstate / back-forward must not spam history).
        if (parseHash() !== leaf) {
          window.history.pushState(null, '', `#/${leaf}`)
        }
      } catch {
        // tests without history
      }
      const anc = idx.ancestors.get(leaf) || []
      accordionState.openChain(anc)
    },
    [idx]
  )

  // initial selection: valid leaf from hash, else the landing default
  const landing = admin ? 'overview' : 'projects.all'
  const initial = useMemo(() => {
    const h = parseHash()
    if (!h) return landing
    // exact leaf
    if (idx.allLeaves.has(h)) return h
    // folder → first leaf in order
    const arr = idx.byPrefix.get(h)
    if (arr && arr.length > 0) {
      const first = arr[arr.length - 1]
      const findFirstLeaf = (n: HubNode): string | null => {
        if (!n.children || n.children.length === 0) return n.id
        for (const c of n.children) {
          const f = findFirstLeaf(c)
          if (f) return f
        }
        return null
      }
      const f = findFirstLeaf(first)
      if (f) return f
    }
    return h
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [idx, landing])

  useEffect(() => {
    select(initial)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initial])

  const node = idx.byId.get(path) || idx.allLeaves.get(path) || null
  const title = TITLES[path]?.title || node?.label || 'LibreLeaf hub'
  const subtitle = TITLES[path]?.subtitle

  // cross-section navigation (Overview shortcuts, owner review #2): the
  // shortcut ids are short aliases mapped to real hub leaves
  useEffect(() => onHubNavigate(id => select(SHORTCUT_ALIASES[id] || id)), [select])

  // browser back/forward + in-page hash fragments (e.g. /hub → /hub#/x)
  useEffect(() => {
    const onHash = () => {
      const h = parseHash()
      if (h && (idx.allLeaves.has(h) || idx.byId.has(h))) select(h)
    }
    const onPop = () => {
      // #1: back/forward restore a hash entry without firing 'hashchange'
      // — select is idempotent (guarded pushState, no loop).
      const h = parseHash()
      if (h && (idx.allLeaves.has(h) || idx.byId.has(h))) select(h)
    }
    window.addEventListener('hashchange', onHash)
    window.addEventListener('popstate', onPop)
    return () => {
      window.removeEventListener('hashchange', onHash)
      window.removeEventListener('popstate', onPop)
    }
  }, [select, idx])

  // #1 (2026-09-08): deterministic reconciler — the hash is the source of
  // truth. Same-document hash changes can race React state (rail click vs
  // programmatic navigation, harness gotos, address-bar edits, template
  // category swap-in). After every path change (and on focus), if the
  // rendered leaf and the hash disagree, the hash wins. Bounded: each
  // reconcile runs at most once per path change and only mutates state when
  // the two actually disagree.
  useEffect(() => {
    if (typeof window === 'undefined') return undefined
    const timers: number[] = []
    const reconcile = () => {
      const h = parseHash()
      if (!h || h === path) return
      if (idx.allLeaves.has(h) || idx.byId.has(h)) select(h)
    }
    timers.push(window.setTimeout(reconcile, 0))
    timers.push(window.setTimeout(reconcile, 400))
    window.addEventListener('focus', reconcile)
    return () => {
      timers.forEach(t => window.clearTimeout(t))
      window.removeEventListener('focus', reconcile)
    }
  }, [path, idx, select])

  // app logo (owner #1): /logo_full.svg via navbar meta, with a safe default
  const logoSrc = useMemo(() => {
    try {
      const nb = (getMeta as any)('ol-navbar')
      return (nb && (nb.customLogo || nb.customLogoDark)) || '/logo_full.svg'
    } catch {
      return '/logo_full.svg'
    }
  }, [])

  // M2.5 Appearance: custom instance theme (hub-theme.ts) → provider patch
  // + per-scheme CSS variables on the hub wrapper (live on Apply).
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
  const themePatch = useMemo(() => buildThemePatch(appliedTheme), [appliedTheme])
  const cssVars = useMemo(() => cssVarsFor(appliedTheme, scheme === 'dark'), [appliedTheme, scheme])

  const standalone =
    STANDALONE[path] ||
    (node && node.id.startsWith('templates.')
      ? { href: '/templates', label: 'Full template gallery' }
      : undefined)

  return (
    <OlliTProvider themePatch={themePatch}>
      <SplitTestProvider>
        <UserSettingsProvider>
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100vh',
        overflow: 'hidden',
        background: 'var(--mantine-color-body)',
        color: 'var(--mantine-color-text)',
        ...(cssVars as React.CSSProperties),
      }}
    >
      <header
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 16,
          padding: '0 20px',
          height: 64,
          flexShrink: 0,
          background: 'var(--mantine-color-body)',
          borderBottom: '1px solid var(--mantine-color-border)',
          position: 'sticky',
          top: 0,
          zIndex: 100,
        }}
      >
        <a href="/" style={{ display: 'inline-flex', alignItems: 'center' }}>
          <img
            src={logoSrc}
            alt="LibreLeaf"
            style={{ height: 36, width: 'auto', display: 'block' }}
          />
        </a>
        <Group gap="sm" wrap="nowrap">
          {standalone ? (
            <Anchor href={standalone.href} target="_blank" rel="noreferrer" size="sm" style={{ textDecoration: 'none' }}>
              <Group gap={6}>
                <Icon name="open_in_new" size={16} />
                {standalone.label}
              </Group>
            </Anchor>
          ) : null}
          <ThemeToggle />
        </Group>
      </header>

      <div style={{ display: 'flex', flex: 1, height: '100%', minHeight: 0 }}>
        <nav
          aria-label="Primary"
          className="ol-hub-scroll"
          style={{
            width: 260,
            flexShrink: 0,
            height: '100%',
            overflowY: 'auto',
            overscrollBehavior: 'contain',
            background: 'var(--mantine-color-body)',
            borderRight: '1px solid var(--mantine-color-border)',
            padding: '12px 10px',
          }}
        >
          <Rail nav={nav} active={path} onSelect={select} />
        </nav>

        <main className="ol-hub-scroll" style={{ flex: 1, minWidth: 0, overflowY: 'auto', height: '100%', background: 'var(--mantine-color-body)' }}>
          <div style={{ padding: 24, maxWidth: 1400, margin: '0 auto', width: '100%' }}>
            <div style={{ marginBottom: 20 }}>
              <Title order={2} style={{ margin: 0, fontSize: 22, fontWeight: 700, color: 'var(--mantine-color-text)' }}>
                {title}
              </Title>
              {subtitle ? (
                <Text c="dimmed" mt={4} size="sm">
                  {subtitle}
                </Text>
              ) : null}
            </div>
            {node ? (
              <SectionBoundary label={node.label}><UserProvider><SSOProvider>{renderLeaf(node)}</SSOProvider></UserProvider></SectionBoundary>
            ) : (
              <Text size="sm" c="dimmed">
                Unknown section “{path}”. Pick a page from the menu.
              </Text>
            )}
          </div>
        </main>
      </div>
    </div>
        </UserSettingsProvider>
      </SplitTestProvider>
    </OlliTProvider>
  )
}
