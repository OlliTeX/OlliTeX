import React, { useState } from 'react'
import { Meta, StoryObj } from '@storybook/react'
import { Box, Group, Text } from '@mantine/core'
import { HUB_NAV, HubNode } from '../../modules/ollitex-hub/frontend/js/hub/nav-tree'
import ThemeToggle from '../../modules/ollitex-hub/frontend/js/shared/theme-toggle'
import {
  SettingsShell,
  SettingsSection,
  SettingsNavGroup,
} from '../../modules/ollitex-hub/frontend/js/hub/settings-shell'

/**
 * OlliTeX hub chrome — the AJ/AH settings-surface family (AJ-1 per-section
 * pages, AJ-6 theme dropdown, AH-1 shell geometry) + the nav-tree data
 * shape (the rail is data-driven: HUB_NAV).
 */
const meta: Meta = {
  title: 'Hub/Chrome',
  component: SettingsShell,
  parameters: { layout: 'fullscreen' },
  tags: ['autodocs'],
}
export default meta

type Story = StoryObj

/* ---------------- nav-tree (data-driven rail) ---------------- */

function Outline({ nodes, depth = 0 }: { nodes: HubNode[]; depth?: number }) {
  return (
    <Box component="ul" ml={depth ? 16 : 0} mt={0} style={{ listStyle: 'none', paddingLeft: 0 }}>
      {nodes.map(n => (
        <Box key={n.id} my={2}>
          <Group gap={8}>
            <Text component="span" size={n.section ? 'sm' : 'md'} fw={n.section ? 700 : 500} c={n.section ? 'dimmed' : undefined} tt={n.section ? 'uppercase' : undefined}>
              {n.label}
            </Text>
            {!!n.admin && <Text component="span" size="xs" c="yellow.6">admin-only</Text>}
            {!!n.render && <Text component="span" size="xs" c="dimmed">{n.render}{n.siteId ? `:${n.siteId}` : ''}{n.pview ? `:${n.pview}` : ''}</Text>}
            <Text component="span" size="xs" c="dimmed">#{n.id}</Text>
          </Group>
          {!!n.children && <Outline nodes={n.children} depth={depth + 1} />}
        </Box>
      ))}
    </Box>
  )
}

export const NavTree: Story = {
  render: () => (
    <Box p="xl" style={{ maxWidth: 860 }}>
      <Text size="lg" fw={700} mb="sm">
        HUB_NAV — the rail is data-driven (nav-tree.ts)
      </Text>
      <Outline nodes={HUB_NAV} />
    </Box>
  ),
}

/* ---------------- AJ-6: the one theme dropdown ---------------- */

export const ThemePicker: Story = {
  render: () => (
    <Group p="lg" gap="md">
      <Text size="sm" c="dimmed">
        AJ-6 — ONE dropdown (the three radios are retired), saved per user:
      </Text>
      <ThemeToggle />
    </Group>
  ),
}

/* ---------------- AH-1 / AJ-1: the settings shell ---------------- */

const NAV: SettingsNavGroup[] = [
  {
    group: 'Account',
    entries: [
      { id: 'account', label: 'Account', icon: 'person' },
      { id: 'appearance', label: 'Appearance', icon: 'palette' },
      { id: 'notifications', label: 'Notifications', icon: 'notifications' },
    ],
  },
  {
    group: 'Editor',
    entries: [
      { id: 'editor', label: 'Editor', icon: 'edit' },
      { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard' },
    ],
  },
]

function sampleSection(id: string, label: string) {
  return (
    <SettingsSection id={id} node={{ id, label, icon: 'settings' }}>
      <Text size="sm" c="dimmed" mb="sm">
        {label} — one page per section (AJ-1); no long scroll.
      </Text>
      <Text size="sm">
        Body content slot: forms render here inside the shell. The shell owns
        the full-width header, the 300px sidebar (real anchors
        {id === 'account' ? ' /user-settings/<id>' : ' /admin-settings/<id>'}) and the mobile
        overlay.
      </Text>
    </SettingsSection>
  )
}

export const SettingsShellUser: Story = {
  render: () => (
    <SettingsShell
      title="My settings"
      nav={NAV}
      basePath="/user-settings"
      activeId="account"
      backHref="/user-settings"
      backLabel="All settings"
    >
      {sampleSection('account', 'Account')}
      {sampleSection('appearance', 'Appearance')}
    </SettingsShell>
  ),
}

export const SettingsShellAdmin: Story = {
  render: () => (
    <SettingsShell
      title="Site settings"
      nav={NAV}
      basePath="/admin-settings"
      activeId="notifications"
      backHref="/admin-settings"
      backLabel="All settings"
      aside={<Text size="xs" c="dimmed">Aside slot (AJ-5 badges retired from the shell)</Text>}
    >
      {sampleSection('notifications', 'Notifications (admin)')}
    </SettingsShell>
  ),
}
