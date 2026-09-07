import React from 'react'
import { Text } from '@mantine/core'
import HubLayout, { HubNavGroup, OpenInAppLink } from '../shared/hub-layout'
import useHashSection from '../shared/use-hash-section'
import ThemeToggle from '../shared/theme-toggle'
import { SectionBoundary } from './hub-shell'
import { PageError } from '../shared/page-state'
import ProjectsSection from '../sections/workspace/projects-section'
import LibrarySection from '../sections/workspace/library-section'
import TemplatesSection from '../sections/workspace/templates-section'
import MySettingsSection from '../sections/workspace/my-settings-section'
import NotificationsSettingsSection from '../sections/workspace/notifications-settings-section'
import LlmSettingsSection from '../sections/workspace/llm-settings-section'

const NAV: HubNavGroup[] = [
  {
    label: 'Workspace',
    items: [
      { id: 'projects', label: 'Projects', icon: 'folder' },
      { id: 'library', label: 'Reference library', icon: 'menu_book' },
      { id: 'templates', label: 'Templates', icon: 'extension' },
    ],
  },
  {
    label: 'My settings',
    items: [
      { id: 'settings-account', label: 'My settings', icon: 'person' },
      { id: 'settings-notifications', label: 'Notifications', icon: 'notifications' },
      { id: 'settings-llm', label: 'LLM assistant', icon: 'smart_toy' },
    ],
  },
]

const META: Record<string, { title: string; subtitle: string; standalone?: string; standaloneLabel?: string }> = {
  projects: {
    title: 'Projects',
    subtitle: 'Everything you work on — create, open, and manage projects.',
    standalone: '/project',
    standaloneLabel: 'Full project list',
  },
  library: {
    title: 'Reference library',
    subtitle: 'Your personal bibliography, citable from any project.',
    standalone: '/library',
    standaloneLabel: 'Full library',
  },
  templates: {
    title: 'Templates',
    subtitle: 'Start a new project from a shared template.',
    standalone: '/templates',
    standaloneLabel: 'Full template gallery',
  },
  'settings-account': {
    title: 'My settings',
    subtitle: 'Account, password, appearance, and editor defaults.',
    standalone: '/user/mysettings',
    standaloneLabel: 'Full settings page',
  },
  'settings-notifications': {
    title: 'Notification preferences',
    subtitle: 'Control activity emails and their batching delay.',
    standalone: '/user/notification-preferences',
    standaloneLabel: 'Full preferences page',
  },
  'settings-llm': {
    title: 'LLM assistant',
    subtitle: 'Your bring-your-own LLM providers and models.',
    standalone: '/user/llm-settings',
    standaloneLabel: 'Full LLM settings',
  },
}

function SectionFallback({ label }: { label: string }) {
  return (
    <PageError
      label={`The ${label} section failed to load`}
      detail="This section hit a runtime error. The rest of the hub is unaffected — reload the page or try the standalone page."
    />
  )
}

export default function WorkspaceHubRoot() {
  const { section, select } = useHashSection('projects')

  return (
    <HubLayout
      brand="LibreLeaf"
      tagline="Workspace"
      nav={NAV}
      active={section}
      onSelect={select}
      title={META[section]?.title || 'Workspace'}
      subtitle={META[section]?.subtitle}
      headerActions={
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <ThemeToggle />
          {META[section]?.standalone ? (
            <OpenInAppLink href={META[section].standalone as string} label={META[section].standaloneLabel as string} />
          ) : null}
        </div>
      }
    >
      <SectionBoundary label={META[section]?.title || section}>
        {section === 'projects' ? <ProjectsSection /> : null}
        {section === 'library' ? <LibrarySection /> : null}
        {section === 'templates' ? <TemplatesSection /> : null}
        {section === 'settings-account' ? <MySettingsSection /> : null}
        {section === 'settings-notifications' ? <NotificationsSettingsSection /> : null}
        {section === 'settings-llm' ? <LlmSettingsSection /> : null}
        {!META[section] ? (
          <Text size="sm" c="dimmed">
            Unknown section “{section}”. Pick a page from the menu.
          </Text>
        ) : null}
      </SectionBoundary>
    </HubLayout>
  )
}
