import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Badge,
  Button,
  Checkbox,
  Group,
  Menu,
  Modal,
  NativeSelect,
  Stack,
  Table,
  Text,
  TextInput,
} from '@mantine/core'
import {
  ActionIcon,
  Tooltip,
} from '@mantine/core'
import { Dashboard as UppyDashboard } from '@uppy/react'
import '@uppy/core/dist/style.css'
import '@uppy/dashboard/dist/style.css'
import { useProjectUploader } from '@/features/project-list/hooks/use-project-uploader'
import { notifications } from '@mantine/notifications'
import { postJSON, getJSON, deleteJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import ConfirmModal from '../../shared/confirm-modal'
import { EmptyState, PageError, PageLoading } from '../../shared/page-state'

/**
 * Owner #10a–#10f (2026-09-07): the Projects leaf is the reworked project
 * list — a proper LIST (the old card grid went):
 *   #10a  list view: search, name/owner/updated columns, 25-per-page
 *   #10b  row actions: Open · Copy (clone) · Download (.zip) ·
 *         Archive/Un-archive · Move to trash / Restore · Add to tag · Leave
 *   #10c  multi-select + toolbar: Download (one .zip) · Archive/Un-archive ·
 *         Move to trash · Restore · Add to tag
 *   #10d  trashed view (own leaf) with Restore
 *   #10e  New project: Blank · From template · From GitHub · .zip · .docx ·
 *         .md — all as Mantine modals
 *   #10f  the zip/docx/md upload modals reworked as Mantine chrome (the
 *         proven Uppy engine stays inside)
 * Endpoints (verified against this build's router):
 *   POST /api/project {filters: {ownedByUser,sharedWithUser,archived,trashed,tag}, sort}
 *   POST /Project/:id/clone {projectName}            → {project_id}
 *   GET  /Project/:id/download/zip                   (single)
 *   GET  /project/download/zip?project_ids=a,b       (bulk)
 *   POST|DELETE /project/:id/archive · /project/:id/trash
 *   POST /project/:id/leave · /tag · /tag/:id/projects · .../projects/remove
 *   POST /project/new {projectName, template}
 *   POST /project/new/upload (uppy) · /project/new/import-document?type=
 *   GET  /user/github-sync/status · /user/github-sync/repos?provider=...
 *   POST /project/new/github-sync {...repo, provider, serverUrl, username}
 */

type Project = {
  _id?: string
  id: string
  name: string
  owner?: { id?: string; email: string; firstName?: string; lastName?: string }
  lastUpdated: string
  accessLevel?: 'owner' | 'readWrite' | 'readOnly' | 'review'
  archived?: boolean
  trashed?: boolean
}

type View = 'all' | 'owned' | 'shared' | 'archived' | 'trashed'

const PAGE_SIZE = 25

function timeAgo(iso: string): string {
  try {
    const t = new Date(iso).getTime()
    if (Number.isNaN(t)) return '—'
    const s = Math.max(1, Math.floor((Date.now() - t) / 1000))
    if (s < 60) return 'just now'
    const m = Math.floor(s / 60)
    if (m < 60) return `${m} min ago`
    const h = Math.floor(m / 60)
    if (h < 24) return `${h} h ago`
    const d = Math.floor(h / 24)
    if (d < 30) return `${d} d ago`
    const mo = Math.floor(d / 30)
    if (mo < 12) return `${mo} mo ago`
    return `${Math.floor(mo / 12)} y ago`
  } catch {
    return '—'
  }
}

function personName(u?: { email: string; firstName?: string; lastName?: string }): string {
  if (!u) return ''
  const n = `${u.firstName || ''} ${u.lastName || ''}`.trim()
  return n || u.email
}

const pid = (p: Project) => p._id || p.id

// ─────────────────────────── Uppy-in-Mantine upload ──────────────────────
function UppyUploadModal({
  open,
  title,
  accept,
  endpoint,
  browseLabel,
  dragLabel,
  modalId,
  onCancel,
  onDone,
}: {
  open: boolean
  title: string
  accept: string[]
  endpoint: string
  browseLabel: string
  dragLabel: string
  modalId?: string
  onCancel: () => void
  onDone: (projectId: string, convertedFrom?: string) => void
}) {
  const convertedFrom = endpoint.includes('import-document')
    ? (endpoint.match(/type=(\w+)/)?.[1] || undefined)
    : undefined
  const uppy = useProjectUploader({
    endpoint,
    allowedFileTypes: accept,
    onSuccess: (projectId: string) => onDone(projectId, convertedFrom),
    onError: (response: any) => {
      const message = response?.body?.error || 'Upload failed.'
      notifications.show({ message, color: 'red' })
    },
  })
  if (!open) return null
  return (
    <Modal opened onClose={onCancel} size="lg" title={<Text fw={700}>{title}</Text>} withinPortal id={modalId}>
      <Stack gap="md">
        <UppyDashboard
          uppy={uppy}
          proudlyDisplayPoweredByUppy={false}
          showLinkToFileUploadResult={false}
          hideUploadButton
          showSelectedFiles={false}
          height={280}
          locale={{
            strings: {
              browseFiles: browseLabel,
              dropPasteFiles: dragLabel,
            },
          }}
        />
        <Group justify="flex-end">
          <Button variant="default" onClick={onCancel} disabled={Boolean(uppy && Array.isArray(uppy.files) && uppy.files.some((f: any) => (f.progress?.upload ?? 0) > 0 && f.progress.upload < 100))}>
            Cancel
          </Button>
        </Group>
      </Stack>
    </Modal>
  )
}

// ─────────────────────────────── GitHub import ───────────────────────────
type GitServer = { id: string; provider: string; url: string; username?: string; name?: string }
type GitRepo = { name: string; fullName: string; defaultBranchName?: string }

function GithubImportModal({
  open,
  onClose,
}: {
  open: boolean
  onClose: () => void
}) {
  const [providers, setProviders] = useState<GitServer[]>([])
  const [providerId, setProviderId] = useState('')
  const [repos, setRepos] = useState<GitRepo[] | null>(null)
  const [importing, setImporting] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setProviders([])
    setProviderId('')
    setRepos(null)
    setImporting(null)
    setError(null)
    getJSON<{ providers?: GitServer[] }>('/user/github-sync/status')
      .then(res => {
        const list = Array.isArray(res?.providers) ? res.providers : []
        setProviders(list)
        if (list.length) setProviderId(list[0].id)
      })
      .catch(err => {
        setProviders([])
        setError((err?.data?.message as string) || 'Could not read GitHub connections.')
      })
  }, [open])

  const selected = providers.find(p => p.id === providerId) || null

  useEffect(() => {
    if (!open) return
    if (!selected) {
      setRepos(null)
      return
    }
    setRepos(null)
    setError(null)
    const qs = new URLSearchParams({
      provider: selected.provider,
      serverUrl: selected.url,
    })
    if (selected.username) qs.set('username', selected.username)
    getJSON<{ repos?: GitRepo[] }>(`/user/github-sync/repos?${qs.toString()}`)
      .then(res => setRepos(Array.isArray(res?.repos) ? res.repos : []))
      .catch(err => {
        setRepos(null)
        setError((err?.data?.message as string) || 'Could not list GitHub repositories.')
      })
  }, [open, providerId, selected])

  const doImport = async (repo: GitRepo) => {
    if (!selected) return
    setImporting(repo.fullName)
    setError(null)
    try {
      const data = await postJSON<{ projectId: string }>('/project/new/github-sync', {
        body: {
          ...repo,
          provider: selected.provider,
          serverUrl: selected.url,
          username: selected.username,
        },
      })
      onClose()
      if (data?.projectId) {
        window.location.href = `/project/${data.projectId}`
        return
      }
    } catch (err: any) {
      setError((err?.data?.message as string) || 'GitHub import failed.')
    } finally {
      setImporting(null)
    }
  }

  return (
    <Modal opened={open} onClose={onClose} size="lg" title={<Text fw={700}>Import from GitHub</Text>} withinPortal>
      <Stack gap="md">
        {providers.length > 0 ? (
          <Group gap="xs" wrap="nowrap" align="center">
            <Text size="sm" fw={600} pr="xs">GitHub account:</Text>
            <NativeSelect
              value={providerId}
              onChange={v => setProviderId(v)}
              data={providers.map(p => ({
                value: p.id,
                label: p.username || p.url || p.provider || p.id,
              }))}
              style={{ maxWidth: 360 }}
            />
          </Group>
        ) : null}
        {error ? <Text size="sm" c="red">{error}</Text> : null}
        {repos === null && !error ? (
          <Text size="sm" c="dimmed">
            {providers.length ? 'Loading repositories…' : 'No GitHub accounts are linked on this instance yet. Link one in the editor (Repository sync) to import a repository.'}
          </Text>
        ) : null}
        {repos && repos.length === 0 ? (
          <EmptyState
            icon="cloud"
            title="No repositories found"
            hint="This GitHub account has no repositories visible to it."
          />
        ) : null}
        {repos && repos.length > 0 ? (
          <Table striped withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
            <Table.Tbody>
              {repos.map(repo => (
                <Table.Tr key={repo.fullName}>
                  <Table.Td>
                    <Text size="sm" fw={600}>{repo.name}</Text>
                    <a href={`https://github.com/${repo.fullName}`} target="_blank" rel="noreferrer">
                      <Text size="xs" c="dimmed">{repo.fullName}</Text>
                    </a>
                  </Table.Td>
                  <Table.Td style={{ textAlign: 'right' }}>
                    <Button
                      size="xs"
                      color="ollitex"
                      loading={importing === repo.fullName}
                      disabled={importing !== null && importing !== repo.fullName}
                      onClick={() => void doImport(repo)}
                    >
                      Import
                    </Button>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        ) : null}
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose} disabled={importing !== null}>
            Cancel
          </Button>
        </Group>
      </Stack>
    </Modal>
  )
}

// ─────────────────────────────────── main section ────────────────────────
export default function ProjectsSection({
  defaultFilter = 'all',
  defaultTagId,
}: {
  defaultFilter?: View
  defaultTagId?: string | null
} = {}) {
  const [view] = useState<View>(defaultFilter)
  const [projects, setProjects] = useState<Project[] | null>(null)
  const [, setAllProjects] = useState<Project[]>([])
  const [error, setError] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [tagId, setTagId] = useState<string | null>(defaultTagId || null)
  const [tags, setTags] = useState<Array<{ _id?: string; id?: string; name?: string }>>([])
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<string[]>([])
  const [busy, setBusy] = useState<string | null>(null)

  // new-project modals
  const [newOpen, setNewOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [templates, setTemplates] = useState<{ id: string; name: string }[]>([])
  const [template, setTemplate] = useState('none')
  const [newName, setNewName] = useState('')
  const [newErr, setNewErr] = useState<string | null>(null)
  const [zipOpen, setZipOpen] = useState(false)
  const [docxOpen, setDocxOpen] = useState(false)
  const [mdOpen, setMdOpen] = useState(false)
  const [ghOpen, setGhOpen] = useState(false)

  // action states
  const [toTrash, setToTrash] = useState<Project[] | null>(null)
  const [toLeave, setToLeave] = useState<Project | null>(null)
  const [tagPicker, setTagPicker] = useState<{ projects: Project[] } | null>(null)
  const [tagPickId, setTagPickId] = useState('')

  const isTrashedView = view === 'trashed'

  const load = useCallback(async (v: View, tag: string | null) => {
    setError(null)
    try {
      const filters: Record<string, unknown> = {}
      if (v === 'owned') filters.ownedByUser = true
      if (v === 'shared') filters.sharedWithUser = true
      if (v === 'archived') filters.archived = true
      if (v === 'trashed') filters.trashed = true
      if (tag != null) filters.tag = tag
      const data = await postJSON('/api/project', {
        body: {
          filters,
          sort: { by: 'lastUpdated', order: 'desc' },
        },
      })
      // The server's "all"/owned/shared views include trashed projects
      // (trashed is only an INCLUSIVE filter) — exclude them here; the
      // trashed view shows exactly the trashed set.
      const list = (Array.isArray(data?.projects) ? data.projects : [])
        .filter((p: any) => (v === 'trashed' ? true : !p?.trashed))
        .map((p: any) => ({
          ...p,
          id: p?._id || p?.id,
        })) as Project[]
      setAllProjects(list)
      setProjects(list)
    } catch (err: any) {
      setProjects([])
      setAllProjects([])
      setError((err?.data?.message as string) || String(err?.message || err))
    }
  }, [])

  useEffect(() => {
    setProjects(null)
    setSelected([])
    setPage(1)
    void load(view, tagId)
  }, [load, view, tagId])

  useEffect(() => {
    let alive = true
    getJSON('/tag')
      .then((data: any) => {
        if (alive && Array.isArray(data?.tags)) setTags(data.tags)
        else if (alive && Array.isArray(data)) setTags(data)
      })
      .catch(() => {
        if (alive) setTags([])
      })
    return () => {
      alive = false
    }
  }, [])

  useEffect(() => {
    let alive = true
    getJSON('/api/templates?by=name&order=asc&category=none')
      .then((data: any) => {
        if (!alive) return
        const list = Array.isArray(data?.templates) ? data.templates : []
        setTemplates(
          list
            .map((t: any) => ({ id: t?._id || t?.id || '', name: t?.title || t?.name || 'Template' }))
            .filter((t: any) => t.id)
        )
      })
      .catch(() => {
        if (alive) setTemplates([])
      })
    return () => {
      alive = false
    }
  }, [])

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase()
    const list = !q
      ? (projects || [])
      : (projects || []).filter(p => (p.name || '').toLowerCase().includes(q))
    return list
  }, [projects, query])

  const pageRows = useMemo(() => {
    if (visible.length <= PAGE_SIZE) return visible
    const start = (page - 1) * PAGE_SIZE
    return visible.slice(start, start + PAGE_SIZE)
  }, [visible, page])

  const openProject = (id: string) => window.location.assign(`/project/${id}`)

  const act = async (key: string, fn: () => Promise<unknown>, okMsg?: string) => {
    setBusy(key)
    try {
      await fn()
      if (okMsg) notifications.show({ message: okMsg, color: 'teal' })
      setSelected([])
      await load(view, tagId)
    } catch (err: any) {
      notifications.show({ message: (err?.data?.message as string) || `Action failed: ${key}`, color: 'red' })
    } finally {
      setBusy(null)
    }
  }

  const doClone = async (p: Project) => {
    const suffix = p.name.endsWith('_copy') ? '' : '_copy'
    await act(
      `clone:${p.id}`,
      async () => {
        const data = await postJSON<{ project_id: string }>(`/Project/${p.id}/clone`, {
          body: { projectName: `${p.name}${suffix}` },
        })
        return data
      },
      `Copied “${p.name}”.`
    )
  }

  const downloadOne = (p: Project) => {
    window.open(`/Project/${p.id}/download/zip`, '_blank', 'noopener')
  }

  const downloadBulk = (list: Project[]) => {
    if (list.length === 0) return
    window.open(`/project/download/zip?project_ids=${list.map(pid).join(',')}`, '_blank', 'noopener')
  }

  // Download PDF = compile (server waits for the first compile), then open
  // the PDF in a new tab — same flow as the legacy compile-and-download button.
  const doDownloadPdf = async (p: Project) => {
    const id = pid(p)
    setBusy(`pdf:${id}`)
    try {
      await postJSON(`/project/${id}/compile`, {
        body: { check: 'silent', draft: false, incrementalCompilesEnabled: true },
      })
      window.open(`/project/${id}/pdf`, '_blank', 'noopener')
    } catch (err: any) {
      notifications.show({ message: err?.body?.error || err?.message || 'Compile failed — PDF not available yet.', color: 'red' })
    } finally {
      setBusy('')
    }
  }

  // B4/10e (owner review): "Example project" = a template matching example/sample,
  // else the first available one.
  const exampleTpl = templates.find(t => /example|sample/i.test(t.name)) || templates[0]

  const openWithTemplate = (id?: string) => {
    setNewName('')
    setNewErr(null)
    setTemplate(id || 'none')
    setNewOpen(true)
  }

  const doArchive = async (list: Project[], archive: boolean) => {
    await act(
      `archive:${archive}`,
      async () => {
        for (const p of list) {
          if (archive) await postJSON(`/project/${p.id}/archive`, {})
          else await deleteJSON(`/project/${p.id}/archive`)
        }
      },
      `${list.length} project${list.length === 1 ? '' : 's'} ${archive ? 'archived' : 'unarchived'}.`
    )
  }

  const doTrash = async (list: Project[]) => {
    await act(
      'trash',
      async () => {
        for (const p of list) {
          await postJSON(`/project/${p.id}/trash`, {})
        }
      },
      `${list.length} project${list.length === 1 ? '' : 's'} moved to trash.`
    )
  }

  const doRestore = async (list: Project[]) => {
    await act(
      'restore',
      async () => {
        for (const p of list) {
          await deleteJSON(`/project/${p.id}/trash`)
        }
      },
      `${list.length} project${list.length === 1 ? '' : 's'} restored.`
    )
  }

  const doLeave = async (p: Project) => {
    await act(
      `leave:${p.id}`,
      () => postJSON(`/project/${p.id}/leave`, {}),
      `Left “${p.name}”.`
    )
  }

  const addToTag = async (list: Project[], tag: string) => {
    const t = tags.find(x => (x?._id || x?.id) === tag)
    if (!t) return
    await act(
      `tag:${tag}`,
      () => postJSON(`/tag/${tag}/projects`, { body: { project_ids: list.map(pid) } }),
      `Added to “${t?.name || 'tag'}”.`
    )
  }

  const createProject = async () => {
    if (!newName.trim()) {
      setNewErr('Project name is required.')
      return
    }
    setCreating(true)
    setNewErr(null)
    try {
      const data = await postJSON<{ project_id: string }>('/project/new', {
        body: {
          projectName: newName.trim(),
          template: template === 'none' ? '' : template,
        },
      })
      setNewOpen(false)
      setNewName('')
      if (data?.project_id) {
        window.location.assign(`/project/${data.project_id}`)
        return
      }
      await load(view, tagId)
    } catch (err: any) {
      setNewErr((err?.data?.message as string) || 'Could not create the project.')
    } finally {
      setCreating(false)
    }
  }

  const toggleSelect = (id: string) =>
    setSelected(prev => (prev.includes(id) ? prev.filter(x => x !== id) : [...prev, id]))
  const toggleAll = (v: boolean) => setSelected(v ? pageRows.map(pid) : [])

  const openTagPicker = (list: Project[]) => {
    setTagPickId('')
    setTagPicker({ projects: list })
  }

  const totalProjects = (projects || []).length
  const ownerRows = totalProjects > PAGE_SIZE

  if (!projects && !error) return <PageLoading label="Loading your projects…" />

  return (
    <Stack gap="md">
      <Group justify="space-between" wrap="wrap" gap="sm">
        <Group gap="xs" wrap="wrap" align="center">
          <Text size="xs" c="dimmed" fw={600} ta="uppercase" style={{ letterSpacing: '0.06em' }}>
            {isTrashedView ? 'Trashed projects' : `${totalProjects} project${totalProjects === 1 ? '' : 's'}`}
          </Text>
          {(tags || []).length > 0 ? (
            <Group gap={4} wrap="nowrap">
              <Icon name="tag" size={14} style={{ color: 'var(--mantine-color-text-dimmed)' }} />
              {tags.slice(0, 8).map(t => {
                const id = t?._id || t?.id || ''
                return (
                  <Button
                    key={id}
                    size="xs"
                    variant={tagId === id ? 'filled' : 'default'}
                    color="ollitex"
                    onClick={() => setTagId(tagId === id ? null : id)}
                  >
                    {t?.name}
                  </Button>
                )
              })}
            </Group>
          ) : null}
        </Group>
        <Group gap="xs">
          <TextInput
            leftSection={<Icon name="search" size={18} />}
            value={query}
            onChange={e => { setQuery(e.currentTarget.value); setPage(1) }}
            placeholder="Search projects"
            size="md"
            style={{ maxWidth: 320, width: '100%' }}
          />
          <Menu width={320} position="bottom-end" withinPortal>
            <Menu.Target>
              <Button
                size="md"
                color="ollitex"
                leftSection={<Icon name="add" size={18} />}
                rightSection={<Icon name="expand_more" size={16} />}
                disabled={isTrashedView}
              >
                New project
              </Button>
            </Menu.Target>
            <Menu.Dropdown>
              <Menu.Item
                leftSection={<Icon name="article" size={16} />}
                onClick={() => openWithTemplate('none')}
              >
                Blank project
              </Menu.Item>
              <Menu.Divider>Import</Menu.Divider>
              <Menu.Item leftSection={<Icon name="folder_zip" size={16} />} onClick={() => setZipOpen(true)}>
                Existing project (.zip)
              </Menu.Item>
              <Menu.Item leftSection={<Icon name="description" size={16} />} onClick={() => setDocxOpen(true)}>
                Word document (.docx)
              </Menu.Item>
              <Menu.Item leftSection={<Icon name="markdown" size={16} />} onClick={() => setMdOpen(true)}>
                Markdown file (.md)
              </Menu.Item>
              <Menu.Item leftSection={<Icon name="cloud" size={16} />} onClick={() => setGhOpen(true)}>
                Import from GitHub
              </Menu.Item>
              <Menu.Divider>Templates</Menu.Divider>
              {exampleTpl ? (
                <Menu.Item leftSection={<Icon name="menu_book" size={16} />} onClick={() => openWithTemplate(exampleTpl.id)}>
                  Example project
                  <Text size="xs" c="dimmed" mt={2} fw={400}>
                    {exampleTpl.name}
                  </Text>
                </Menu.Item>
              ) : null}
              <Menu.Label>More templates</Menu.Label>
              {templates.length === 0 ? (
                <Menu.Item disabled>No templates published yet</Menu.Item>
              ) : (
                templates.map(t => (
                  <Menu.Item key={t.id} leftSection={<Icon name="auto_stories" size={16} />} onClick={() => openWithTemplate(t.id)}>
                    {t.name}
                  </Menu.Item>
                ))
              )}
            </Menu.Dropdown>
          </Menu>
        </Group>
      </Group>

      {error ? <PageError label="Couldn’t load projects" detail={error} onRetry={() => void load(view, tagId)} /> : null}

      {selected.length > 0 ? (
        <Group gap={6} wrap="wrap" align="center" style={{ background: 'rgba(30,107,65,0.06)', borderRadius: 10, padding: '8px 12px', border: '1px solid rgba(30,107,65,0.2)' }}>
          <Text size="sm" fw={700}>{selected.length} selected</Text>
          <ActionIcon label="Clear selection" variant="subtle" onClick={() => setSelected([])} aria-label="Clear selection">
            <Icon name="close" size={16} />
          </ActionIcon>
          <Group gap={6} wrap="nowrap">
            <Button size="xs" variant="light" color="ollitex" loading={busy === 'bulk-dl'} leftSection={<Icon name="download" size={14} />}
              onClick={() => downloadBulk((projects || []).filter(p => selected.includes(pid(p))))}>
              Download
            </Button>
            {isTrashedView ? (
              <Button size="xs" variant="light" color="teal" loading={busy === 'restore'} leftSection={<Icon name="restore_from_trash" size={14} />}
                onClick={() => setToTrash((projects || []).filter(p => selected.includes(pid(p))))}>
                Restore
              </Button>
            ) : (
              <>
                <Button size="xs" variant="light" color="teal" loading={!!busy && busy.startsWith('archive')} leftSection={<Icon name="archive" size={14} />}
                  onClick={() => void doArchive((projects || []).filter(p => selected.includes(pid(p))), true)}>
                  Archive
                </Button>
                <Button size="xs" variant="light" color="red" loading={busy === 'trash'} leftSection={<Icon name="delete" size={14} />}
                  onClick={() => setToTrash((projects || []).filter(p => selected.includes(pid(p))))}>
                  Move to trash
                </Button>
              </>
            )}
            <Button size="xs" variant="light" leftSection={<Icon name="tag" size={14} />}
              onClick={() => openTagPicker((projects || []).filter(p => selected.includes(pid(p))))}>
              Add to tag
            </Button>
          </Group>
        </Group>
      ) : null}

      {projects && pageRows.length === 0 ? (
        <EmptyState
          icon={query ? 'search_off' : isTrashedView ? 'delete_sweep' : 'folder'}
          title={query ? 'No projects match your search' : isTrashedView ? 'Trash is empty' : 'No projects yet'}
          hint={
            query
              ? `Nothing matched “${query}”.`
              : isTrashedView
                ? 'Projects you move to the trash appear here until you restore them.'
                : 'Create your first project to start writing LaTeX — blank, from a template, from GitHub, or by importing a .zip / .docx / .md.'
          }
          action={
            !query && !isTrashedView ? (
              <Button size="sm" color="ollitex" leftSection={<Icon name="add" size={16} />} onClick={() => { setNewName(''); setNewOpen(true) }}>
                New project
              </Button>
            ) : undefined
          }
        />
      ) : null}

      {projects && pageRows.length > 0 ? (
        <Table striped highlightOnHover withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
          <Table.Thead>
            <Table.Tr>
              <Table.Th style={{ width: 40 }}>
                <Checkbox
                  checked={pageRows.length > 0 && pageRows.every(p => selected.includes(pid(p)))}
                  onChange={e => toggleAll(!!e.currentTarget.checked)}
                  aria-label="Select all projects on this page"
                />
              </Table.Th>
              <Table.Th>Project</Table.Th>
              <Table.Th style={{ width: 220 }}>Owner</Table.Th>
              <Table.Th style={{ width: 110 }}>Updated</Table.Th>
              <Table.Th style={{ width: 190, textAlign: 'right' }}>Actions</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {pageRows.map(p => {
              const id = pid(p)
              const selectedRow = selected.includes(id)
              return (
                <Table.Tr
                  key={id}
                  style={{ background: selectedRow ? 'rgba(30,107,65,0.06)' : undefined }}
                >
                  <Table.Td>
                    <Checkbox
                      checked={selectedRow}
                      onChange={() => toggleSelect(id)}
                      aria-label={`Select ${p.name}`}
                    />
                  </Table.Td>
                  <Table.Td>
                    <Group gap={8} wrap="nowrap" style={{ minWidth: 0 }}>
                      <Button
                        component="a"
                        href={isTrashedView ? undefined : `/project/${id}`}
                        target="_blank"
                        rel="noreferrer"
                        variant="subtle"
                        fw={600}
                        size="xs"
                        style={{ padding: 0, textDecoration: 'none', display: 'flex', minWidth: 0 }}
                        disabled={isTrashedView}
                      >
                        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 340 }}>{p.name}</span>
                      </Button>
                      {p.accessLevel === 'owner' ? (
                        <Badge size="xs" variant="light" color="ollitex" radius="sm">Owner</Badge>
                      ) : (
                        <Badge size="xs" variant="subtle" radius="sm">Shared</Badge>
                      )}
                      {p.archived && !p.trashed ? (
                        <Badge size="xs" variant="outline" radius="sm">Archived</Badge>
                      ) : null}
                    </Group>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{p.owner ? personName(p.owner) : '—'}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" c="dimmed">{timeAgo(p.lastUpdated)}</Text>
                  </Table.Td>
                  <Table.Td style={{ textAlign: 'right' }}>
                    <Group gap={4} justify="flex-end" wrap="nowrap">
                      {isTrashedView ? (
                        <Tooltip label="Restore">
                          <ActionIcon variant="subtle" color="teal" aria-label="Restore" style={{ cursor: 'pointer' }}
                            onClick={() => { setToTrash([p]); }}>
                            <Icon name="restore_from_trash" size={18} />
                          </ActionIcon>
                        </Tooltip>
                      ) : (
                        <>
                          <Tooltip label="Copy project">
                            <ActionIcon variant="subtle" aria-label="Copy project" style={{ cursor: 'pointer' }}
                              loading={busy === `clone:${id}`}
                              onClick={() => void doClone(p)}>
                              <Icon name="content_copy" size={18} />
                            </ActionIcon>
                          </Tooltip>
                          <Tooltip label="Download (.zip)">
                            <ActionIcon variant="subtle" aria-label="Download (.zip)" style={{ cursor: 'pointer' }} onClick={() => downloadOne(p)}>
                              <Icon name="download" size={18} />
                            </ActionIcon>
                          </Tooltip>
                          <Tooltip label={p.archived ? 'Un-archive' : 'Archive'}>
                            <ActionIcon variant="subtle" aria-label={p.archived ? 'Un-archive' : 'Archive'} style={{ cursor: 'pointer' }}
                              loading={busy === `archive:${id}`}
                              onClick={() => void doArchive([p], !p.archived)}>
                              <Icon name={p.archived ? 'unarchive' : 'archive'} size={18} />
                            </ActionIcon>
                          </Tooltip>
                          {p.accessLevel && p.accessLevel !== 'owner' ? (
                            <Tooltip label="Leave project">
                              <ActionIcon variant="subtle" color="orange" aria-label="Leave project" style={{ cursor: 'pointer' }} onClick={() => setToLeave(p)}>
                                <Icon name="logout" size={18} />
                              </ActionIcon>
                            </Tooltip>
                          ) : null}
                          <Tooltip label="Move to trash">
                            <ActionIcon variant="subtle" color="red" aria-label="Move to trash" style={{ cursor: 'pointer' }} onClick={() => setToTrash([p])}>
                              <Icon name="delete" size={18} />
                            </ActionIcon>
                          </Tooltip>
                        </>
                      )}
                      <Menu width={220} position="bottom-end">
                        <Menu.Target>
                          <ActionIcon variant="subtle" c="dimmed" aria-label="More actions" style={{ cursor: 'pointer' }}>
                            <Icon name="more_vert" size={16} />
                          </ActionIcon>
                        </Menu.Target>
                        <Menu.Dropdown>
                          {isTrashedView ? null : (
                            <>
                              <Menu.Item leftSection={<Icon name="open_in_new" size={16} />} component="a" href={`/project/${id}`} target="_blank" rel="noreferrer">
                                Open
                              </Menu.Item>
                              <Menu.Item leftSection={<Icon name="content_copy" size={16} />} onClick={() => void doClone(p)}>
                                Copy project
                              </Menu.Item>
                              <Menu.Item leftSection={<Icon name="download" size={16} />} onClick={() => downloadOne(p)}>
                                Download (.zip)
                              </Menu.Item>
                              <Menu.Item
                                leftSection={<Icon name="picture_as_pdf" size={16} />}
                                loading={busy === `pdf:${id}`}
                                onClick={() => void doDownloadPdf(p)}
                              >
                                Download PDF
                              </Menu.Item>
                              <Menu.Item leftSection={<Icon name={p.archived ? 'unarchive' : 'archive'} size={16} />} onClick={() => void doArchive([p], !p.archived)}>
                                {p.archived ? 'Un-archive' : 'Archive'}
                              </Menu.Item>
                              <Menu.Divider />
                            </>
                          )}
                          <Menu.Item leftSection={<Icon name="tag" size={16} />} onClick={() => openTagPicker([p])}>
                            Add to tag…
                          </Menu.Item>
                          {p.accessLevel && p.accessLevel !== 'owner' && !isTrashedView ? (
                            <Menu.Item leftSection={<Icon name="logout" size={16} />} color="orange" onClick={() => setToLeave(p)}>
                              Leave project
                            </Menu.Item>
                          ) : null}
                          <Menu.Item
                            leftSection={<Icon name={isTrashedView ? 'restore_from_trash' : 'delete'} size={16} />}
                            color={isTrashedView ? 'teal' : 'red'}
                            onClick={() => setToTrash([p])}
                          >
                            {isTrashedView ? 'Restore' : 'Move to trash'}
                          </Menu.Item>
                        </Menu.Dropdown>
                      </Menu>
                    </Group>
                  </Table.Td>
                </Table.Tr>
              )
            })}
          </Table.Tbody>
        </Table>
      ) : null}

      {ownerRows ? (
        <Group justify="space-between" wrap="wrap" gap="xs">
          <Text size="sm" c="dimmed">
            Page {page} of {Math.max(1, Math.ceil(totalProjects / PAGE_SIZE))} — {totalProjects} project{totalProjects === 1 ? '' : 's'}
          </Text>
          <Group gap={6}>
            <Button size="xs" variant="default" disabled={page <= 1} onClick={() => setPage(p => Math.max(1, p - 1))}>
              Prev
            </Button>
            <Button size="xs" variant="default" disabled={page >= Math.ceil(totalProjects / PAGE_SIZE)} onClick={() => setPage(p => p + 1)}>
              Next
            </Button>
          </Group>
        </Group>
      ) : null}

      {/* New project (blank / from template) */}
      <Modal opened={newOpen} onClose={() => setNewOpen(false)} size="sm" title={<Text fw={700}>New project</Text>} withinPortal>
        <Stack gap="md">
          <label htmlFor="hub-new-project-name" style={{ display: 'block' }}>
            <Text size="sm" fw={600} mb={6}>Project name</Text>
            <TextInput
              id="hub-new-project-name"
              value={newName}
              onChange={e => setNewName(e.currentTarget.value)}
              placeholder="e.g. Thesis chapter 1"
              error={newErr && !newName.trim() ? 'required' : undefined}
            />
          </label>
          <label htmlFor="hub-new-project-template" style={{ display: 'block' }}>
            <Text size="sm" fw={600} mb={6}>Start from</Text>
            <NativeSelect
              id="hub-new-project-template"
              value={template}
              onChange={e => setTemplate(e.currentTarget.value)}
              data={[
                { value: 'none', label: 'Blank project' },
                ...templates.map(t => ({ value: t.id, label: t.name })),
              ]}
            />
          </label>
          {newErr && newName.trim() ? <Text size="sm" c="red">{newErr}</Text> : null}
          <Group justify="flex-end" gap="xs">
            <Button variant="default" onClick={() => setNewOpen(false)} disabled={creating}>
              Cancel
            </Button>
            <Button color="ollitex" loading={creating} onClick={() => void createProject()}>
              Create
            </Button>
          </Group>
        </Stack>
      </Modal>

      {/* GitHub import (Mantine rework, same APIs) */}
      <GithubImportModal open={ghOpen} onClose={() => setGhOpen(false)} />

      {/* zip / docx / md uploads (Mantine chrome over the proven Uppy engine) */}
      <UppyUploadModal
        open={zipOpen}
        title="Upload a zipped project"
        accept={['.zip']}
        modalId="upload-project-modal"
        endpoint="/project/new/upload"
        browseLabel="Select a .zip file"
        dragLabel="Select a .zip file or drag one here"
        onCancel={() => setZipOpen(false)}
        onDone={id => openProject(id)}
      />
      <UppyUploadModal
        open={docxOpen}
        title="Import a Word document"
        accept={['.docx']}
        endpoint="/project/new/import-document?type=docx"
        browseLabel="Select a .docx file"
        dragLabel="Select a .docx file or drag it here"
        onCancel={() => setDocxOpen(false)}
        onDone={id => openProject(id)}
      />
      <UppyUploadModal
        open={mdOpen}
        title="Import a Markdown file"
        accept={['.md', '.markdown']}
        endpoint="/project/new/import-document?type=markdown"
        browseLabel="Select a .md file"
        dragLabel="Select a .md file or drag it here"
        onCancel={() => setMdOpen(false)}
        onDone={id => openProject(id)}
      />

      {/* Move-to-trash / restore confirm */}
      <ConfirmModal
        open={!!toTrash}
        title={isTrashedView ? 'Restore project(s)?' : 'Move to trash?'}
        body={
          toTrash
            ? isTrashedView
              ? `${toTrash.length} project${toTrash.length === 1 ? '' : 's'} will be moved back to your projects.`
              : `${toTrash.length} project${toTrash.length === 1 ? '' : 's'} will be moved to the trash (you can restore them any time).`
            : ''
        }
        confirmLabel={isTrashedView ? 'Restore' : 'Move to trash'}
        danger={!isTrashedView}
        loading={busy === 'trash' || busy === 'restore'}
        onCancel={() => setToTrash(null)}
        onConfirm={() => {
          const list = toTrash || []
          setToTrash(null)
          if (isTrashedView) void doRestore(list)
          else void doTrash(list)
        }}
      />

      {/* Leave confirm */}
      <ConfirmModal
        open={!!toLeave}
        title="Leave project?"
        body={toLeave ? `You will lose access to “${toLeave.name}”.` : ''}
        confirmLabel="Leave"
        danger
        loading={busy === `leave:${toLeave?.id}`}
        onCancel={() => setToLeave(null)}
        onConfirm={() => {
          const p = toLeave
          setToLeave(null)
          if (p) void doLeave(p)
        }}
      />

      {/* Tag picker */}
      <Modal opened={!!tagPicker} onClose={() => setTagPicker(null)} size="sm" title={<Text fw={700}>Add to tag</Text>} withinPortal>
        <Stack gap="md">
          {tags.length === 0 ? (
            <Text size="sm" c="dimmed">
              You have no tags yet. Create a project tag in the editor (or ask your admin to enable tags) and it will appear here.
            </Text>
          ) : (
            <NativeSelect
              value={tagPickId}
              onChange={v => setTagPickId(v)}
              placeholder="Choose a tag"
              data={tags.map(t => ({ value: t?._id || t?.id || '', label: t?.name || 'Tag' }))}
            />
          )}
          <Group justify="flex-end" gap="xs">
            <Button variant="default" onClick={() => setTagPicker(null)}>
              Cancel
            </Button>
            <Button
              color="ollitex"
              disabled={!tagPickId}
              loading={!!busy && busy.startsWith('tag:')}
              onClick={() => {
                const list = tagPicker?.projects || []
                setTagPicker(null)
                void addToTag(list, tagPickId)
              }}
            >
              Add
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  )
}
