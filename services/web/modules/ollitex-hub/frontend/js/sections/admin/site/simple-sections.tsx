// /hub → Site settings: the remaining native sections (owner #32).
// Branding, Services, Grammar (LanguageTool), Pandoc, Git, GitHub Sync,
// Linked file types, WebDAV, Dropbox — same section ids + field sets as the
// legacy admin tabs.

import React from 'react'
import { Checkbox, Group, Text, Anchor } from '@mantine/core'
import {
  Area,
  Field,
  PageLoading,
  SectionShell,
  SectionTitle,
  SecretField,
  bool0,
  num0,
  str0,
  useSyncValues,
  useSiteSettings,
} from './site-core'

export function BrandingSection() {
  const { data, error, flash, save, load } = useSiteSettings('branding')
  const { v, up } = useSyncValues(data, d => ({
    navTitle: str0((d as any).navTitle),
    leftFooter: str0((d as any).leftFooter),
    rightFooter: str0((d as any).rightFooter),
  }))
  if (!data && !error) return <PageLoading label="Loading branding…" />
  if (error && !data) return <Group><Text size="sm" c="red">Branding: {error}</Text><Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor></Group>
  return (
    <SectionShell
      title="Branding"
      badge="branding"
      description="Navbar title and footer contents."
      footerNote="Changes apply on the next container cycle."
      flash={flash}
      onSave={() => void save({
        navTitle: String(v.navTitle || ''),
        leftFooter: String(v.leftFooter || ''),
        rightFooter: String(v.rightFooter || ''),
      })}
    >
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Navbar title" value={String(v.navTitle || '')} onChange={x => up({ navTitle: x })} placeholder="LibreLeaf" hint="Shown in the top navigation bar." />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Area label="Left footer" width="100%" value={String(v.leftFooter || '')} onChange={x => up({ leftFooter: x })} placeholder='[{"text": "…", "url": "https://…"}]' hint='JSON array of {text,url} items, or plain text.' />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Area label="Right footer" width="100%" value={String(v.rightFooter || '')} onChange={x => up({ rightFooter: x })} rows={2} placeholder='[{"text": "Powered by LibreLeaf", "url": "…"}]' />
      </Group>
    </SectionShell>
  )
}

export function ServicesSection() {
  const { data, error, flash, save, load } = useSiteSettings('services')
  const { v, up } = useSyncValues(data, d => ({
    v1HistoryUrl: str0((d as any).v1HistoryUrl),
    githubInterfaceUrl: str0((d as any).githubInterfaceUrl),
    webdavInterfaceUrl: str0((d as any).webdavInterfaceUrl),
    dropboxInterfaceUrl: str0((d as any).dropboxInterfaceUrl),
    dataManipulatorUrl: str0((d as any).dataManipulatorUrl),
  }))
  if (!data && !error) return <PageLoading label="Loading services…" />
  if (error && !data) return <Group><Text size="sm" c="red">Services: {error}</Text><Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor></Group>
  return (
    <SectionShell
      title="Services"
      badge="internal"
      description="Addresses of the companion services (web → service URLs)."
      footerNote="Changes apply on the next container cycle."
      flash={flash}
      onSave={() => void save({
        v1HistoryUrl: String(v.v1HistoryUrl || ''),
        githubInterfaceUrl: String(v.githubInterfaceUrl || ''),
        webdavInterfaceUrl: String(v.webdavInterfaceUrl || ''),
        dropboxInterfaceUrl: String(v.dropboxInterfaceUrl || ''),
        dataManipulatorUrl: String(v.dataManipulatorUrl || ''),
      })}
    >
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start', width: '100%' }}>
        <Field label="V1 history" value={String(v.v1HistoryUrl || '')} onChange={x => up({ v1HistoryUrl: x })} placeholder="http://overleafserver:3100/api" width="100%" />
        <Field label="GitHub interface" value={String(v.githubInterfaceUrl || '')} onChange={x => up({ githubInterfaceUrl: x })} placeholder="http://localhost:4013" width="100%" />
        <Field label="WebDAV interface" value={String(v.webdavInterfaceUrl || '')} onChange={x => up({ webdavInterfaceUrl: x })} placeholder="http://localhost:4002" width="100%" />
        <Field label="Dropbox interface" value={String(v.dropboxInterfaceUrl || '')} onChange={x => up({ dropboxInterfaceUrl: x })} placeholder="http://localhost:4003" width="100%" />
        <Field label="Data manipulator" value={String(v.dataManipulatorUrl || '')} onChange={x => up({ dataManipulatorUrl: x })} placeholder="http://localhost:4001" width="100%" />
      </Group>
    </SectionShell>
  )
}

export function GrammarSection() {
  const { data, error, flash, save, load } = useSiteSettings('languagetool')
  const { v, up } = useSyncValues(data, d => ({
    enabled: bool0((d as any).enabled, true),
    url: str0((d as any).url),
  }))
  if (!data && !error) return <PageLoading label="Loading grammar settings…" />
  if (error && !data) return <Group><Text size="sm" c="red">Grammar: {error}</Text><Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor></Group>
  return (
    <SectionShell
      title="Grammar (LanguageTool)"
      badge="grammar"
      enabled={Boolean(v.enabled)}
      onEnabled={x => up({ enabled: x })}
      description="LanguageTool grammar checking in the editor."
      footerNote="Changes apply on the next container cycle."
      flash={flash}
      onSave={() => void save({
        enabled: Boolean(v.enabled),
        url: String(v.url || ''),
      })}
    >
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start', width: '100%' }}>
        <Field label="Server URL" value={String(v.url || '')} onChange={x => up({ url: x })} placeholder="http://languagetool:8010" hint="Base URL of the LanguageTool server." width="100%" />
      </Group>
    </SectionShell>
  )
}

export function PandocSection() {
  const { data, error, flash, save, load } = useSiteSettings('pandoc')
  const { v, up } = useSyncValues(data, d => ({
    enabled: bool0((d as any).enabled),
    image: str0((d as any).image, 'pandoc-ol:3.10.0.0'),
  }))
  if (!data && !error) return <PageLoading label="Loading Pandoc settings…" />
  if (error && !data) return <Group><Text size="sm" c="red">Pandoc: {error}</Text><Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor></Group>
  return (
    <SectionShell
      title="Pandoc"
      badge="pandoc"
      enabled={Boolean(v.enabled)}
      onEnabled={x => up({ enabled: x })}
      description="Pandoc image used for Markdown/Word document compilation."
      footerNote="Changes apply on the next container cycle."
      flash={flash}
      onSave={() => void save({
        enabled: Boolean(v.enabled),
        image: String(v.image || ''),
      })}
    >
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start', width: '100%' }}>
        <Field label="Docker image" required value={String(v.image || '')} onChange={x => up({ image: x })} placeholder="pandoc-ol:3.10.0.0" hint="e.g. pandoc-ol:3.10.0.0 — build your own image for custom filters." width="100%" />
      </Group>
    </SectionShell>
  )
}

export function GitSection() {
  const { data, error, flash, save, load } = useSiteSettings('git-integration')
  const { v, up } = useSyncValues(data, d => ({
    enabled: bool0((d as any).enabled),
    host: str0((d as any).host, 'git-bridge'),
    port: String(num0((d as any).port, 8000)),
  }))
  if (!data && !error) return <PageLoading label="Loading git settings…" />
  if (error && !data) return <Group><Text size="sm" c="red">Git: {error}</Text><Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor></Group>
  return (
    <SectionShell
      title="Git integration"
      badge="git"
      enabled={Boolean(v.enabled)}
      onEnabled={x => up({ enabled: x })}
      description="External git bridge for per-project git repositories."
      footerNote="Changes apply on the next container cycle."
      flash={flash}
      onSave={() => void save({
        enabled: Boolean(v.enabled),
        host: String(v.host || ''),
        port: num0(v.port, 8000),
      })}
    >
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Host" required value={String(v.host || '')} onChange={x => up({ host: x })} placeholder="git-bridge" />
        <Field label="Port" required value={String(v.port || '')} onChange={x => up({ port: x.replace(/[^\d]/g, '') })} placeholder="8000" width="33%" />
      </Group>
    </SectionShell>
  )
}

export function GithubSection() {
  const { data, error, flash, save, load } = useSiteSettings('github-sync')
  const { v, up } = useSyncValues(data, d => ({
    enabled: bool0((d as any).enabled),
    clientId: str0((d as any).clientId || (d as any).clientID),
    clientSecret: '',
    cipherFile: str0((d as any).cipherFile),
    cipherLabel: str0((d as any).cipherLabel),
    advanced: Boolean((d as any).cipherFile || (d as any).cipherLabel),
  }))
  const [showAdv, setShowAdv] = React.useState(Boolean((data as any)?.cipherFile) || Boolean((data as any)?.cipherLabel))
  const secretSet = Boolean((data as any)?.clientSecretSet)
  if (!data && !error) return <PageLoading label="Loading GitHub sync settings…" />
  if (error && !data) return <Group><Text size="sm" c="red">GitHub sync: {error}</Text><Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor></Group>
  return (
    <SectionShell
      title="GitHub sync"
      badge="github"
      enabled={Boolean(v.enabled)}
      onEnabled={x => up({ enabled: x })}
      description="Sync projects with GitHub repositories via OAuth."
      footerNote="Changes apply on the next container cycle."
      flash={flash}
      onSave={() => void save({
        enabled: Boolean(v.enabled),
        clientId: String(v.clientId || ''),
        clientSecret: String(v.clientSecret || ''),
        cipherFile: String(v.cipherFile || ''),
        cipherLabel: String(v.cipherLabel || ''),
      })}
    >
      <SectionTitle top>OAuth App</SectionTitle>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Client ID" required value={String(v.clientId || '')} onChange={x => up({ clientId: x })} />
        <SecretField label="Client secret" set={Boolean(secretSet)} value={String(v.clientSecret || '')} onChange={x => up({ clientSecret: x })} hint="Leave empty to keep the stored secret." />
      </Group>
      <Anchor href="#" size="sm" onClick={e => { e.preventDefault(); setShowAdv(a => !a) }}>
        {showAdv ? '−' : '+'} Advanced (encryption cache)
      </Anchor>
      {showAdv ? (
        <Group wrap="wrap" gap="md" mb="xs" mt={8} style={{ alignItems: 'flex-start' }}>
          <Field label="Cipher file" value={String(v.cipherFile || '')} onChange={x => up({ cipherFile: x })} />
          <Field label="Cipher label" value={String(v.cipherLabel || '')} onChange={x => up({ cipherLabel: x })} hint="Key label used by the encryption cache." />
        </Group>
      ) : null}
      <Group gap="xs" wrap="wrap" mt="xs">
        <Text size="xs" c="dimmed">OAuth callback: https://&lt;instance&gt;/user/github-sync/oauth2/callback</Text>
      </Group>
    </SectionShell>
  )
}

const LINKED_TYPES = [
  { key: 'project_file', locked: true, label: 'Project files' },
  { key: 'project_output_file', locked: true, label: 'Project output files' },
  { key: 'url', locked: false, label: 'URLs' },
  { key: 'zotero', locked: false, label: 'Zotero items' },
]

export function LinkedFileTypesSection() {
  const { data, error, flash, save, load } = useSiteSettings('linked-file-types')
  const { v, up } = useSyncValues(data, d => {
    const types: string[] = Array.isArray((d as any).enabledTypes) ? (d as any).enabledTypes : []
    return {
      enabledTypes: ['project_file', 'project_output_file'].concat(
        types.filter(k => k !== 'project_file' && k !== 'project_output_file')
      ),
    }
  })
  const types: string[] = Array.isArray(v.enabledTypes) ? (v.enabledTypes as string[]) : []
  if (!data && !error) return <PageLoading label="Loading linked file types…" />
  if (error && !data) return <Group><Text size="sm" c="red">Linked file types: {error}</Text><Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor></Group>
  return (
    <SectionShell
      title="Linked file types"
      badge="files"
      description="File types that projects can link."
      footerNote="Changes apply on the next container cycle."
      flash={flash}
      onSave={() => void save({
        enabledTypes: ['project_file', 'project_output_file']
          .concat(types.filter(k => k !== 'project_file' && k !== 'project_output_file')),
      })}
    >
      <Group wrap="wrap" gap="sm" mb="xs">
        {LINKED_TYPES.map(row => (
          <Checkbox
            key={row.key}
            label={row.label + (row.locked ? ' (always on)' : '')}
            checked={types.includes(row.key) || row.locked}
            disabled={row.locked}
            onChange={e => {
              const on = e.currentTarget.checked
              const cur = Array.isArray(v.enabledTypes) ? (v.enabledTypes as string[]) : []
              const next = on
                ? Array.from(new Set([...cur, row.key]))
                : cur.filter(k => k !== row.key)
              up({ enabledTypes: next })
            }}
            color="ol"
          />
        ))}
      </Group>
    </SectionShell>
  )
}

export function WebdavSection() {
  const { data, error, flash, save, load } = useSiteSettings('webdav')
  const { v, up } = useSyncValues(data, d => ({
    enabled: bool0((d as any).enabled),
    rootPath: str0((d as any).rootPath, '/Overleaf'),
    requestTimeoutMs: String(num0((d as any).requestTimeoutMs, 60000)),
    retryCount: String(num0((d as any).retryCount, 2)),
    retryDelayMs: String(num0((d as any).retryDelayMs, 500)),
    cipherLabel: str0((d as any).cipherLabel),
    cipherPassword: '',
  }))
  const [showAdv, setShowAdv] = React.useState(Boolean((data as any)?.cipherLabel))
  const passSet = Boolean((data as any)?.cipherPasswordSet)
  if (!data && !error) return <PageLoading label="Loading WebDAV settings…" />
  if (error && !data) return <Group><Text size="sm" c="red">WebDAV: {error}</Text><Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor></Group>
  return (
    <SectionShell
      title="WebDAV"
      badge="webdav"
      enabled={Boolean(v.enabled)}
      onEnabled={x => up({ enabled: x })}
      description="WebDAV external drive integration."
      footerNote="Changes apply on the next container cycle."
      flash={flash}
      onSave={() => void save({
        enabled: Boolean(v.enabled),
        rootPath: String(v.rootPath || ''),
        requestTimeoutMs: num0(v.requestTimeoutMs, 60000),
        retryCount: num0(v.retryCount, 2),
        retryDelayMs: num0(v.retryDelayMs, 500),
        cipherLabel: String(v.cipherLabel || ''),
        cipherPassword: String(v.cipherPassword || ''),
      })}
    >
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Root path" required value={String(v.rootPath || '')} onChange={x => up({ rootPath: x })} placeholder="/Overleaf" />
        <Field label="Request timeout (ms)" value={String(v.requestTimeoutMs || '')} onChange={x => up({ requestTimeoutMs: x.replace(/[^\d]/g, '') })} placeholder="60000" width="30%" />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Retries" value={String(v.retryCount || '')} onChange={x => up({ retryCount: x.replace(/[^\d]/g, '') })} placeholder="2" width="30%" />
        <Field label="Retry delay (ms)" value={String(v.retryDelayMs || '')} onChange={x => up({ retryDelayMs: x.replace(/[^\d]/g, '') })} placeholder="500" width="30%" />
      </Group>
      <Anchor href="#" size="sm" onClick={e => { e.preventDefault(); setShowAdv(a => !a) }}>
        {showAdv ? '−' : '+'} Advanced (encryption cache)
      </Anchor>
      {showAdv ? (
        <Group wrap="wrap" gap="md" mb="xs" mt={8} style={{ alignItems: 'flex-start' }}>
          <Field label="Cipher label" value={String(v.cipherLabel || '')} onChange={x => up({ cipherLabel: x })} placeholder="OL_WEBDAV2-v3" />
          <SecretField label="Cipher password" set={Boolean(passSet)} value={String(v.cipherPassword || '')} onChange={x => up({ cipherPassword: x })} hint="Leave empty to keep the stored password." />
        </Group>
      ) : null}
    </SectionShell>
  )
}

export function DropboxSection() {
  const { data, error, flash, save, load } = useSiteSettings('dropbox')
  const { v, up } = useSyncValues(data, d => ({
    enabled: bool0((d as any).enabled),
    appKey: str0((d as any).appKey),
    appSecret: '',
  }))
  const secretSet = Boolean((data as any)?.appSecretSet)
  if (!data && !error) return <PageLoading label="Loading Dropbox settings…" />
  if (error && !data) return <Group><Text size="sm" c="red">Dropbox: {error}</Text><Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor></Group>
  return (
    <SectionShell
      title="Dropbox"
      badge="dropbox"
      enabled={Boolean(v.enabled)}
      onEnabled={x => up({ enabled: x })}
      description="Dropbox external drive integration."
      footerNote="Changes apply on the next container cycle."
      flash={flash}
      onSave={() => void save({
        enabled: Boolean(v.enabled),
        appKey: String(v.appKey || ''),
        appSecret: String(v.appSecret || ''),
      })}
    >
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="App key" required value={String(v.appKey || '')} onChange={x => up({ appKey: x })} placeholder="xs8q2ebd8qrmhuu" />
        <SecretField label="App secret" set={Boolean(secretSet)} value={String(v.appSecret || '')} onChange={x => up({ appSecret: x })} hint="Leave empty to keep the stored secret." />
      </Group>
    </SectionShell>
  )
}
