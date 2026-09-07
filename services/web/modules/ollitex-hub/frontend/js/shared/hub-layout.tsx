import React, { useState } from 'react'
import {
  Group,
  Text,
  Title,
  Anchor,
} from '@mantine/core'
import Icon from './icons'

export interface HubNavItem {
  id: string
  label: string
  icon: string
  badge?: string | number
}
export interface HubNavGroup {
  label?: string
  items: HubNavItem[]
}

interface HubLayoutProps {
  brand: string
  tagline?: string
  nav: HubNavGroup[]
  active: string
  onSelect: (id: string) => void
  title: string
  subtitle?: string
  headerActions?: React.ReactNode
  children: React.ReactNode
}

/**
 * Shared chrome for the OlliTeX hubs (/hub/workspace, /hub/admin).
 *
 * Deliberately NOT Mantine AppShell: the AppShell CSS (offset margins,
 * fixed rail) did not apply reliably in our bundle, which left the nav
 * rail overlapping the content. A plain flexbox shell is deterministic:
 *   header (sticky) / row( nav rail (own scrollbar) | main (own scrollbar) )
 * and both panes always carry the app body color, in light and dark.
 */
export default function HubLayout({
  brand,
  tagline,
  nav,
  active,
  onSelect,
  title,
  subtitle,
  headerActions,
  children,
}: HubLayoutProps) {
  const [railOpen, setRailOpen] = useState(true)

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        minHeight: '100vh',
        background: 'var(--mantine-color-body)',
        color: 'var(--mantine-color-text)',
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
          <button
            type="button"
            aria-label="Toggle navigation"
            onClick={() => setRailOpen(v => !v)}
            style={{
              border: 'none',
              background: 'transparent',
              cursor: 'pointer',
              display: 'inline-flex',
              padding: 4,
            }}
          >
            <Icon name={railOpen ? 'menu_open' : 'menu'} size={22} />
          </button>
          <Group gap="xs" wrap="nowrap">
            <Icon name="auto_stories" size={26} style={{ color: 'var(--mantine-color-ollitex-6)' }} />
            <div>
              <Title order={4} style={{ margin: 0, fontSize: 16, fontWeight: 700 }}>
                {brand}
              </Title>
              {tagline ? (
                <Text size="xs" c="dimmed" style={{ lineHeight: 1.2 }}>
                  {tagline}
                </Text>
              ) : null}
            </div>
          </Group>
        </Group>
        <Group gap="sm" wrap="nowrap" style={{ visibility: headerActions ? 'visible' : 'hidden' }}>
          {headerActions}
        </Group>
      </header>

      <div style={{ display: 'flex', flex: 1, minHeight: 0 }}>
        {railOpen ? (
          <nav
            aria-label="Primary"
            style={{
              width: 232,
              flexShrink: 0,
              overflowY: 'auto',
              overscrollBehavior: 'contain',
              background: 'var(--mantine-color-body)',
              borderRight: '1px solid var(--mantine-color-border)',
              padding: '12px 10px',
            }}
          >
            {nav.map((group, gi) => (
              <div key={group.label || `g${gi}`} style={{ marginBottom: 20 }}>
                {group.label ? (
                  <Text
                    size="xs"
                    tt="uppercase"
                    fw={700}
                    c="dimmed"
                    style={{ letterSpacing: '0.08em', padding: '0 8px', marginBottom: 6 }}
                  >
                    {group.label}
                  </Text>
                ) : null}
                <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                  {group.items.map(item => {
                    const isActive = active === item.id
                    return (
                      <button
                        key={item.id}
                        type="button"
                        onClick={() => onSelect(item.id)}
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          gap: 10,
                          width: '100%',
                          padding: '9px 12px',
                          borderRadius: 8,
                          border: 'none',
                          cursor: 'pointer',
                          fontSize: 14,
                          fontWeight: isActive ? 600 : 500,
                          textAlign: 'left',
                          background: isActive
                            ? 'var(--mantine-color-ollitex-6)'
                            : 'transparent',
                          color: isActive
                            ? 'var(--mantine-color-white)'
                            : 'var(--mantine-color-text)',
                          transition: 'background 120ms ease',
                        }}
                        onMouseEnter={e => {
                          if (!isActive) {
                            ;(e.currentTarget as HTMLButtonElement).style.background = 'var(--mantine-color-default-hover)'
                          }
                        }}
                        onMouseLeave={e => {
                          if (!isActive) {
                            ;(e.currentTarget as HTMLButtonElement).style.background = 'transparent'
                          }
                        }}
                      >
                        <Icon
                          name={item.icon}
                          size={20}
                          style={{ color: isActive ? 'var(--mantine-color-white)' : 'var(--mantine-color-dimmed)' }}
                        />
                        <span style={{ flex: 1 }}>{item.label}</span>
                        {item.badge ? (
                          <span
                            style={{
                              fontSize: 11,
                              fontWeight: 700,
                              padding: '1px 8px',
                              borderRadius: 999,
                              background: isActive
                                ? 'rgba(255,255,255,0.22)'
                                : 'var(--mantine-color-default-hover)',
                              color: isActive ? '#fff' : 'var(--mantine-color-dimmed)',
                            }}
                          >
                            {item.badge}
                          </span>
                        ) : null}
                      </button>
                    )
                  })}
                </div>
              </div>
            ))}
          </nav>
        ) : null}

        <main style={{ flex: 1, minWidth: 0, overflowY: 'auto', background: 'var(--mantine-color-body)' }}>
          <div style={{ padding: 24, maxWidth: 1400, margin: '0 auto', width: '100%' }}>
            {title ? (
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
            ) : null}
            {children}
          </div>
        </main>
      </div>
    </div>
  )
}

export function OpenInAppLink({ href, label }: { href: string; label: string }) {
  return (
    <Anchor
      href={href}
      target="_blank"
      rel="noreferrer"
      size="sm"
      style={{ textDecoration: 'none' }}
    >
      <Group gap={6}>
        <Icon name="open_in_new" size={16} />
        {label}
      </Group>
    </Anchor>
  )
}
