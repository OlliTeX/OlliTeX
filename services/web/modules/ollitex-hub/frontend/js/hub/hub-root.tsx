import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Group, Text, Title, Anchor } from '@mantine/core'
import { getJSON } from '@/infrastructure/fetch-json'
import getMeta from '@/utils/meta'
import Icon from '../shared/icons'
import ThemeToggle from '../shared/theme-toggle'
import Rail from './rail'
import SectionBoundary from './section-boundary'
import { renderLeaf } from './leaves'
import { HUB_NAV, HubNode, indexNav, visibleNav } from './nav-tree'
import { accordionState } from './accordion-state'
import {
  buildThemePatch,
  cssVarsFor,
  getAppliedHubTheme,
  onAppliedHubThemeChange,
} from './hub-theme'
import OlliTProvider from '../../../../../frontend/js/shared/mantine/provider'
import {
  currentColorScheme,
  onColorSchemeChange,
} from '../../../../../frontend/js/shared/mantine/overall-theme'

function parseHash(): string {
  try {
    return window.location.hash.replace(/^#\/?/, '')
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

/** Standalone classic pages for the header "Open full page" link. */
const STANDALONE: Record<string, { href: string; label: string }> = {
  'projects.all': { href: '/project', label: 'Full project list' },
  'projects.owned': { href: '/project', label: 'Full project list' },
  'projects.shared': { href: '/project', label: 'Full project list' },
  'projects.archived': { href: '/project', label: 'Full project list' },
  'templates.all': { href: '/templates', label: 'Full template gallery' },
  library: { href: '/library', label: 'Full library' },
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
}

export default function HubRoot() {
  const admin = useMemo(() => isAdminUser(), [])
  const [cats, setCats] = useState<HubNode[] | null>(null)
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
          .filter((c: any) => c && typeof c.key === 'string')
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
      setPath(id)
      try {
        window.history.replaceState(null, '', `#/${id}`)
      } catch {
        // tests without history
      }
      const anc = idx.ancestors.get(id) || []
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
  const title = TITLES[path]?.title || node?.label || 'OlliTeX hub'
  const subtitle = TITLES[path]?.subtitle

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
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        minHeight: '100vh',
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
        <Group gap="sm" wrap="nowrap">
          <Icon name="auto_storyboard" size={26} style={{ color: 'var(--mantine-color-ollitex-6)' }} />
          <div>
            <Title order={4} style={{ margin: 0, fontSize: 16, fontWeight: 700 }}>
              OlliTeX
            </Title>
            <Text size="xs" c="dimmed" style={{ lineHeight: 1.2 }}>
              {admin ? 'Workspace & administration' : 'Workspace'}
            </Text>
          </div>
        </Group>
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

      <div style={{ display: 'flex', flex: 1, minHeight: 0 }}>
        <nav
          aria-label="Primary"
          style={{
            width: 260,
            flexShrink: 0,
            overflowY: 'auto',
            overscrollBehavior: 'contain',
            background: 'var(--mantine-color-body)',
            borderRight: '1px solid var(--mantine-color-border)',
            padding: '12px 10px',
          }}
        >
          <Rail nav={nav} active={path} onSelect={select} />
        </nav>

        <main style={{ flex: 1, minWidth: 0, overflowY: 'auto', background: 'var(--mantine-color-body)' }}>
          <div style={{ padding: 24, maxWidth: 1400, margin: '0 auto', width: '100%' }}>
            <div style={{ marginBottom: 20 }}>
              <Title order={2} style={{ margin: 0, fontSize: 22, fontWeight: 700 }}>
                {title}
              </Title>
              {subtitle ? (
                <Text c="dimmed" mt={4} size="sm">
                  {subtitle}
                </Text>
              ) : null}
            </div>
            {node ? (
              <SectionBoundary label={node.label}>{renderLeaf(node)}</SectionBoundary>
            ) : (
              <Text size="sm" c="dimmed">
                Unknown section “{path}”. Pick a page from the menu.
              </Text>
            )}
          </div>
        </main>
      </div>
    </div>
    </OlliTProvider>
  )
}
