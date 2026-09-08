// Site settings → Projects leaves (legacy /admin/projects parity, owner #20).
// Per-row: open · download zip · trash/restore-from-trash · delete · undelete ·
// purge · transfer owner.
// Select-all + view-gated bulk toolbar:
//   all/inactive → Download · Change owner · Trash
//   trashed      → Download · Change owner · Restore · Delete
//   deleted      → Restore · Purge
// APIs (legacy admin-tools project-list/util/api.ts, verified):
//   POST /admin/user/:idOr'null'/projects, POST /project/:id/transfer-ownership
//   {user_id, skipEmails}, POST /admin/project/:id/trash|untrash {userId},
//   POST /admin/project/:id/undelete {userId}, DELETE /admin/project/:id,
//   DELETE /admin/project/:id/purge, GET /project/download/zip?project_ids=a,b

import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ActionIcon,
  Badge,
  Button,
  Checkbox,
  Group,
  Menu,
  Modal,
  Pagination,
  NativeSelect,
  Stack,
  Table,
  Text,
  TextInput,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { deleteJSON, postJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import ConfirmModal from '../../shared/confirm-modal'
import { EmptyState, PageError, PageLoading } from '../../shared/page-state'
import { BulkToolbar, HeaderCheckbox, RowCheckbox, useSelection, BulkAction } from '../../shared/bulk-select'

type AdminProject = {
  _id?: string
  id?: string
  name?: string
  owner_ref?: string
  ownerId?: string
  owner?: { email?: string }
  lastUpdated?: string
  lastOpened?: string
  lastActive?: string
  trashed?: boolean
  deleted?: boolean
  deletedAt?: string
}

function pid(p: any): string {
  return p?._id || p?.id || ''
}
function pname(p: any): string {
  return p?.name || p?.title || 'Untitled'
}
function powner(p: any): string {
  // admin projects API returns owner as a user-id string; accept ref shapes too
  if (typeof p?.owner === 'string') return p.owner
  return p?.owner_ref || p?.ownerId || p?.owner?._id || p?.owner?.email || ''
}
function pdate(p: any, ...keys: string[]): string {
  for (const k of keys) {
    if (p?.[k]) {
      const t = new Date(p[k]).getTime()
      if (!Number.isNaN(t)) return new Date(t).toLocaleDateString()
      return String(p[k])
    }
  }
  return '—'
}

const ownerEmails: Record<string, string> = {}

export type AdminProjectsView = 'all' | 'inactive' | 'trashed' | 'deleted'

export default function AdminProjectsSection({
  view = 'all',
}: {
  view?: AdminProjectsView
}) {
  const [users, setUsers] = useState<any[]>([])
  const [scope, setScope] = useState('all')
  const [projects, setProjects] = useState<AdminProject[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState<number | null>(null)
  const [busyKey, setBusyKey] = useState<string | null>(null)
  const [confirmPurge, setConfirmPurge] = useState<AdminProject | null>(null)
  const [confirmPurgeBulk, setConfirmPurgeBulk] = useState(false)
  const [confirmDeleteBulk, setConfirmDeleteBulk] = useState(false)
  const [confirmDeleteOne, setConfirmDeleteOne] = useState<AdminProject | null>(null)
  const [transferOpen, setTransferOpen] = useState(false)
  const [transferTarget, setTransferTarget] = useState<any | null>(null) // row-level transfer target (parity fix)
  const [shareTarget, setShareTarget] = useState<any | null>(null)
  const [shareEmail, setShareEmail] = useState('')
  const [sharePriv, setSharePriv] = useState('editor')
  const [shareErr, setShareErr] = useState<string | null>(null)
  const [shareBusy, setShareBusy] = useState(false)

  const openShare = (proj: any) => {
    setShareTarget(proj)
    setShareEmail('')
    setSharePriv('editor')
    setShareErr(null)
  }

  // Legacy admin share: POST /admin/project/:id/invite { email, privileges }
  const inviteShare = async () => {
    const target = shareTarget
    if (!target) return
    const email = shareEmail.trim()
    if (!email || !/.+@.+.+/.test(email)) {
      setShareErr('Enter a valid email address.')
      return
    }
    setShareBusy(true)
    setShareErr(null)
    try {
      await postJSON(`/admin/project/${pid(target)}/invite`, { body: { email, privileges: sharePriv } })
      notifications.show({ message: 'Invitation sent.', color: 'teal' })
      setShareTarget(null)
    } catch (err: any) {
      const raw = err?.body?.error || err?.data?.message || ''
      setShareErr(raw === 'cannot_invite_self' ? 'You cannot invite yourself to a project.' : raw || 'Could not send the invitation.')
    } finally {
      setShareBusy(false)
    }
  }
  const [tNewOwner, setTNewOwner] = useState('')
  const [tSkipEmails, setTSkipEmails] = useState(false)
  const [transferring, setTransferring] = useState(false)

  useEffect(() => {
    void (async () => {
      try {
        const data = await postJSON('/admin/users', { body: { sort: { by: 'signUpDate', order: 'desc' } } })
        const list = Array.isArray(data?.users) ? data.users : []
        setUsers(list.filter((u: any) => u && (u.email || u._id || u.id)))
        for (const u of list) {
          const id = u._id || u.id
          if (id && u.email) ownerEmails[id] = u.email
        }
      } catch {
        setUsers([])
      }
    })()
  }, [])

  const PAGE_SIZE = 25

  const load = useCallback(async (p: number) => {
    setError(null)
    const userId = scope === 'all' ? 'null' : scope
    const filters: Record<string, unknown> = {}
    if (view === 'trashed') filters.trashed = true
    else if (view === 'deleted') filters.deleted = true
    else filters.owned = true
    const q = search.trim()
    if (q) filters.search = q
    try {
      const data = await postJSON(`/admin/user/${encodeURIComponent(userId)}/projects`, {
        body: {
          sort: { by: 'lastUpdated', order: 'desc' },
          page: { index: p, size: PAGE_SIZE },
          filters,
        },
      })
      const list = Array.isArray(data?.projects) ? data.projects : []
      setProjects(list)
      setTotal(typeof data?.totalSize === 'number' ? data.totalSize : null)
    } catch (err: any) {
      setProjects([])
      setTotal(null)
      setError((err?.data?.message as string) || String(err?.message || err))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scope, view, search])

  useEffect(() => {
    setPage(1)
  }, [scope, view, search])

  useEffect(() => {
    setProjects(null)
    void load(page)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [load, page])

  // Server-side filtering (owner #2); the CE build returns the full filtered
  // list, so page client-side (slice only when > one page — stays correct if a
  // future build pages server-side). 'inactive' has no server filter.
  const allList = useMemo(() => {
    const list = projects || []
    if (view !== 'inactive') return list
    const INACTIVE_MS = 15 * 24 * 3600 * 1000
    return list.filter(p => {
      if (typeof p.inactive === 'boolean') return p.inactive
      const t0 = p.lastOpened || p.lastActive || p.lastUpdated
      if (!t0) return true
      const t = Date.parse(t0)
      if (Number.isNaN(t)) return true
      return Date.now() - t > INACTIVE_MS
    })
  }, [projects, view])

  const filtered = useMemo(() => {
    const list = allList
    if (list.length <= PAGE_SIZE) return list
    const start = (page - 1) * PAGE_SIZE
    return list.slice(start, start + PAGE_SIZE)
  }, [allList, page, PAGE_SIZE])

  const sel = useSelection(filtered.map(pid))
  const selectedProjects = useMemo(() => filtered.filter(p => sel.isSelected(pid(p))), [filtered, sel])

  const act = async (fn: () => Promise<unknown>, okMsg: string, key?: string) => {
    if (key) setBusyKey(key)
    try {
      await fn()
      notifications.show({ message: okMsg, color: 'teal' })
      await load()
    } catch (err: any) {
      notifications.show({ message: (err?.data?.message as string) || 'Action failed.', color: 'red' })
    } finally {
      if (key) setBusyKey(null)
    }
  }

  const downloadZip = (ids: string[]) => {
    if (!ids.length) return
    window.open(`/project/download/zip?project_ids=${ids.join(',')}`, '_blank', 'noopener')
  }

  const userLabel = (u: any) => (u ? `${u.first_name || ''} ${u.last_name || ''} ${u.email || ''}`.trim() || u._id || u.id || '' : '')

  const ownerOptions = [
    { value: 'all', label: 'All users' },
    ...users.map((u: any) => ({
      value: u._id || u.id,
      label: (u.email || 'user') + (u.first_name ? ` (${userLabel(u)})` : ''),
    })),
  ]
  const newOwnerOptions = users.map((u: any) => ({
    value: u._id || u.id,
    label: (u.email || 'user') + (u.first_name ? ` (${userLabel(u)})` : ''),
  }))

  // ---- bulk actions (owner #20, legacy project-tools parity) --------------
  const runBulkProject = async (key: string, fn: (p: AdminProject) => Promise<unknown>, okMsg: string) => {
    setBusyKey(key)
    let fail = 0
    for (const p of [...selectedProjects]) {
      try {
        await fn(p)
      } catch (e: any) {
        fail += 1
        notifications.show({ message: `${pname(p)}: ${e?.data?.message || 'failed'}`, color: 'red' })
      }
    }
    setBusyKey(null)
    if (fail === 0) notifications.show({ message: okMsg, color: 'teal' })
    sel.clear()
    await load()
  }

  const doBulkTransfer = async () => {
    if (!tNewOwner) return
    const scope = transferTarget ? [transferTarget] : [...selectedProjects]
    if (scope.length === 0) { setTransferOpen(false); return }
    setTransferring(true)
    let fail = 0
    for (const p of scope) {
      try {
        await postJSON(`/project/${pid(p)}/transfer-ownership`, {
          body: { user_id: tNewOwner, skipEmails: tSkipEmails },
        })
      } catch (e: any) {
        fail += 1
        notifications.show({ message: `${pname(p)}: ${e?.data?.message || 'failed'}`, color: 'red' })
      }
    }
    setTransferring(false)
    setTransferOpen(false)
    setTNewOwner('')
    if (fail === 0) notifications.show({ message: 'Ownership transferred.', color: 'teal' })
    setTransferTarget(null)
    sel.clear()
    await load()
  }

  const bulkActions: BulkAction[] = useMemo(() => {
    const L = busyKey
    if (view === 'deleted') {
      return [
        { key: 'restore', label: 'Restore', icon: 'restore_from_trash', loading: L === 'restore', onClick: () => void runBulkProject('restore', p => postJSON(`/admin/project/${pid(p)}/undelete`, { body: { userId: powner(p) } }), 'Projects restored.') },
        { key: 'purge', label: 'Purge', icon: 'delete_forever', tone: 'danger', onClick: () => setConfirmPurgeBulk(true) },
      ]
    }
    const a: BulkAction[] = [
      { key: 'download', label: 'Download', icon: 'download', onClick: () => downloadZip(selectedProjects.map(pid)) },
      { key: 'transfer', label: 'Change owner', icon: 'swap_horiz', onClick: () => { setTransferTarget(null); setTNewOwner(''); setTSkipEmails(false); setTransferOpen(true) } },
    ]
    if (view !== 'trashed') {
      a.push({ key: 'trash', label: 'Trash', icon: 'delete', loading: L === 'trash', onClick: () => void runBulkProject('trash', p => postJSON(`/admin/project/${pid(p)}/trash`, { body: { userId: powner(p) } }), 'Projects moved to trash.') })
    } else {
      a.push({ key: 'untrash', label: 'Restore', icon: 'restore_from_trash', loading: L === 'untrash', onClick: () => void runBulkProject('untrash', p => postJSON(`/admin/project/${pid(p)}/untrash`, { body: { userId: powner(p) } }), 'Projects restored.') })
      a.push({ key: 'delete', label: 'Delete', icon: 'delete', tone: 'danger', onClick: () => setConfirmDeleteBulk(true) })
    }
    return a
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, selectedProjects.length, busyKey])

  const allSelectedIds = filtered.map(pid)
  const someSelected = sel.count > 0 && sel.count < allSelectedIds.length

  if (!projects && !error) return <PageLoading label="Loading projects…" />

  return (
    <Stack gap="md">
      <Group justify="space-between" wrap="wrap" gap="sm">
        <Group gap="xs" wrap="wrap">
          <div style={{ width: 280 }}>
            <NativeSelect value={scope} onChange={e => setScope(e.currentTarget.value)} data={ownerOptions} size="sm" />
          </div>
          <TextInput
            leftSection={<Icon name="search" size={16} />}
            value={search}
            onChange={e => setSearch(e.currentTarget.value)}
            placeholder="Search project name"
            size="sm"
            style={{ width: 260 }}
          />
        </Group>
      </Group>

      {error ? <PageError label="Couldn’t load projects" detail={error} onRetry={() => void load()} /> : null}

      {projects && filtered.length > 0 ? (
        <BulkToolbar count={sel.count} actions={bulkActions} onClear={sel.clear} />
      ) : null}

      {projects && filtered.length === 0 ? (
        <EmptyState icon="folder_off" title="No projects" hint={search ? `Nothing matched “${search}”.` : 'No projects found for this scope yet.'} />
      ) : null}

      {projects && filtered.length > 0 ? (
        <Table striped highlightOnHover withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
          <Table.Thead>
            <Table.Tr>
              <Table.Th style={{ width: 36 }}>
                <HeaderCheckbox checked={sel.count > 0 && !someSelected} indeterminate={someSelected} onChange={sel.toggleAll} label="Select all projects in this view" />
              </Table.Th>
              <Table.Th>Project</Table.Th>
              <Table.Th style={{ width: 240 }}>Owner</Table.Th>
              <Table.Th style={{ width: 110 }}>Last opened</Table.Th>
              <Table.Th style={{ width: 110 }}>Updated</Table.Th>
              <Table.Th style={{ width: 90 }}>State</Table.Th>
              <Table.Th style={{ width: 60, textAlign: 'right' }}>Actions</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {filtered.map(p => (
              <Table.Tr
                key={pid(p)}
                onClick={() => sel.toggle(pid(p))}
                style={{ cursor: 'pointer', background: sel.isSelected(pid(p)) ? 'var(--mantine-color-teal-0)' : undefined }}
              >
                <Table.Td onClick={e => e.stopPropagation()}>
                  <RowCheckbox id={pid(p)} label={pname(p)} selected={sel.isSelected(pid(p))} onToggle={sel.toggle} />
                </Table.Td>
                <Table.Td>
                  <Text size="sm" fw={600} ellipsis>{pname(p)}</Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" c="dimmed" ellipsis>{ownerEmails[powner(p)] || powner(p) || '—'}</Text>
                </Table.Td>
                <Table.Td><Text size="sm" c="dimmed">{pdate(p, 'lastOpened')}</Text></Table.Td>
                <Table.Td><Text size="sm" c="dimmed">{pdate(p, 'lastUpdated')}</Text></Table.Td>
                <Table.Td>
                  {p.deleted || p.deletedAt ? (
                    <Badge size="xs" color="red" radius="sm" variant="light">Deleted</Badge>
                  ) : p.trashed ? (
                    <Badge size="xs" color="orange" radius="sm" variant="light">Trash</Badge>
                  ) : (
                    <Badge size="xs" color="teal" radius="sm" variant="light">Active</Badge>
                  )}
                </Table.Td>
                <Table.Td style={{ textAlign: 'right' }} onClick={e => e.stopPropagation()}>
                  <Menu width={260} position="bottom-end">
                    <Menu.Target>
                      <ActionIcon variant="subtle" aria-label="Actions" style={{ cursor: 'pointer' }}>
                        <Icon name="more_vert" size={18} />
                      </ActionIcon>
                    </Menu.Target>
                    <Menu.Dropdown>
                      {!p.deleted && !p.deletedAt ? (
                        <Menu.Item
                          icon={<Icon name="open_in_new" size={16} />}
                          component="a"
                          href={`/project/${pid(p)}`}
                          target="_blank"
                          rel="noreferrer"
                        >
                          Open project
                        </Menu.Item>
                      ) : null}
                      {!p.deleted && !p.deletedAt ? (
                        <Menu.Item
                          icon={<Icon name="download" size={16} />}
                          component="a"
                          href={`/project/${pid(p)}/download/zip`}
                          target="_blank"
                          rel="noreferrer"
                        >
                          Download .zip
                        </Menu.Item>
                      ) : null}
                      <Menu.Item
                        icon={<Icon name="swap_horiz" size={16} />}
                        onClick={() => {
                          setTNewOwner('')
                          setTSkipEmails(false)
                          setTransferTarget(p)
                          setTransferOpen(true)
                        }}
                      >
                        Change owner
                      </Menu.Item>
                      {!p.deleted && !p.deletedAt ? (
                        <Menu.Item
                          icon={<Icon name="person_add" size={16} />}
                          onClick={() => openShare(p)}
                        >
                          Share… (invite)
                        </Menu.Item>
                      ) : null}
                      {!p.trashed && !(p.deleted || p.deletedAt) ? (
                        <Menu.Item
                          icon={<Icon name="delete" size={16} />}
                          color="orange"
                          onClick={() => void act(() => postJSON(`/admin/project/${pid(p)}/trash`, { body: { userId: powner(p) } }), 'Moved to trash.', 'trash')}
                        >
                          Move to trash
                        </Menu.Item>
                      ) : null}
                      {p.trashed && !(p.deleted || p.deletedAt) ? (
                        <Menu.Item
                          icon={<Icon name="restore_from_trash" size={16} />}
                          color="teal"
                          onClick={() => void act(() => postJSON(`/admin/project/${pid(p)}/untrash`, { body: { userId: powner(p) } }), 'Restored from trash.', 'untrash')}
                        >
                          Restore from trash
                        </Menu.Item>
                      ) : null}
                      {!(p.deleted || p.deletedAt) ? (
                        <>
                          <Menu.Divider />
                          <Menu.Item
                            icon={<Icon name="delete" size={16} />}
                            color="red"
                            onClick={() => setConfirmDeleteOne(p)}
                          >
                            Delete
                          </Menu.Item>
                        </>
                      ) : (
                        <>
                          <Menu.Divider />
                          <Menu.Item
                            icon={<Icon name="restore_from_trash" size={16} />}
                            color="teal"
                            onClick={() => void act(() => postJSON(`/admin/project/${pid(p)}/undelete`, { body: { userId: powner(p) } }), 'Undeleted.', 'undelete')}
                          >
                            Undelete
                          </Menu.Item>
                          <Menu.Item
                            icon={<Icon name="delete_forever" size={16} />}
                            color="red"
                            onClick={() => setConfirmPurge(p)}
                          >
                            Purge permanently
                          </Menu.Item>
                        </>
                      )}
                    </Menu.Dropdown>
                  </Menu>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      ) : null}

      <Modal onClose={() => setShareTarget(null)} size="sm" title="Share project" withinPortal opened={Boolean(shareTarget)}>
        <Stack gap="md">
          {shareTarget ? <Text size="sm" c="dimmed">Invite someone to “{shareTarget.name}”. They will receive an email with the invite.</Text> : null}
          <TextInput label="Email address" value={shareEmail} onChange={e => setShareEmail(e.currentTarget.value)} placeholder="colleague@uni-bremen.de" error={shareErr || undefined} />
          <NativeSelect
            label="Access level"
            value={sharePriv}
            onChange={e => setSharePriv(e.currentTarget.value)}
            data={[
              { value: 'admin', label: 'Admin — full control' },
              { value: 'editor', label: 'Editor — can edit' },
              { value: 'reader', label: 'Reader — read only' },
              { value: 'reviewer', label: 'Reviewer — can comment' },
            ]}
          />
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setShareTarget(null)} disabled={shareBusy}>Cancel</Button>
            <Button color="ollitex" loading={shareBusy} onClick={() => void inviteShare()}>Send invite</Button>
          </Group>
        </Stack>
      </Modal>

      <Modal opened={transferOpen} onClose={() => setTransferOpen(false)} size="sm" title="Change owner">
        <Stack gap="md">
          <Text size="sm">
            Transfer {selectedProjects.length > 0 ? `${selectedProjects.length} selected project${selectedProjects.length === 1 ? '' : 's'}` : 'project'} to:
          </Text>
          <NativeSelect
            value={tNewOwner}
            onChange={v => setTNewOwner(v.target.value)}
            data={newOwnerOptions}
            placeholder="Choose new owner…"
          />
          <Checkbox label="Skip notification emails" checked={tSkipEmails} onChange={checked => setTSkipEmails(checked === true)} color="ollitex" />
          <Group justify="flex-end" gap="xs">
            <Button variant="default" onClick={() => setTransferOpen(false)} disabled={transferring}>Cancel</Button>
            <Button color="ollitex" loading={transferring} disabled={!tNewOwner} onClick={() => void doBulkTransfer()}>
              Transfer
            </Button>
          </Group>
        </Stack>
      </Modal>

      <ConfirmModal
        open={!!confirmDeleteOne}
        title="Delete project?"
        body={confirmDeleteOne ? `“${pname(confirmDeleteOne)}” will be deleted (recoverable from the trash view).` : ''}
        confirmLabel="Delete"
        danger
        loading={busyKey === 'del-one'}
        onCancel={() => setConfirmDeleteOne(null)}
        onConfirm={() => void act(() => deleteJSON(`/admin/project/${pid(confirmDeleteOne as AdminProject)}`), 'Project deleted.', 'del-one').then(() => setConfirmDeleteOne(null))}
      />

      <ConfirmModal
        open={confirmDeleteBulk}
        title={`Delete ${sel.count} project${sel.count === 1 ? '' : 's'}?`}
        body="They will move to deleted state (recoverable via Restore from this view)."
        confirmLabel="Delete"
        danger
        loading={busyKey === 'delete-bulk'}
        onCancel={() => setConfirmDeleteBulk(false)}
        onConfirm={() => void runBulkProject('delete-bulk', p => deleteJSON(`/admin/project/${pid(p)}`), 'Projects deleted.').then(() => setConfirmDeleteBulk(false))}
      />

      <ConfirmModal
        open={!!confirmPurge}
        title="Purge project permanently?"
        body={confirmPurge ? `“${pname(confirmPurge)}” and all of its documents will be irreversibly deleted.` : ''}
        confirmLabel="Purge"
        danger
        loading={busyKey === 'purge-one'}
        onCancel={() => setConfirmPurge(null)}
        onConfirm={() => void act(() => deleteJSON(`/admin/project/${pid(confirmPurge as AdminProject)}/purge`), 'Project purged.', 'purge-one').then(() => setConfirmPurge(null))}
      />

      <ConfirmModal
        open={confirmPurgeBulk}
        title={`Purge ${sel.count} project${sel.count === 1 ? '' : 's'} permanently?`}
        body="Documents and history will be irreversibly removed."
        confirmLabel="Purge"
        danger
        loading={busyKey === 'purge-bulk'}
        onCancel={() => setConfirmPurgeBulk(false)}
        onConfirm={() => void runBulkProject('purge-bulk', p => deleteJSON(`/admin/project/${pid(p)}/purge`), 'Projects purged.').then(() => setConfirmPurgeBulk(false))}
      />
          {total != null && total > PAGE_SIZE ? (
        <Group justify="space-between" wrap="wrap" gap="xs">
          <Text size="sm" c="dimmed">
            Page {page} of {Math.max(1, Math.ceil(total / PAGE_SIZE))} — {total} project{total === 1 ? '' : 's'}
          </Text>
          <Pagination order={page} total={Math.ceil(total / PAGE_SIZE)} onChange={setPage} size="sm" />
        </Group>
      ) : null}
    </Stack>
  )
}
