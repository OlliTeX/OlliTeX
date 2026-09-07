import React from 'react'
import { Card, Group, Stack, Text } from '@mantine/core'
import Icon from '../../../shared/icons'
import { HUB_NAV, visibleNav } from '../../../hub/nav-tree'
import type { HubNode } from '../../../hub/nav-tree'

interface Row {
  id: string
  label: string
  icon: string
  parent?: string
}

function* leafRows(node: HubNode, parent?: string): Generator<Row> {
  if (node.children && node.children.length > 0) {
    for (const c of node.children) yield* leafRows(c, node.label)
  } else {
    yield { id: node.id, label: node.label, icon: node.icon || 'chevron_right', parent }
  }
}

/**
 * site.general.enclose — native "All site settings" index (owner review B16):
 * replaces the legacy full-panel embed with a map of the Mantine sections.
 * Plain hash anchors work because hub-root listens to hashchange.
 */
export default function SiteSettingsIndexSection() {
  const nav = visibleNav(HUB_NAV, true)
  const site = nav.find(n => n.id === 'site')
  const rows = site
    ? Array.from(leafRows(site)).filter(r => r.id !== 'site.general.enclose')
    : []

  const folders: Array<{ name: string; items: Row[] }> = []
  rows.forEach(r => {
    const name = r.parent || 'Site'
    let f = folders.find(x => x.name === name)
    if (!f) {
      f = { name, items: [] }
      folders.push(f)
    }
    f.items.push(r)
  })

  return (
    <Stack gap="md" style={{ maxWidth: 860 }}>
      <Text size="sm" c="dimmed">
        Instance configuration. Every section below is a full page in the hub — the classic
        /admin/site panel is retired.
      </Text>
      {folders.map(f => (
        <Card key={f.name} withBorder radius="lg" paddings="md">
          <Stack gap="xs">
            <Text size="sm" fw={700} c="dimmed" style={{ textTransform: 'uppercase', fontSize: 11, letterSpacing: 0.4 }}>
              {f.name}
            </Text>
            {f.items.map(r => (
              <a
                key={r.id}
                href={`#/${r.id}`}
                style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '6px 4px', borderRadius: 8, textDecoration: 'none', color: 'var(--mantine-color-text)' }}
                onMouseEnter={e => (e.currentTarget.style.background = 'color-mix(in srgb, var(--mantine-color-ollitex-6) 10%, transparent)')}
                onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
              >
                <Icon name={r.icon} size={18} style={{ color: 'var(--mantine-color-dimmed)', flexShrink: 0 }} />
                <span style={{ fontSize: 13.5 }}>{r.label}</span>
              </a>
            ))}
          </Stack>
        </Card>
      ))}
      {rows.length === 0 ? (
        <Text size="sm" c="dimmed">No site settings sections registered.</Text>
      ) : null}
    </Stack>
  )
}
