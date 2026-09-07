import React from 'react'
import { Stack, Text, Title, Alert } from '@mantine/core'
import type { HubNode } from './nav-tree'
import Icon from '../shared/icons'
import ProjectsSection from '../sections/workspace/projects-section'
import TemplatesSection from '../sections/workspace/templates-section'
import LibrarySection from '../sections/workspace/library-section'
import NotificationsSettingsSection from '../sections/workspace/notifications-settings-section'
import MySettingsSection from '../sections/workspace/my-settings-section'
import LlmSettingsSection from '../sections/workspace/llm-settings-section'
import AppearanceSection from '../sections/appearance-section'
import AdminInstanceSection from '../sections/admin/admin-instance-section'
import AdminSiteSection from '../sections/admin/admin-site-section'
import AdminLlmSection from '../sections/admin/admin-llm-section'
import AdminUsersSection from '../sections/admin/admin-users-section'
import AdminProjectsSection from '../sections/admin/admin-projects-section'
import AdminTemplatesSection from '../sections/admin/admin-templates-section'

/**
 * Leaf renderer for the unified /hub (nav_structure.md §4).
 * Reuses the existing Mantine section components; leaves that are still
 * "combined" pages (to be split by view in later milestones) all point at
 * the same working component — nothing is stubbed away from the user.
 */
export function renderLeaf(node: HubNode): React.ReactNode {
  const kind = node.render
  if (kind === 'projects') {
    return <ProjectsSection key={node.id} defaultFilter={node.pview || 'all'} />
  }
  if (kind === 'template-cat') {
    return <TemplatesSection key={node.id} initialCategory={node.category || 'none'} />
  }
  if (kind === 'site-sec' && node.siteId) {
    return <AdminSiteSection key={node.id} fixedSection={node.siteId} hideNav />
  }
  if (kind === 'overview') return <AdminInstanceSection key={node.id} />
  if (kind === 'library') return <LibrarySection key={node.id} />

  switch (node.id) {
    case 'projects.tags.tags':
      return <ProjectsSection key={node.id} tagsFocus />
    case 'projects.tags.new':
      return <ProjectsSection key={node.id} tagsFocus defaultNewTag />
    case 'mysettings.email':
      return <NotificationsSettingsSection key={node.id} />
    case 'mysettings.llm.general':
    case 'mysettings.llm.grammar':
    case 'mysettings.llm.compliance':
    case 'mysettings.llm.usage':
      // combined BYO-LLM page (four sections: General, Grammar Checking,
      // Compliance Review, Usage) — split per item in a later milestone
      return <LlmSettingsSection key={node.id} />
    case 'site.general.managetpl':
      return <AdminTemplatesSection key={node.id} />
    case 'site.general.appearance':
      return <AppearanceSection key={node.id} />
    case 'site.general.projects.all':
    case 'site.general.projects.inactive':
    case 'site.general.projects.trashed':
    case 'site.general.projects.deleted':
      return <AdminProjectsSection key={node.id} />
    case 'site.general.users.all':
    case 'site.general.users.admins':
    case 'site.general.users.suspended':
    case 'site.general.users.inactive':
    case 'site.general.users.deleted':
      return <AdminUsersSection key={node.id} />
    case 'site.llm.features':
    case 'site.llm.connection':
    case 'site.llm.models':
    case 'site.llm.prompt':
    case 'site.llm.prompts':
    case 'site.llm.usage':
      return <AdminLlmSection key={node.id} />
    default:
      break
  }

  // mysettings split pages before the split milestone render the combined
  // my-settings page so all current parameters stay reachable
  if (node.id.startsWith('mysettings.')) {
    return <MySettingsSection key={node.id} />
  }

  // honest placeholder for sections still being built (nav_structure.md §4)
  return (
    <Alert
      icon={<Icon name="construction" size={20} />}
      title={<span style={{ fontSize: 15, fontWeight: 700 }}>“{node.label}” is being built</span>}
      color="blue"
      variant="light"
      radius="md"
    >
      This section of the unified hub is on the build plan. The data and
      settings it manages are untouched — the page will appear here as soon
      as it ships.
    </Alert>
  )
}
