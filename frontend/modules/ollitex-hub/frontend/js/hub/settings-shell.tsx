import React, { useCallback, useMemo, useState } from 'react'
import {
  Anchor,
  Box,
  Card,
  Group,
  ScrollArea,
  Text,
  rem,
} from '@mantine/core'
import { useDisclosure, useMediaQuery } from '@mantine/hooks'
import getMeta from '@/utils/meta'
import ThemeToggle from '../shared/theme-toggle'
import type { HubNode } from './nav-tree'

/**
 * AH-1 (2026-10-08) — the settings-page shell (mantine-analytics-dashboard
 * layout, Mantine 9.6). Owner fixes applied in this pass:
 *   - header is FULL WIDTH (starts completely left — no 300px offset),
 *   - the footer is GONE,
 *   - vertical padding is tightened (no "insanely large" section gaps),
 *   - the hamburger / person glyph is INLINE SVG (the material-symbols
 *     ligature was quoted by the owner as rendered raw text — SVG renders
 *     in every browser, no font dependency),
 *   - no subtitle slot (the "(AH)" prose was removed by the owner's ask),
 *   - sidebar entries are real ANCHORS: /user-settings/<id> and
 *     /admin-settings/<id> — one page per section (AJ-1: no long scroll).
 * The hub theme bridge (OlliTProvider) is rendered by the page wrapper.
 */

const SIDEBAR_WIDTH = 300
const HEADER_HEIGHT = 60

export type SettingsNavItem = { id: string; label: string; icon: string }
export type SettingsNavGroup = { group: string; entries: SettingsNavItem[] }

/** inline SVG hamburger (replaces the material-symbols `menu` ligature). */
function MenuIcon({ size = 20 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
      <path d="M4 6h16M4 12h16M4 18h16" />
    </svg>
  )
}

/** inline SVG person glyph (replaces the material-symbols `person`). */
function PersonIcon({ size = 16 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="8" r="4" />
      <path d="M4 20c0-4 3.5-6 8-6s8 2 8 6" />
    </svg>
  )
}

/** inline SVG chevron used by sidebar entries (working icon, no font). */
function ChevronIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M9 6l6 6-6 6" />
    </svg>
  )
}

export function SettingsShell({
  title,
  nav,
  basePath,
  activeId,
  children,
  aside,
  backHref,
  backLabel,
}: {
  title: string
  nav: SettingsNavGroup[]
  /** '/user-settings' | '/admin-settings' — sidebar entries link to
   * `${basePath}/${id}` (one page per section, AJ-1). */
  basePath: string
  /** the section this page shows (sidebar highlight). */
  activeId?: string
  children: React.ReactNode
  aside?: React.ReactNode
  /** breadcrumb target (section pages link back to the landing). */
  backHref?: string
  backLabel?: string
}) {
  const mobile = useMediaQuery('(max-width: 768px)')
  const [mobileOpen, { toggle: toggleMobile, close: closeMobile }] = useDisclosure()

  const go = useCallback(
    (href: string) => {
      closeMobile()
      window.location.href = href
    },
    [closeMobile],
  )

  const email = useMemo(() => {
    try {
      return (getMeta as any)('ol-users-email') || ''
    } catch {
      return ''
    }
  }, [])
  const brand = useMemo(() => {
    try {
      const nb = (getMeta as any)('ol-navbar')
      return (nb && (nb.customLogo || nb.customLogoDark)) || '/logo_full.svg'
    } catch {
      return '/logo_full.svg'
    }
  }, [])

  return (
    <Box
      style={{
        color: 'var(--mantine-color-text)',
        minHeight: '100vh',
        background: 'var(--mantine-color-body)',
      }}
    >
      {/* fixed header — FULL WIDTH, starts completely left (owner AH-1) */}
      <header
        style={{
          position: 'fixed',
          top: 0,
          left: 0,
          right: 0,
          width: '100vw',
          height: HEADER_HEIGHT,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 16,
          padding: '0 20px',
          background: 'var(--mantine-color-body)',
          borderBottom: '1px solid var(--mantine-color-border)',
          zIndex: 101,
          boxShadow: 'var(--mantine-shadow-sm)',
        }}
      >
        <Group gap="md" align="center" wrap="nowrap" style={{ minWidth: 0 }}>
          <button
            type="button"
            aria-label="Toggle menu"
            onClick={toggleMobile}
            style={{
              display: mobile ? 'inline-flex' : 'none',
              alignItems: 'center',
              justifyContent: 'center',
              width: 34,
              height: 34,
              border: 'none',
              background: 'transparent',
              color: 'inherit',
              cursor: 'pointer',
              borderRadius: 6,
            }}
          >
            <MenuIcon size={20} />
          </button>
          <a href={backHref || '/'} style={{ display: 'inline-flex', alignItems: 'center' }}>
            <img src={brand} alt="OlliTeX" style={{ height: 30, width: 'auto', display: 'block' }} />
          </a>
          {!mobile ? (
            <Box style={{ minWidth: 0 }}>
              <Text size="sm" fw={600} lh={1.1} style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                {title}
              </Text>
            </Box>
          ) : null}
        </Group>
        <Group gap="sm" align="center" wrap="nowrap">
          <Anchor href={backHref || '/hub'} size="sm">
            {backLabel || (activeId ? `← ${title}` : '← Hub')}
          </Anchor>
          <ThemeToggle />
          {email && !mobile ? (
            <Group gap="xs" px={10} py={4} wrap="nowrap">
              <span style={{ display: 'inline-flex', color: 'var(--mantine-color-dimmed)' }}>
                <PersonIcon size={16} />
              </span>
              <Text
                size="sm"
                fw={600}
                style={{ maxWidth: 220, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
              >
                {email}
              </Text>
            </Group>
          ) : null}
        </Group>
      </header>

      {/* fixed sidebar (mobile → overlay) — below the full-width header */}
      <aside
        style={{
          position: 'fixed',
          top: HEADER_HEIGHT,
          left: 0,
          width: SIDEBAR_WIDTH,
          height: `calc(100vh - ${HEADER_HEIGHT}px)`,
          background: 'var(--mantine-color-body)',
          borderRight: '1px solid var(--mantine-color-border)',
          zIndex: mobile ? (mobileOpen ? 102 : 1) : 100,
          transform: mobile && !mobileOpen ? 'translateX(-100%)' : 'translateX(0)',
          transition: 'transform 150ms ease',
        }}
      >
        <ScrollArea style={{ height: '100vh', maxHeight: `calc(100vh - ${HEADER_HEIGHT}px)` }}>
          <Box p="sm" style={{ paddingTop: 10 }}>
            {nav.map(group => (
              <Box key={group.group} mb="sm">
                <Text
                  size="10"
                  fw={700}
                  tt="uppercase"
                  c="dimmed"
                  px={8}
                  mb={3}
                  style={{ letterSpacing: '0.08em' }}
                >
                  {group.group}
                </Text>
                {group.entries.map(entry => {
                  const activeNow = activeId === entry.id
                  return (
                    <a
                      key={entry.id}
                      href={`${basePath}/${entry.id}`}
                      onClick={e => {
                        e.preventDefault()
                        go(`${basePath}/${entry.id}`)
                      }}
                      data-settings-entry={entry.id}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 8,
                        padding: '5px 8px',
                        borderRadius: 6,
                        color: activeNow ? 'var(--mantine-color-blue)' : 'inherit',
                        fontWeight: activeNow ? 600 : 400,
                        background: activeNow ? 'var(--mantine-color-blue-light)' : 'transparent',
                        textDecoration: 'none',
                      }}
                    >
                      <span
                        style={{
                          width: 18,
                          height: 18,
                          display: 'inline-flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          color: activeNow ? 'inherit' : 'var(--mantine-color-dimmed)',
                        }}
                      >
                        <ChevronIcon />
                      </span>
                      <Text
                        size="sm"
                        style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                      >
                        {entry.label}
                      </Text>
                    </a>
                  )
                })}
              </Box>
            ))}
          </Box>
        </ScrollArea>
      </aside>

      {/* mobile overlay backdrop */}
      {mobile && mobileOpen && (
        <Box
          pos="fixed"
          zIndex={101}
          sx={{ inset: 0, background: 'rgba(0,0,0,0.4)' }}
          onClick={closeMobile}
        />
      )}

      {/* content pane — tight vertical rhythm (owner AH-1: no huge gaps) */}
      <main
        style={{
          marginLeft: mobile ? 0 : SIDEBAR_WIDTH,
          paddingTop: HEADER_HEIGHT,
          minHeight: '100vh',
        }}
      >
        <div style={{ maxWidth: 1080, padding: '18px 24px 32px' }}>
          {activeId ? (
            <Group gap="sm" align="center" mb="sm" wrap="nowrap">
              {backHref ? (
                <Anchor href={backHref} size="sm" c="dimmed">
                  ← {backLabel || title}
                </Anchor>
              ) : null}
              {aside ? <Group gap="sm" style={{ marginLeft: 'auto' }}>{aside}</Group> : null}
            </Group>
          ) : (
            <Group justify="space-between" align="flex-start" mb="sm" wrap="nowrap">
              <Text size="md" c="dimmed" style={{ maxWidth: 720 }}>
                Select a section — each one is its own page (no long scroll).
              </Text>
              {aside && !mobile ? <Group gap="sm">{aside}</Group> : null}
            </Group>
          )}
          {children}
        </div>
      </main>
      {/* no footer (owner AH-1: footers removed) */}
    </Box>
  )
}

/** the section card — used by the single-section pages (AJ-1: ONE card per
 * page, so the card is the page; spacing is tight). */
export function SettingsSection({
  id,
  node,
  children,
}: {
  id: string
  node: HubNode
  children: React.ReactNode
}) {
  return (
    <Card
      id={'settings-sec-' + id}
      mb="xs"
      mt={rem(4)}
      withBorder
      radius="md"
    >
      <Card.Section p="sm" pb="xs">
        <Text size="md" fw={600}>
          {node.label}
        </Text>
      </Card.Section>
      <Card.Section p="md">{children}</Card.Section>
    </Card>
  )
}
