import React from 'react'
import { Stack, Text, Title, Alert, Card } from '@mantine/core'
import type { HubNode } from './nav-tree'
import { hubNavigate } from './navigate'
import Icon from '../shared/icons'
import ProjectsSection from '../sections/workspace/projects-section'
import TemplatesSection from '../sections/workspace/templates-section'
import LibrarySection from '../sections/workspace/library-section'
import NotificationsSettingsSection from '../sections/workspace/notifications-settings-section'
import MySettingsSection from '../sections/workspace/my-settings-section'
import KeybindingsSection from '../sections/workspace/keybindings-section'
import LlmSettingsSection from '../sections/workspace/llm-settings-section'
import AppearanceSection from '../sections/appearance-section'
import AdminInstanceSection from '../sections/admin/admin-instance-section'
import AdminSiteSection from '../sections/admin/admin-site-section'
import { NATIVE_SITE_SECTIONS } from '../sections/admin/site'
import AdminLlmSection from '../sections/admin/admin-llm-section'
import AdminUsersSection from '../sections/admin/admin-users-section'
import AdminProjectsSection from '../sections/admin/admin-projects-section'
import AdminTemplatesSection from '../sections/admin/admin-templates-section'
// Legacy user-settings sections (features/settings) — self-contained React
// components reused so /hub keeps every parameter (owner parity #11).
import SessionsLeaf from '../sections/workspace/sessions-section'
import SystemMessagesSection from '../sections/admin/system-messages-section'
import InstanceStatsSection from '../sections/admin/instance-stats-section'
import GrammarSettingsSection from '../../../../languagetool/frontend/js/grammar-settings-section'
import LLMComplianceSettings from '../../../../llm/frontend/js/components/llm-compliance-settings'
import LLMUsageMeter from '../../../../llm/frontend/js/components/llm-usage-meter'
import GitHubSyncWidget from '../../../../github-sync/frontend/js/components/github-sync-widget'
import WebdavWidget from '../../../../webdav/frontend/js/components/webdav-widget'
import DropboxWidget from '../../../../dropbox/frontend/js/components/dropbox-widget'
import { ZoteroWidget } from '../../../../zotero/frontend/js/components/zotero-widget'
import { MendeleyWidget } from '../../../../mendeley/frontend/js/components/mendeley-widget'


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
    const Native = NATIVE_SITE_SECTIONS[node.siteId]
    if (Native) return <Native key={node.id} />
    return <AdminSiteSection key={node.id} fixedSection={node.siteId} hideNav />
  }
  if (kind === 'overview') {
    // Overview "Instance management" shortcuts (owner review #2): navigate
    // to the relevant section instead of dead links (no-op before).
    return <AdminInstanceSection key={node.id} onNavigate={hubNavigate} />
  }
  if (kind === 'library') return <LibrarySection key={node.id} />

  switch (node.id) {
    case 'projects.tags.tags':
      return <ProjectsSection key={node.id} tagsFocus />
    case 'projects.tags.new':
      return <ProjectsSection key={node.id} tagsFocus defaultNewTag />
    case 'mysettings.email':
      return <NotificationsSettingsSection key={node.id} />
    case 'mysettings.llm.general':
      // BYO providers (add / remove / test / model selection)
      return <LlmSettingsSection key={node.id} />
    case 'mysettings.llm.grammar':
      // owner #7 (2026-09-07): each leaf shows its OWN section (was: all
      // four rendered the same combined page)
      return (
        <Card key={node.id} withBorder paddings="md" radius="lg" mb="xs">
          <Text fw={700} mb="xs">Grammar Checking</Text>
          <Text size="sm" c="dimmed" mb="md">
            Shared LanguageTool settings for grammar feedback in your projects
            (mode, language, blocked rules). The per-project strictness toggle
            stays in the project settings.
          </Text>
          <GrammarSettingsSection />
        </Card>
      )
    case 'mysettings.llm.compliance':
      return (
        <Card key={node.id} withBorder paddings="md" radius="lg" mb="xs">
          <Text fw={700} mb="xs">Compliance Review</Text>
          <Text size="sm" c="dimmed" mb="md">
            Your review rubrics for the AI compliance review in the project
            review panel.
          </Text>
          <LLMComplianceSettings />
        </Card>
      )
    case 'mysettings.llm.usage':
      return (
        <Card key={node.id} withBorder paddings="md" radius="lg" mb="xs">
          <Text fw={700} mb="xs">Usage</Text>
          <Text size="sm" c="dimmed" mb="md">
            Your LLM usage for the last 30 days.
          </Text>
          <LLMUsageMeter scope="user" />
        </Card>
      )
    case 'site.general.managetpl':
      return <AdminTemplatesSection key={node.id} />
    case 'site.general.appearance':
      return <AppearanceSection key={node.id} />
    case 'site.general.enclose':
      // Owner mapping 2026-09-07: this leaf carries the legacy /admin/panel
      // content (the full classic site-settings panel, mirror of /admin/site)
      // until the native Mantine rebuilds land.
      return <AdminSiteSection key={node.id} />
    case 'site.general.projects.all':
      return <AdminProjectsSection key={node.id} view="all" />
    case 'site.general.projects.inactive':
      return <AdminProjectsSection key={node.id} view="inactive" />
    case 'site.general.projects.trashed':
      return <AdminProjectsSection key={node.id} view="trashed" />
    case 'site.general.projects.deleted':
      return <AdminProjectsSection key={node.id} view="deleted" />
    case 'site.general.users.all':
      return <AdminUsersSection key={node.id} view="all" />
    case 'site.general.users.admins':
      return <AdminUsersSection key={node.id} view="admins" />
    case 'site.general.users.suspended':
      return <AdminUsersSection key={node.id} view="suspended" />
    case 'site.general.users.inactive':
      return <AdminUsersSection key={node.id} view="inactive" />
    case 'site.general.users.deleted':
      return <AdminUsersSection key={node.id} view="deleted" />
    case 'mysettings.account':
      return <MySettingsSection key={node.id} initialTab="account" />
    case 'mysettings.password':
      return <MySettingsSection key={node.id} initialTab="password" />
    case 'mysettings.appearance':
      return <MySettingsSection key={node.id} initialTab="appearance" />
    case 'mysettings.editordefaults':
      return <MySettingsSection key={node.id} initialTab="editor" />
    case 'mysettings.keybindings':
      // Owner #5a/5b (2026-09-07): Mantine rework (legacy bootstrap card →
      // Mantine Radio.Group + custom-bindings modal, same saveUserSettings
      // store, same capture/import/export logic).
      return <KeybindingsSection key={node.id} />
    case 'mysettings.sync':
      // owner #6b (2026-09-07): each leaf renders its OWN provider set
      // (before: both leaves showed the same combined legacy page — and on
      // /hub it was EMPTY, because importOverleafModules has no widgets in
      // this entry). The provider widgets below are plain React components
      // and render natively in the hub.
      return (
        <Stack key={node.id} gap="md">
          <Text size="sm" c="dimmed">
            Keep your projects in sync with external storage and git remotes.
          </Text>
          <Card withBorder paddings="md" radius="lg">
            <Text fw={700} mb={4}>GitHub</Text>
            <Text size="sm" c="dimmed" mb="md">
              Sync projects with GitHub repositories and git servers.
            </Text>
            <GitHubSyncWidget />
          </Card>
          <Card withBorder paddings="md" radius="lg">
            <Text fw={700} mb={4}>WebDAV (Nextcloud)</Text>
            <Text size="sm" c="dimmed" mb="md">
              Mirror your project to a WebDAV / Nextcloud folder.
            </Text>
            <WebdavWidget />
          </Card>
          <Card withBorder paddings="md" radius="lg">
            <Text fw={700} mb={4}>Dropbox</Text>
            <Text size="sm" c="dimmed" mb="md">
              Mirror your project to Dropbox.
            </Text>
            <DropboxWidget />
          </Card>
        </Stack>
      )
    case 'mysettings.references':
      return (
        <Stack key={node.id} gap="md">
          <Text size="sm" c="dimmed">
            Connect your reference managers and import literature into any project.
          </Text>
          <Card withBorder paddings="md" radius="lg">
            <Text fw={700} mb={4}>Zotero</Text>
            <Text size="sm" c="dimmed" mb="md">
              Link your Zotero account with an API key
              (zotero.org/settings/keys).
            </Text>
            <ZoteroWidget />
          </Card>
          <Card withBorder paddings="md" radius="lg">
            <Text fw={700} mb={4}>Mendeley</Text>
            <Text size="sm" c="dimmed" mb="md">
              Sign in to Mendeley to link your reference library.
            </Text>
            <MendeleyWidget />
          </Card>
          <Card withBorder paddings="md" radius="lg">
            <Text fw={700} mb={4}>ORCID &amp; manual imports</Text>
            <Text size="sm" c="dimmed">
              Import from ORCID, paste BibTeX/DOI, or enter references manually
              in your{' '}
              <a href="/hub#/library" style={{ color: 'var(--mantine-color-ollitex-6, #1e6b41)' }}>
                reference library
              </a>
              .
            </Text>
          </Card>
        </Stack>
      )
    case 'mysettings.sessions':
      return <SessionsLeaf key={node.id} />
    case 'site.general.messages':
      return <SystemMessagesSection key={node.id} />
    case 'site.general.stats':
      return <InstanceStatsSection key={node.id} />
    case 'site.llm.features':
    case 'site.llm.connection':
    case 'site.llm.models':
    case 'site.llm.prompt':
    case 'site.llm.prompts':
    case 'site.llm.usage':
      // owner #1 (2026-09-07): every leaf now renders ONLY its own section
      // (before all six showed the same combined page)
      return (
        <AdminLlmSection key={node.id} section={node.id.split('.').pop() as 'features' | 'connection' | 'models' | 'prompt' | 'prompts' | 'usage'} />
      )
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
