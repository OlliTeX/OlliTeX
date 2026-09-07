import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Anchor,
  AppShell,
  Button,
  Card,
  Group,
  NativeSelect,
  Stack,
  Switch,
  Text,
  TextInput,
  Textarea,
  UnstyledButton,
  Box,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { getJSON, putJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import { PageError, PageLoading } from '../../shared/page-state'
// Legacy (ce-admin-ui) section components — self-contained save hooks; to be
// restyled one-by-one (email & branding first per owner).
import {
  SandboxedCompilesTab,
  GitIntegrationTab,
  GithubSyncTab,
  EmailTab,
  LinkedFileTypesTab,
  PandocTab,
  WebdavTab,
  DropboxTab,
  LanguagetoolTab,
  BrandingTab,
  ServicesTab,
  MiscTab,
} from '../../../../../admin-tools/frontend/js/site-settings/components/r9-settings-tabs'
import {
  SamlSsoTab,
  OidcSsoTab,
  LdapSsoTab,
} from '../../../../../admin-tools/frontend/js/site-settings/components/sso-settings-tab'

type Sec = Record<string, any>

const NAV: { id: string; label: string; icon: string; group: string }[] = [
  { id: 'misc', label: 'Miscellaneous', icon: 'tune', group: 'General' },
  { id: 'signup', label: 'Sign-up', icon: 'person_add', group: 'General' },
  { id: 'templates', label: 'Template gallery', icon: 'draft', group: 'General' },
  { id: 'zotero', label: 'Zotero', icon: 'auto_stories', group: 'Integrations' },
  { id: 'externalUrl', label: 'External URLs', icon: 'link', group: 'Integrations' },
  { id: 'sso-saml', label: 'SSO · SAML', icon: 'verified_user', group: 'Integrations' },
  { id: 'sso-oidc', label: 'SSO · OIDC', icon: 'badge', group: 'Integrations' },
  { id: 'sso-ldap', label: 'SSO · LDAP', icon: 'groups', group: 'Integrations' },
  { id: 'email', label: 'Email / SMTP', icon: 'mail', group: 'Services' },
  { id: 'services', label: 'Services', icon: 'dns', group: 'Services' },
  { id: 'branding', label: 'Branding', icon: 'palette', group: 'Services' },
  { id: 'languagetool', label: 'Grammar (LT)', icon: 'spellcheck', group: 'Services' },
  { id: 'sandboxed-compiles', label: 'Sandboxed compiles', icon: 'build', group: 'Compilation' },
  { id: 'pandoc', label: 'Pandoc', icon: 'convert', group: 'Compilation' },
  { id: 'git-integration', label: 'Git integration', icon: 'commit', group: 'Compilation' },
  { id: 'github-sync', label: 'GitHub sync', icon: 'cloud_sync', group: 'Compilation' },
  { id: 'linked-file-types', label: 'Linked file types', icon: 'attachment', group: 'Compilation' },
  { id: 'webdav', label: 'WebDAV', icon: 'cloud', group: 'Compilation' },
  { id: 'dropbox', label: 'Dropbox', icon: 'cloud_done', group: 'Compilation' },
]

const ALIAS: Record<string, string[]> = {
  externalUrl: ['externalUrl', 'external-url', 'externalUrl'],
  'sso-saml': ['sso-saml', 'ssoSaml'],
  'sso-oidc': ['sso-oidc', 'ssoOidc'],
  'sso-ldap': ['sso-ldap', 'ssoLdap'],
  'sandboxed-compiles': ['sandboxed-compiles', 'sandboxedCompiles'],
  'git-integration': ['git-integration', 'gitIntegration'],
  'github-sync': ['github-sync', 'githubSync'],
  'linked-file-types': ['linked-file-types', 'linkedFileTypes'],
  languagetool: ['languagetool', 'languageTool'],
}

function pick(all: Sec, id: string): Sec {
  const keys = ALIAS[id] || [id]
  for (const k of keys) if (all[k] && typeof all[k] === 'object') return all[k]
  return {}
}

function Classic({ children, note }: { children: React.ReactNode; note?: string }) {
  return (
    <Box
      style={{
        border: '1px solid var(--mantine-color-default-border)',
        borderRadius: 10,
        padding: '20px 22px',
        background: 'var(--mantine-color-body)',
      }}
    >
      {note ? (
        <Group gap={8} mb="md" wrap="nowrap">
          <Text size="xs" c="dimmed">
            Classic controls — will be restyled to the OlliTeX kit in a follow-up.
          </Text>
        </Group>
      ) : null}
      {children}
    </Box>
  )
}

/* ─────────────── Mantine-native section forms ─────────────── */

function MiscNative({ initial, onChange }: { initial: Sec; onChange: (patch: Sec) => void }) {
  const [appName, setAppName] = useState(initial.appName || 'OlliTeX')
  const [hidePoweredBy, setHidePoweredBy] = useState(initial.hidePoweredBy === true)
  const [noindex, setNoindex] = useState(initial.noindex === true)
  const [allowPublic, setAllowPublic] = useState(initial.allowPublic !== false)
  const [anonRW, setAnonRW] = useState(initial.anonRW === true)
  const [disableLink, setDisableLink] = useState(initial.disableLinkSharing === true)
  const [pythonRunner, setPythonRunner] = useState(initial.pythonRunner === true)
  const [historyRestore, setHistoryRestore] = useState(initial.historyRestore === true)
  const [maxUpload, setMaxUpload] = useState(String(initial.maxUploadSizeMiB ?? 50))
  const [maxEntities, setMaxEntities] = useState(String(initial.maxEntitiesPerProject ?? 2000))
  const [compiler, setCompiler] = useState(initial.defaultLatexCompiler || 'pdflatex')
  const [notifDelay, setNotifDelay] = useState(String(initial.projectChangeNotificationDelayMs ?? 30000))

  useEffect(() => {
    onChange({
      appName,
      hidePoweredBy,
      noindex,
      allowPublic,
      anonRW,
      disableLinkSharing: disableLink,
      pythonRunner,
      historyRestore,
      maxUploadSizeMiB: parseInt(maxUpload, 10),
      maxEntitiesPerProject: parseInt(maxEntities, 10),
      defaultLatexCompiler: compiler,
      projectChangeNotificationDelayMs: parseInt(notifDelay, 10) || 0,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [appName, hidePoweredBy, noindex, allowPublic, anonRW, disableLink, pythonRunner, historyRestore, maxUpload, maxEntities, compiler, notifDelay])

  return (
    <Stack gap="md">
      <Text fw={700}>Branding</Text>
      <Group gap="md" wrap="wrap">
        <div style={{ flex: 1, minWidth: 260 }}>
          <Text size="sm" fw={600} mb={6}>
            Product name
          </Text>
          <TextInput value={appName} onChange={e => setAppName(e.currentTarget.value)} />
        </div>
        <Group gap="lg">
          <Switch checked={hidePoweredBy} onChange={setHidePoweredBy} label="Hide attribution" color="ollitex" />
          <Switch checked={noindex} onChange={setNoindex} label="Block indexing (noindex)" color="ollitex" />
        </Group>
      </Group>

      <Text fw={700}>Access</Text>
      <Group gap="lg" wrap="wrap">
        <Switch checked={allowPublic} onChange={setAllowPublic} label="Allow public projects" color="ollitex" />
        <Switch checked={anonRW} onChange={setAnonRW} label="Anonymous read/write links" color="ollitex" />
        <Switch checked={disableLink} onChange={setDisableLink} label="Disable link sharing" color="ollitex" />
      </Group>

      <Text fw={700}>Features</Text>
      <Group gap="lg" wrap="wrap">
        <Switch checked={pythonRunner} onChange={setPythonRunner} label="Python split editor (browser)" color="ollitex" />
        <Switch checked={historyRestore} onChange={setHistoryRestore} label="Restore history on open" color="ollitex" />
      </Group>

      <Text fw={700}>Limits & defaults</Text>
      <Group gap="md" wrap="wrap">
        <div style={{ minWidth: 180 }}>
          <Text size="sm" fw={600} mb={6}>
            Max upload (MiB)
          </Text>
          <TextInput value={maxUpload} onChange={e => setMaxUpload(e.currentTarget.value)} />
        </div>
        <div style={{ minWidth: 180 }}>
          <Text size="sm" fw={600} mb={6}>
            Max entities per project
          </Text>
          <TextInput value={maxEntities} onChange={e => setMaxEntities(e.currentTarget.value)} />
        </div>
        <div style={{ minWidth: 180 }}>
          <Text size="sm" fw={600} mb={6}>
            Default compiler
          </Text>
          <NativeSelect
            value={compiler}
            onChange={e => setCompiler(e.currentTarget.value)}
            data={['pdflatex', 'lualatex', 'xelatex', 'context']}
          />
        </div>
        <div style={{ minWidth: 180 }}>
          <Text size="sm" fw={600} mb={6}>
            Change-notification delay (ms)
          </Text>
          <TextInput value={notifDelay} onChange={e => setNotifDelay(e.currentTarget.value)} />
        </div>
      </Group>
    </Stack>
  )
}

function SignupNative({ initial, onChange }: { initial: Sec; onChange: (patch: Sec) => void }) {
  const [enabled, setEnabled] = useState(initial.enabled !== false)
  const [domains, setDomains] = useState((initial.allowedDomains || []).join('\n'))
  useEffect(() => {
    onChange({
      enabled,
      allowedDomains: domains
        .split(/[\n,]/)
        .map(s => s.trim())
        .filter(Boolean),
      requireEmailConfirmation: initial.requireEmailConfirmation !== false,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, domains])
  return (
    <Stack gap="md">
      <Group justify="space-between" wrap="nowrap">
        <div>
          <Text fw={700}>Open registration</Text>
          <Text size="sm" c="dimmed" mt={4}>
            Allow new accounts to be created on this instance.
          </Text>
        </div>
        <Switch checked={enabled} onChange={setEnabled} color="ollitex" />
      </Group>
      <div>
        <Text size="sm" fw={600} mb={6}>
          Allowed email domains
          <Text span size="xs" c="dimmed" fw={400}>
            {' '}
            (one per line — empty means all domains)
          </Text>
        </Text>
        <Textarea value={domains} onChange={e => setDomains(e.currentTarget.value)} minRows={3} maxRows={8} placeholder={'uni-bremen.de\nexample.org'} />
      </div>
    </Stack>
  )
}

function ZoteroNative({ initial, onChange }: { initial: Sec; onChange: (patch: Sec) => void }) {
  const [enabled, setEnabled] = useState(initial.enabled === true)
  const [appId, setAppId] = useState(initial.clientId || '')
  const [appSecret, setAppSecret] = useState(initial.clientSecret || '')
  useEffect(() => {
    onChange({ enabled, clientId: appId, clientSecret: appSecret })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, appId, appSecret])
  return (
    <Stack gap="md">
      <Group justify="space-between" wrap="nowrap">
        <div>
          <Text fw={700}>Zotero connector</Text>
          <Text size="sm" c="dimmed" mt={4}>
            Lets users import references from their Zotero library.
          </Text>
        </div>
        <Switch checked={enabled} onChange={setEnabled} color="ollitex" />
      </Group>
      <Group gap="md" wrap="wrap">
        <div style={{ flex: 1, minWidth: 220 }}>
          <Text size="sm" fw={600} mb={6}>
            Client ID
          </Text>
          <TextInput value={appId} onChange={e => setAppId(e.currentTarget.value)} placeholder="zotero-web-application-client-id" />
        </div>
        <div style={{ flex: 1, minWidth: 220 }}>
          <Text size="sm" fw={600} mb={6}>
            Client secret
          </Text>
          <TextInput type="password" value={appSecret} onChange={e => setAppSecret(e.currentTarget.value)} />
        </div>
      </Group>
    </Stack>
  )
}

function ExternalUrlsNative({ initial, onChange }: { initial: Sec; onChange: (patch: Sec) => void }) {
  const [enabled, setEnabled] = useState(initial.enabled !== false)
  const [blocked, setBlocked] = useState((initial.blockedCidrList || []).join('\n') || (initial.blockedDomains || []).join('\n'))
  const [allowed, setAllowed] = useState(initial.allowedResourcesRegex || '')
  useEffect(() => {
    onChange({
      enabled,
      blockedCidrList: blocked.split(/[\n,]/).map(s => s.trim()).filter(Boolean),
      allowedResourcesRegex: allowed || undefined,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, blocked, allowed])
  return (
    <Stack gap="md">
      <Group justify="space-between" wrap="nowrap">
        <div>
          <Text fw={700}>Linked URL import</Text>
          <Text size="sm" c="dimmed" mt={4}>
            Allow importing content from arbitrary URLs into projects (SSRF-guarded).
          </Text>
        </div>
        <Switch checked={enabled} onChange={setEnabled} color="ollitex" />
      </Group>
      <div>
        <Text size="sm" fw={600} mb={6}>
          Blocked CIDRs / domains (one per line)
        </Text>
        <Textarea value={blocked} onChange={e => setBlocked(e.currentTarget.value)} minRows={2} maxRows={6} placeholder={'10.0.0.0/8\n172.16.0.0/12'} />
      </div>
      <div>
        <Text size="sm" fw={600} mb={6}>
          Allowed-resources regex (optional)
        </Text>
        <TextInput value={allowed} onChange={e => setAllowed(e.currentTarget.value)} placeholder="^https?://.*\.(pdf|zip)$" />
      </div>
    </Stack>
  )
}

/* ─────────────── section frame + root ─────────────── */

function NativeCard({
  title,
  onSave,
  saving,
  children,
}: {
  title: string
  onSave: () => void
  saving: boolean
  children: React.ReactNode
}) {
  return (
    <Card withBorder paddings="lg" radius="lg">
      <Stack gap="md">
        {children}
        <Group justify="flex-end">
          <Button color="ollitex" size="md" loading={saving} onClick={onSave}>
            Save {title.toLowerCase()}
          </Button>
        </Group>
      </Stack>
    </Card>
  )
}

export default function AdminSiteSection({
  fixedSection,
  hideNav,
}: {
  fixedSection?: string
  hideNav?: boolean
} = {}) {
  const [settings, setSettings] = useState<Sec | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [active, setActive] = useState(fixedSection || 'misc')
  const [drafts, setDrafts] = useState<Record<string, Sec>>({})
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    setError(null)
    try {
      const all = await getJSON('/admin/site-settings')
      setSettings(all || {})
    } catch (err: any) {
      setError((err?.data?.message as string) || String(err?.message || err))
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const sectionItems = useMemo(() => {
    const g: Record<string, { id: string; label: string; icon: string }[]> = {}
    for (const n of NAV) {
      (g[n.group] = g[n.group] || []).push({ id: n.id, label: n.label, icon: n.icon })
    }
    return g
  }, [])

  const saveNative = async (id: string) => {
    const patch = drafts[id]
    if (!patch) return
    setSaving(true)
    try {
      const body = pick(settings || {}, id)
      Object.assign(body, patch)
      await putJSON(`/admin/site-settings/${id}`, { body })
      if (settings) setSettings({ ...settings, [id]: body })
      setDrafts(d => ({ ...d, [id]: {} }))
      notifications.show({ message: 'Saved.', color: 'teal' })
    } catch (err: any) {
      notifications.show({
        message: (err?.data?.message as string) || 'Could not save the section.',
        color: 'red',
      })
    } finally {
      setSaving(false)
    }
  }

  const meta = NAV.find(n => n.id === active)
  const initial = settings ? pick(settings, active) : {}

  const renderContent = () => {
    switch (active) {
      case 'misc':
        return (
          <NativeCard title="settings" saving={saving} onSave={() => void saveNative('misc')}>
            <MiscNative
              initial={(settings && pick(settings, 'misc')) || {}}
              onChange={p => setDrafts(d => ({ ...d, misc: { ...(d.misc || {}), ...p } }))}
            />
          </NativeCard>
        )
      case 'signup':
        return (
          <NativeCard title="settings" saving={saving} onSave={() => void saveNative('signup')}>
            <SignupNative
              initial={(settings && pick(settings, 'signup')) || {}}
              onChange={p => setDrafts(d => ({ ...d, signup: { ...(d.signup || {}), ...p } }))}
            />
          </NativeCard>
        )
      case 'zotero':
        return (
          <NativeCard title="settings" saving={saving} onSave={() => void saveNative('zotero')}>
            <ZoteroNative
              initial={(settings && pick(settings, 'zotero')) || {}}
              onChange={p => setDrafts(d => ({ ...d, zotero: { ...(d.zotero || {}), ...p } }))}
            />
          </NativeCard>
        )
      case 'externalUrl':
        return (
          <NativeCard title="settings" saving={saving} onSave={() => void saveNative('externalUrl')}>
            <ExternalUrlsNative
              initial={(settings && pick(settings, 'externalUrl')) || {}}
              onChange={p => setDrafts(d => ({ ...d, externalUrl: { ...(d.externalUrl || {}), ...p } }))}
            />
          </NativeCard>
        )
      case 'templates':
        return (
          <Card withBorder paddings="lg" radius="lg">
            <Stack gap="sm">
              <Text fw={700}>Template gallery</Text>
              <Text size="sm" c="dimmed">
                Gallery visibility, categories, bundles, and imports live in the dedicated
                Templates section of this hub.
              </Text>
              <Group gap="xs">
                <Anchor href="#templates" size="sm">
                  Go to Templates →
                </Anchor>
              </Group>
              <Text size="xs" c="dimmed">
                (Templates is a top-level item in this hub — bundle upload, imports, and the
                public gallery switch all live there.)
              </Text>
            </Stack>
          </Card>
        )
      case 'sso-saml':
        return (
          <Classic>
            <SamlSsoTab key={`saml-${initial.enabled}`} section={initial} />
          </Classic>
        )
      case 'sso-oidc':
        return (
          <Classic>
            <OidcSsoTab key={`oidc-${initial.enabled}`} section={initial} />
          </Classic>
        )
      case 'sso-ldap':
        return (
          <Classic>
            <LdapSsoTab key={`ldap-${initial.enabled}`} section={initial} />
          </Classic>
        )
      case 'sandboxed-compiles':
        return (
          <Classic>
            <SandboxedCompilesTab initial={initial} />
          </Classic>
        )
      case 'git-integration':
        return (
          <Classic>
            <GitIntegrationTab initial={initial} />
          </Classic>
        )
      case 'github-sync':
        return (
          <Classic>
            <GithubSyncTab initial={initial} />
          </Classic>
        )
      case 'email':
        return (
          <Classic>
            <EmailTab initial={initial} />
          </Classic>
        )
      case 'linked-file-types':
        return (
          <Classic>
            <LinkedFileTypesTab initial={initial} />
          </Classic>
        )
      case 'pandoc':
        return (
          <Classic>
            <PandocTab initial={initial} />
          </Classic>
        )
      case 'webdav':
        return (
          <Classic>
            <WebdavTab initial={initial} />
          </Classic>
        )
      case 'dropbox':
        return (
          <Classic>
            <DropboxTab initial={initial} />
          </Classic>
        )
      case 'languagetool':
        return (
          <Classic>
            <LanguagetoolTab initial={initial} />
          </Classic>
        )
      case 'branding':
        return (
          <Classic>
            <BrandingTab initial={initial} />
          </Classic>
        )
      case 'services':
        return (
          <Classic>
            <ServicesTab initial={initial} />
          </Classic>
        )
      default:
        return <Text size="sm">Section not found.</Text>
    }
  }

  if (error)
    return (
      <PageError
        label="Couldn’t load site settings"
        detail={error}
        onRetry={() => void load()}
      />
    )
  if (!settings) return <PageLoading label="Loading site settings…" />

  return (
    <div style={{ display: 'flex', minHeight: 520, alignItems: 'stretch' }}>
      {!hideNav ? (
      <nav
        aria-label="Site settings sections"
        style={{
          width: 248,
          flexShrink: 0,
          overflowY: 'auto',
          overscrollBehavior: 'contain',
          maxHeight: 'calc(100vh - 120px)',
          padding: '4px 6px',
          borderRight: '1px solid var(--mantine-color-border)',
          background: 'var(--mantine-color-body)',
        }}
      >
        <Stack gap="xs">
          {Object.entries(sectionItems).map(([group, items]) => (
            <div key={group}>
              <Text size="xs" tt="uppercase" fw={700} c="dimmed" mt="md" mb={6} pl={8} style={{ letterSpacing: '0.08em' }}>
                {group}
              </Text>
              {items.map(it => (
                <UnstyledButton
                  key={it.id}
                  onClick={() => setActive(it.id)}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 10,
                    width: '100%',
                    padding: '8px 10px',
                    borderRadius: 9,
                    fontSize: 13.5,
                    background: active === it.id ? 'var(--mantine-color-ollitex-6)' : 'transparent',
                    color: active === it.id ? 'var(--mantine-color-white)' : 'var(--mantine-color-text)',
                    transition: 'background 120ms ease',
                  }}
                  onMouseEnter={e => {
                    if (active !== it.id) (e.currentTarget as HTMLButtonElement).style.background = 'var(--mantine-color-default-hover)'
                  }}
                  onMouseLeave={e => {
                    if (active !== it.id) (e.currentTarget as HTMLButtonElement).style.background = 'transparent'
                  }}
                >
                  <Icon
                    name={it.icon}
                    size={18}
                    style={{ color: active === it.id ? 'var(--mantine-color-white)' : 'var(--mantine-color-dimmed)' }}
                  />
                  {it.label}
                </UnstyledButton>
              ))}
            </div>
          ))}
        </Stack>
      </nav>
      ) : null}
      <main style={{ flex: 1, minWidth: 0, overflowY: 'auto', maxHeight: 'calc(100vh - 120px)', padding: 12, background: 'var(--mantine-color-body)' }}>
        <Stack gap="md" maw={920}>
          <Text fw={700} size="lg">
            {meta?.label}
          </Text>
          {renderContent()}
        </Stack>
      </main>
    </div>
  )
}
