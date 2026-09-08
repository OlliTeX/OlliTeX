// /hub → Workspace → My Settings leaf? No — Site settings → Users leaves.
// Admin user management (legacy /admin/users parity, owner #21):
// per-row actions + Select-all + view-gated bulk toolbar:
//   all/admins/suspended/inactive → Suspend · Resume · Mail · Set admin ·
//   Unset admin · Delete
//   deleted → Restore · Purge
// APIs: POST /admin/users, /admin/user/:id/update|delete|restore|send-activation,
//       DELETE /admin/user/:id (purge), POST /admin/user/create

import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ActionIcon,
  Badge,
  Button,
  Checkbox,
  Group,
  Menu,
  Modal,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
} from '@mantine/core'
import { notify } from '../../shared/notify'
import { deleteJSON, postJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import HubPagination from '../../shared/pagination'
import ConfirmModal from '../../shared/confirm-modal'
import { EmptyState, PageError, PageLoading } from '../../shared/page-state'
import { BulkToolbar, HeaderCheckbox, RowCheckbox, useSelection, BulkAction } from '../../shared/bulk-select'

type AdminUser = {
  _id: string
  email: string
  first_name?: string
  last_name?: string
  isAdmin?: boolean
  canManageTemplates?: boolean
  flags?: Record<string, unknown>
  suspended?: boolean
  deletedAt?: string
  signUpDate?: string
  lastActive?: string
}

function uid(u: any): string {
  return u?._id || u?.id || ''
}
function fmtDate(v?: string | null): string {
  if (!v) return '—'
  const t = new Date(v).getTime()
  if (Number.isNaN(t)) return String(v)
  return new Date(t).toLocaleDateString()
}
function isTemplateAdmin(u: any): boolean {
  return !!(u?.canManageTemplates || u?.flags?.canManageTemplates)
}

/** Row action “User info” (legacy show-user-info parity, owner B25). */
function UserInfoModal({ user, onClose }: { user: any; onClose: () => void }) {
  if (!user) return null
  const rows: Array<[string, string]> = [
    ['ID', String(user._id || user.id || '—')],
    ['Email', user.email || '—'],
    ['First name', user.firstName || '—'],
    ['Last name', user.lastName || '—'],
    ['Role', [user.isAdmin ? 'Admin' : 'User', isTemplateAdmin(user) ? 'Template manager' : null, user.suspended ? 'Suspended' : null].filter(Boolean).join(' · ') || 'User'],
    ['Signed up', fmtDate(user.signUpDate)],
    ['Last active', fmtDate(user.lastActive)],
  ]
  return (
    <Modal opened onClose={onClose} size="sm" title={<Text fw={700}>User info</Text>} withinPortal>
      <Stack gap="sm">
        {rows.map(([k, val]) => (
          <Group key={k} justify="space-between" gap="md">
            <Text size="sm" c="dimmed" style={{ minWidth: 90, flexShrink: 0 }}>{k}</Text>
            <Text size="sm" style={{ textAlign: 'right', wordBreak: 'break-word' }}>{val}</Text>
          </Group>
        ))}
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>Close</Button>
        </Group>
      </Stack>
    </Modal>
  )
}

/** Row action “Update” (legacy update-user parity, owner B25): name, email,
 * admin flag, template manager. Saves via POST /admin/user/:id/update. */
function UserUpdateModal({ user, onClose, onSaved }: { user: any; onClose: () => void; onSaved: (u: any) => void }) {
  const [first, setFirst] = useState(user?.firstName || '')
  const [last, setLast] = useState(user?.lastName || '')
  const [email, setEmail] = useState(user?.email || '')
  const [admin, setAdmin] = useState(Boolean(user?.isAdmin))
  const [tpls, setTpls] = useState(isTemplateAdmin(user))
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  if (!user) return null
  const save = async () => {
    setSaving(true)
    setErr(null)
    try {
      await postJSON(`/admin/user/${user._id || user.id}/update`, {
        body: { firstName: first, lastName: last, email, isAdmin: admin, canManageTemplates: tpls },
      })
      onSaved(user)
      notify({ message: 'User updated.', color: 'teal' })
      onClose()
    } catch (e: any) {
      setErr((e?.data?.message as string) || 'Could not update the user.')
    } finally {
      setSaving(false)
    }
  }
  return (
    <Modal opened onClose={onClose} size="sm" title={<Text fw={700}>Update user</Text>} withinPortal>
      <Stack gap="md">
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
          <TextInput label="First name" value={first} onChange={e => setFirst(e.currentTarget.value)} />
          <TextInput label="Last name" value={last} onChange={e => setLast(e.currentTarget.value)} />
        </div>
        <TextInput label="Email" value={email} onChange={e => setEmail(e.currentTarget.value)} />
        <Group gap="md">
          <Switch label="Admin" checked={admin} onChange={() => setAdmin(!admin)} />
          <Switch label="Template manager" checked={tpls} onChange={() => setTpls(!tpls)} />
        </Group>
        {err ? <Text size="sm" c="red">{err}</Text> : null}
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose} disabled={saving}>Cancel</Button>
          <Button color="ollitex" loading={saving} onClick={() => void save()}>Save changes</Button>
        </Group>
      </Stack>
    </Modal>
  )
}

export type AdminUsersView = 'all' | 'admins' | 'suspended' | 'inactive' | 'deleted'

export default function AdminUsersSection({
  view = 'all',
}: {
  view?: AdminUsersView
}) {
  const [users, setUsers] = useState<AdminUser[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState<number | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [cEmail, setCEmail] = useState('')
  const [cFirst, setCFirst] = useState('')
  const [cLast, setCLast] = useState('')
  const [cAdmin, setCAdmin] = useState(false)
  const [cTemplates, setCTemplates] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createErr, setCreateErr] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [bulkBusy, setBulkBusy] = useState<string | null>(null)
  const [confirmBulkDel, setConfirmBulkDel] = useState(false)
  const [bulkSendEmail, setBulkSendEmail] = useState(false)
  const [confirmBulkPurge, setConfirmBulkPurge] = useState(false)
  const [confirmPurge, setConfirmPurge] = useState<AdminUser | null>(null)
  // overleaf-lab #4 (2026-09-08): single-user delete had NO confirm
  // (bulk + purge did) — same guard now.
  const [confirmDelete, setConfirmDelete] = useState<AdminUser | null>(null)
  const [purging, setPurging] = useState(false)
  const [infoUser, setInfoUser] = useState<AdminUser | null>(null)
  const [updateUser, setUpdateUser] = useState<AdminUser | null>(null)

  const PAGE_SIZE = 25

  const load = useCallback(async (p: number) => {
    setError(null)
    const filters: Record<string, unknown> = {}
    if (view === 'admins') filters.admin = true
    else if (view === 'suspended') filters.suspended = true
    else if (view === 'inactive') filters.inactive = true
    else if (view === 'deleted') filters.deleted = true
    else filters.all = true
    const q = search.trim()
    if (q) filters.search = q
    try {
      const data = await postJSON('/admin/users', {
        body: {
          sort: { by: 'signUpDate', order: 'desc' },
          page: { index: p, size: PAGE_SIZE },
          filters,
        },
      })
      const list = Array.isArray(data?.users) ? data.users : Array.isArray(data) ? data : []
      setUsers(list)
      setTotal(typeof data?.totalSize === 'number' ? data.totalSize : null)
    } catch (err: any) {
      setUsers([])
      setTotal(null)
      setError((err?.data?.message as string) || String(err?.message || err))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, search])

  useEffect(() => {
    setPage(1)
  }, [view, search])

  useEffect(() => {
    setUsers(null)
    void load(page)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [load, page])

  // Server-side filtering (POST /admin/users filters). This CE build returns
  // the full filtered list (page param accepted but not sliced server-side),
  // so page client-side: slice only when the list is larger than one page —
  // stays correct if a future build pages server-side too.
  const filtered = useMemo(() => {
    const list = users || []
    if (list.length <= PAGE_SIZE) return list
    const start = (page - 1) * PAGE_SIZE
    return list.slice(start, start + PAGE_SIZE)
  }, [users, page, PAGE_SIZE])

  const sel = useSelection(filtered.map(uid))

  // --- single-user actions -------------------------------------------------
  const setUserFlag = async (u: AdminUser, patch: Record<string, unknown>) => {
    setBusyId(uid(u))
    try {
      await postJSON(`/admin/user/${uid(u)}/update`, { body: patch })
      setUsers(list => (list || []).map(x => (uid(x) === uid(u) ? ({ ...x, ...patch } as any) : x)))
      notify({ message: 'User updated.', color: 'teal' })
    } catch (err: any) {
      notify({ message: (err?.data?.message as string) || 'Could not update the user.', color: 'red' })
    } finally {
      setBusyId(null)
    }
  }

  const sendActivation = async (u: AdminUser) => {
    try {
      await postJSON(`/admin/user/${uid(u)}/send-activation`, { body: {} })
      notify({ message: 'Activation email requested.', color: 'teal' })
    } catch (e: any) {
      notify({ message: (e?.data?.message as string) || 'Could not send activation.', color: 'red' })
    }
  }

  const doDeleteOne = async (u: AdminUser) => {
    setBusyId(uid(u))
    try {
      await postJSON(`/admin/user/${uid(u)}/delete`, { body: { sendEmail: false, toUserId: null } })
      notify({ message: `Deleted ${u.email}.`, color: 'gray' })
      sel.clear()
      await load()
    } catch (e: any) {
      notify({ message: (e?.data?.message as string) || 'Delete failed.', color: 'red' })
    } finally {
      setBusyId(null)
    }
  }

  const restoreDeleted = async (u: AdminUser) => {
    setBusyId(uid(u))
    try {
      await postJSON(`/admin/user/${uid(u)}/restore`, { body: {} })
      notify({ message: 'User restored.', color: 'teal' })
      await load()
    } catch (e: any) {
      notify({ message: (e?.data?.message as string) || 'Could not restore.', color: 'red' })
    } finally {
      setBusyId(null)
    }
  }

  const doPurgeOne = async () => {
    if (!confirmPurge) return
    setPurging(true)
    try {
      await deleteJSON(`/admin/user/${uid(confirmPurge)}`)
      notify({ message: `Purged ${confirmPurge.email}.`, color: 'gray' })
      setConfirmPurge(null)
      await load()
    } catch (e: any) {
      notify({ message: (e?.data?.message as string) || 'Purge failed.', color: 'red' })
      setConfirmPurge(null)
    } finally {
      setPurging(false)
    }
  }

  // --- bulk actions (owner #21) --------------------------------------------
  const selectedUsers = useMemo(() => filtered.filter(u => sel.isSelected(uid(u))), [filtered, sel])

  const runBulk = async (key: string, fn: () => Promise<unknown>, okMsg: string) => {
    setBulkBusy(key)
    let fail = 0
    try {
      for (const u of [...selectedUsers]) {
        try {
          await fn(u)
        } catch (e: any) {
          fail += 1
          notify({ message: `${u.email}: ${e?.data?.message || 'failed'}`, color: 'red' })
        }
      }
      if (fail === 0) notify({ message: okMsg, color: 'teal' })
      sel.clear()
      await load()
    } finally {
      setBulkBusy(null)
    }
  }

  const doBulkDelete = async () => {
    setBulkBusy('delete')
    let fail = 0
    try {
      for (const u of [...selectedUsers]) {
        try {
          await postJSON(`/admin/user/${uid(u)}/delete`, { body: { sendEmail: bulkSendEmail, toUserId: null } })
        } catch (e: any) {
          fail += 1
          notify({ message: `${u.email}: ${e?.data?.message || 'failed'}`, color: 'red' })
        }
      }
      setConfirmBulkDel(false)
      setBulkSendEmail(false)
      if (fail === 0) notify({ message: 'Users deleted.', color: 'teal' })
      sel.clear()
      await load()
    } finally {
      setBulkBusy(null)
    }
  }

  const doBulkPurge = async () => {
    setBulkBusy('purge')
    try {
      for (const u of [...selectedUsers]) {
        await deleteJSON(`/admin/user/${uid(u)}`)
      }
      setConfirmBulkPurge(false)
      notify({ message: 'Users permanently purged.', color: 'gray' })
      sel.clear()
      await load()
    } catch (e: any) {
      notify({ message: (e?.data?.message as string) || 'Purge failed.', color: 'red' })
      setConfirmBulkPurge(false)
    } finally {
      setBulkBusy(null)
    }
  }

  // Bulk toolbar per view (legacy user-tools parity + owner #21 list).
  const bulkActions: BulkAction[] = useMemo(() => {
    const L = bulkBusy
    if (view === 'deleted') {
      return [
        { key: 'restore', label: 'Restore', icon: 'restore_from_trash', loading: L === 'restore', onClick: () => void runBulk('restore', u => postJSON(`/admin/user/${uid(u)}/restore`, { body: {} }), 'Users restored.') },
        { key: 'purge', label: 'Purge', icon: 'delete_forever', tone: 'danger', onClick: () => setConfirmBulkPurge(true) },
      ]
    }
    const a: BulkAction[] = []
    if (view !== 'suspended') {
      a.push({ key: 'suspend', label: 'Suspend', icon: 'pause', loading: L === 'suspend', onClick: () => void runBulk('suspend', u => postJSON(`/admin/user/${uid(u)}/update`, { body: { suspended: true } }), 'Users suspended.') })
    }
    a.push({ key: 'resume', label: 'Resume', icon: 'play_arrow', loading: L === 'resume', onClick: () => void runBulk('resume', u => postJSON(`/admin/user/${uid(u)}/update`, { body: { suspended: false } }), 'Users resumed.') })
    if (view !== 'suspended') {
      a.push({ key: 'mail', label: 'Mail', icon: 'mail', loading: L === 'mail', onClick: () => void runBulk('mail', u => postJSON(`/admin/user/${uid(u)}/send-activation`, { body: {} }), 'Activation emails queued.') })
    }
    if (view !== 'admins' && view !== 'suspended') {
      a.push({ key: 'setadmin', label: 'Set admin', icon: 'shield_person', loading: L === 'setadmin', onClick: () => void runBulk('setadmin', u => postJSON(`/admin/user/${uid(u)}/update`, { body: { isAdmin: true } }), 'Admin role granted.') })
    }
    a.push({ key: 'unsetadmin', label: 'Unset admin', icon: 'admin_panel_settings', loading: L === 'unsetadmin', onClick: () => void runBulk('unsetadmin', u => postJSON(`/admin/user/${uid(u)}/update`, { body: { isAdmin: false } }), 'Admin role removed.') })
    a.push({ key: 'delete', label: 'Delete', icon: 'delete', tone: 'danger', onClick: () => setConfirmBulkDel(true) })
    return a
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, selectedUsers.length, bulkBusy])

  const isAllDeleted = view === 'deleted'
  const allSelectedIds = filtered.map(uid)
  const someSelected = sel.count > 0 && sel.count < allSelectedIds.length

  const createUser = async () => {
    if (!cEmail.trim()) {
      setCreateErr('Email is required.')
      return
    }
    setCreating(true)
    setCreateErr(null)
    try {
      await postJSON('/admin/user/create', {
        body: {
          email: cEmail.trim(),
          first_name: cFirst.trim(),
          last_name: cLast.trim(),
          isAdmin: cAdmin,
          canManageTemplates: cTemplates,
          isExternal: false,
        },
      })
      notify({ message: `Created ${cEmail.trim()}.`, color: 'teal' })
      setCreateOpen(false)
      setCEmail('')
      setCFirst('')
      setCLast('')
      setCAdmin(false)
      setCTemplates(false)
      await load()
    } catch (err: any) {
      setCreateErr((err?.data?.message as string) || 'Could not create the user.')
    } finally {
      setCreating(false)
    }
  }

  if (!users && !error) return <PageLoading label="Loading users…" />

  return (
    <Stack gap="md">
      <Group justify="space-between" wrap="wrap" gap="sm">
        <TextInput
          leftSection={<Icon name="search" size={18} />}
          value={search}
          onChange={e => setSearch(e.currentTarget.value)}
          placeholder="Search by name or email"
          aria-label="Search users by name or email"
          style={{ maxWidth: 380, width: '100%' }}
        />
        <Group gap="sm" wrap="wrap">
          <Button size="md" color="ollitex" leftSection={<Icon name="person_add" size={18} />} onClick={() => { setCreateOpen(true); setCreateErr(null) }}>
            New user
          </Button>
        </Group>
      </Group>

      {error ? <PageError label="Couldn’t load users" detail={error} onRetry={() => void load()} /> : null}

      {users && filtered.length > 0 ? (
        <BulkToolbar count={sel.count} actions={bulkActions} onClear={sel.clear} />
      ) : null}

      {users && filtered.length === 0 ? (
        <EmptyState icon="groups" title="No users found" hint={search ? `Nothing matched “${search}”.` : 'Create your first account.'} />
      ) : null}

      {users && filtered.length > 0 ? (
        <Table striped highlightOnHover withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
          <Table.Thead>
            <Table.Tr>
              <Table.Th style={{ width: 36 }}>
                <HeaderCheckbox checked={sel.count > 0 && !someSelected} indeterminate={someSelected} onChange={sel.toggleAll} label="Select all users in this view" />
              </Table.Th>
              <Table.Th>User</Table.Th>
              <Table.Th style={{ width: 150 }}>Role</Table.Th>
              {!isAllDeleted ? <Table.Th style={{ width: 110 }}>Active</Table.Th> : null}
              <Table.Th style={{ width: 110 }}>Created</Table.Th>
              <Table.Th style={{ width: 120 }}>Last active</Table.Th>
              <Table.Th style={{ width: 60, textAlign: 'right' }}>Actions</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {filtered.map(u => (
              <Table.Tr
                key={uid(u)}
                onClick={() => sel.toggle(uid(u))}
                style={{ cursor: 'pointer', background: sel.isSelected(uid(u)) ? 'var(--mantine-color-teal-0)' : undefined }}
              >
                <Table.Td onClick={e => e.stopPropagation()}>
                  <RowCheckbox id={uid(u)} label={u.email} selected={sel.isSelected(uid(u))} onToggle={sel.toggle} />
                </Table.Td>
                <Table.Td>
                  <Text size="sm" fw={600} ellipsis>{u.email}</Text>
                  <Text size="xs" c="dimmed" ellipsis>{[u.first_name, u.last_name].filter(Boolean).join(' ') || '—'}</Text>
                </Table.Td>
                <Table.Td>
                  <Group gap={4} wrap="wrap">
                    {u.isAdmin ? <Badge size="xs" variant="light" color="red" radius="sm">Admin</Badge> : <Badge size="xs" variant="subtle" radius="sm">User</Badge>}
                    {isTemplateAdmin(u) ? <Badge size="xs" variant="subtle" color="blue" radius="sm">Templates</Badge> : null}
                    {u.suspended ? <Badge size="xs" variant="light" color="orange" radius="sm">Suspended</Badge> : null}
                  </Group>
                </Table.Td>
                {!isAllDeleted ? (
                  <Table.Td onClick={e => e.stopPropagation()}>
                    <Switch
                    size="xs"
                    checked={!u.suspended}
                    color="teal"
                    loading={busyId === uid(u)}
                    onChange={() => void setUserFlag(u, { suspended: !u.suspended })}
                    aria-label={u.suspended ? `Activate (unsuspend) ${u.email}` : `Suspend ${u.email}`}
                  />
                  </Table.Td>
                ) : (
                  <Table.Td><Text size="xs" c="red">Deleted</Text></Table.Td>
                )}
                <Table.Td><Text size="sm" c="dimmed">{fmtDate(u.signUpDate)}</Text></Table.Td>
                <Table.Td><Text size="sm" c="dimmed">{fmtDate(u.lastActive)}</Text></Table.Td>
                <Table.Td style={{ textAlign: 'right' }} onClick={e => e.stopPropagation()}>
                  <Menu width={250} position="bottom-end">
                    <Menu.Target>
                      <ActionIcon variant="subtle" aria-label="Actions" style={{ cursor: 'pointer' }}>
                        <Icon name="more_vert" size={18} />
                      </ActionIcon>
                    </Menu.Target>
                    <Menu.Dropdown>
                      {!isAllDeleted ? (
                        <>
                          <Menu.Item
                            icon={<Icon name="mail" size={16} />}
                            onClick={() => void sendActivation(u)}
                          >
                            Send activation email
                          </Menu.Item>
                          <Menu.Item icon={<Icon name="info" size={16} />} onClick={() => setInfoUser(u)}>
                            User info
                          </Menu.Item>
                          <Menu.Item icon={<Icon name="edit" size={16} />} onClick={() => setUpdateUser(u)}>
                            Update…
                          </Menu.Item>
                          <Menu.Item
                            icon={<Icon name={u.isAdmin ? 'admin_panel_settings' : 'shield_person'} size={16} />}
                            onClick={() => void setUserFlag(u, { isAdmin: !u.isAdmin })}
                          >
                            {u.isAdmin ? 'Remove admin role' : 'Make admin'}
                          </Menu.Item>
                          <Menu.Item
                            icon={<Icon name="extension" size={16} />}
                            onClick={() => void setUserFlag(u, { canManageTemplates: !isTemplateAdmin(u) })}
                          >
                            {isTemplateAdmin(u) ? 'Remove template manage' : 'Allow template managing'}
                          </Menu.Item>
                          <Menu.Item
                            icon={<Icon name={u.suspended ? 'play_arrow' : 'pause'} size={16} />}
                            onClick={() => void setUserFlag(u, { suspended: !u.suspended })}
                          >
                            {u.suspended ? 'Resume user' : 'Suspend user'}
                          </Menu.Item>
                          <Menu.Divider />
                          <Menu.Item
                            icon={<Icon name="delete" size={16} />}
                            color="red"
                            onClick={() => setConfirmDelete(u)}
                          >
                            Delete user
                          </Menu.Item>
                        </>
                      ) : (
                        <>
                          <Menu.Item
                            icon={<Icon name="restore_from_trash" size={16} />}
                            color="teal"
                            loading={busyId === uid(u)}
                            onClick={() => void restoreDeleted(u)}
                          >
                            Restore user
                          </Menu.Item>
                          <Menu.Divider />
                          <Menu.Item
                            icon={<Icon name="delete_forever" size={16} />}
                            color="red"
                            onClick={() => setConfirmPurge(u)}
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

      {infoUser ? <UserInfoModal user={infoUser} onClose={() => setInfoUser(null)} /> : null}
      {updateUser ? <UserUpdateModal user={updateUser} onClose={() => setUpdateUser(null)} onSaved={u => {
        setUsers(list => (list || []).map(x => (uid(x) === uid(u) ? ({ ...x } as any) : x)))
        void load(page)
      }} /> : null}

      <Modal opened={createOpen} onClose={() => setCreateOpen(false)} size="sm" title="New user">
        <Stack gap="md">
          <div>
            <Text size="sm" fw={600} mb={6}>Email <span style={{ color: 'var(--mantine-color-red-6)' }}>*</span></Text>
            <TextInput value={cEmail} onChange={e => setCEmail(e.currentTarget.value)} placeholder="name@example.org" aria-label="Email" />
          </div>
          <Group gap="md" wrap="wrap">
            <div style={{ flex: 1, minWidth: 160 }}>
              <Text size="sm" fw={600} mb={6}>First name</Text>
              <TextInput value={cFirst} onChange={e => setCFirst(e.currentTarget.value)} aria-label="First name" />
            </div>
            <div style={{ flex: 1, minWidth: 160 }}>
              <Text size="sm" fw={600} mb={6}>Last name</Text>
              <TextInput value={cLast} onChange={e => setCLast(e.currentTarget.value)} aria-label="Last name" />
            </div>
          </Group>
          <Group gap="lg" wrap="wrap">
            <Switch checked={cAdmin} onChange={() => setCAdmin(!cAdmin)} label="Administrator" color="ollitex" />
            <Switch checked={cTemplates} onChange={() => setCTemplates(!cTemplates)} label="Can manage templates" color="ollitex" />
          </Group>
          <Text size="xs" c="dimmed">The user must confirm their account via the activation email.</Text>
          {createErr ? <Text size="sm" c="red">{createErr}</Text> : null}
          <Group justify="flex-end" gap="xs" mt="sm">
            <Button variant="default" onClick={() => setCreateOpen(false)} disabled={creating}>Cancel</Button>
            <Button color="ollitex" loading={creating} onClick={() => void createUser()}>Create user</Button>
          </Group>
        </Stack>
      </Modal>

      <Modal opened={confirmBulkDel} onClose={() => setConfirmBulkDel(false)} size="sm" title={`Delete ${sel.count} user${sel.count === 1 ? '' : 's'}?`}>
        <Stack gap="md">
          <Text size="sm">
            The selected {sel.count === 1 ? 'user' : 'users'} will be deleted. Projects they owned follow the instance deletion policy.
          </Text>
          <Checkbox label="Send notification email" checked={bulkSendEmail} onChange={checked => setBulkSendEmail(checked === true)} color="ollitex" />
          <Group justify="flex-end" gap="xs">
            <Button variant="default" onClick={() => setConfirmBulkDel(false)}>Cancel</Button>
            <Button color="red" variant="filled" loading={bulkBusy === 'delete'} onClick={() => void doBulkDelete()}>Delete</Button>
          </Group>
        </Stack>
      </Modal>

      <ConfirmModal
        open={confirmBulkPurge}
        title={`Purge ${sel.count} user${sel.count === 1 ? '' : 's'} permanently?`}
        body="Deleted user data (profile, activity, projects) will be irreversibly removed."
        confirmLabel="Purge"
        danger
        loading={bulkBusy === 'purge'}
        onCancel={() => setConfirmBulkPurge(false)}
        onConfirm={() => void doBulkPurge()}
      />

      <ConfirmModal
        open={!!confirmPurge}
        title="Purge user permanently?"
        body={confirmPurge ? `“${confirmPurge.email}” and all related data will be irreversibly removed.` : ''}
        confirmLabel="Purge"
        danger
        loading={purging}
        onCancel={() => setConfirmPurge(null)}
        onConfirm={() => void doPurgeOne()}
      />

      {/* overleaf-lab #4: confirm before the (soft) single-user delete */}
      <ConfirmModal
        open={!!confirmDelete}
        title={`Delete “${confirmDelete?.email ?? ''}”?`}
        body="The user and their projects move to the deleted view and can be restored until they are purged. This cannot be undone from the normal UI. Note: no email is sent to the user for this action."
        confirmLabel="Delete user"
        danger
        loading={!!confirmDelete && busyId === uid(confirmDelete)}
        onCancel={() => setConfirmDelete(null)}
        onConfirm={() => { const u = confirmDelete; setConfirmDelete(null); if (u) void doDeleteOne(u) }}
      />
          {total != null && total > PAGE_SIZE ? (
        <Group justify="space-between" wrap="wrap" gap="xs">
          <Text size="sm" c="dimmed">
            Page {page} of {Math.max(1, Math.ceil(total / PAGE_SIZE))} — {total} user{total === 1 ? '' : 's'}
          </Text>
          <HubPagination page={page} total={Math.ceil(total / PAGE_SIZE)} onChange={setPage} />
        </Group>
      ) : null}
    </Stack>
  )
}
