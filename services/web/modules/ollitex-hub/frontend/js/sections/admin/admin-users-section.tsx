import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ActionIcon,
  Badge,
  Button,
    Group,
  Menu,
  Modal,
  Stack,
  Switch,
  Table,
  Text,
  Tooltip,
  TextInput,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { getJSON, postJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import ConfirmModal from '../../shared/confirm-modal'
import { EmptyState, PageError, PageLoading } from '../../shared/page-state'

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

export default function AdminUsersSection() {
  const [users, setUsers] = useState<AdminUser[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [showInactive, setShowInactive] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [cEmail, setCEmail] = useState('')
  const [cFirst, setCFirst] = useState('')
  const [cLast, setCLast] = useState('')
  const [cAdmin, setCAdmin] = useState(false)
  const [cTemplates, setCTemplates] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createErr, setCreateErr] = useState<string | null>(null)
  const [confirmDel, setConfirmDel] = useState<AdminUser | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [busyId, setBusyId] = useState<string | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      const data = await postJSON('/admin/users', {
        body: { sort: { by: 'signUpDate', order: 'desc' } },
      })
      const list = Array.isArray(data?.users) ? data.users : Array.isArray(data) ? data : []
      setUsers(list)
    } catch (err: any) {
      setUsers([])
      setError((err?.data?.message as string) || String(err?.message || err))
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const filtered = useMemo(() => {
    if (!users) return []
    const q = search.trim().toLowerCase()
    return users.filter(u => {
      if (u.deletedAt) return false
      if (!showInactive && u.suspended) return false
      if (!q) return true
      return (
        (u.email || '').toLowerCase().includes(q) ||
        `${u.first_name || ''} ${u.last_name || ''}`.toLowerCase().includes(q)
      )
    })
  }, [users, search, showInactive])

  const setUserFlag = async (u: AdminUser, patch: Record<string, unknown>) => {
    setBusyId(uid(u))
    try {
      await postJSON(`/admin/user/${uid(u)}/update`, { body: patch })
      setUsers(list =>
        (list || []).map(x => (uid(x) === uid(u) ? ({ ...x, ...patch } as any) : x))
      )
      notifications.show({ message: 'User updated.', color: 'teal' })
    } catch (err: any) {
      notifications.show({
        message: (err?.data?.message as string) || 'Could not update the user.',
        color: 'red',
      })
    } finally {
      setBusyId(null)
    }
  }

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
      notifications.show({ message: `Created ${cEmail.trim()}.`, color: 'teal' })
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

  const doDelete = async () => {
    if (!confirmDel) return
    setDeleting(true)
    try {
      await postJSON(`/admin/user/${uid(confirmDel)}/delete`, {
        body: { sendEmail: false, toUserId: null },
      })
      notifications.show({ message: `Deleted ${confirmDel.email}.`, color: 'gray' })
      setConfirmDel(null)
      await load()
    } catch (err: any) {
      notifications.show({
        message: (err?.data?.message as string) || 'Delete failed.',
        color: 'red',
      })
      setConfirmDel(null)
    } finally {
      setDeleting(false)
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
          style={{ maxWidth: 380, width: '100%' }}
        />
        <Group gap="sm" wrap="wrap">
          <Switch
            checked={showInactive}
            onChange={setShowInactive}
            label="Show inactive"
            size="sm"
            color="ollitex"
          />
          <Button
            size="md"
            color="ollitex"
            leftSection={<Icon name="person_add" size={18} />}
            onClick={() => {
              setCreateOpen(true)
              setCreateErr(null)
            }}
          >
            New user
          </Button>
        </Group>
      </Group>

      {error ? <PageError label="Couldn’t load users" detail={error} onRetry={() => void load()} /> : null}

      {users && filtered.length === 0 ? (
        <EmptyState
          icon="groups"
          title="No users found"
          hint={search ? `Nothing matched “${search}”.` : 'Create your first account.'}
        />
      ) : null}

      {users && filtered.length > 0 ? (
        <Table striped highlightOnHover withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>User</Table.Th>
              <Table.Th style={{ width: 150 }}>Role</Table.Th>
              <Table.Th style={{ width: 110 }}>Status</Table.Th>
              <Table.Th style={{ width: 110 }}>Created</Table.Th>
              <Table.Th style={{ width: 120 }}>Last active</Table.Th>
              <Table.Th style={{ width: 60, textAlign: 'right' }}>Actions</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {filtered.map(u => (
              <Table.Tr key={uid(u)}>
                <Table.Td>
                  <Text size="sm" fw={600} ellipsis>
                    {u.email}
                  </Text>
                  <Text size="xs" c="dimmed" ellipsis>
                    {[u.first_name, u.last_name].filter(Boolean).join(' ') || '—'}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Group gap={4} wrap="wrap">
                    {u.isAdmin ? (
                      <Badge size="xs" variant="light" color="red" radius="sm">
                        Admin
                      </Badge>
                    ) : (
                      <Badge size="xs" variant="subtle" radius="sm">
                        User
                      </Badge>
                    )}
                    {isTemplateAdmin(u) ? (
                      <Badge size="xs" variant="subtle" color="blue" radius="sm">
                        Templates
                      </Badge>
                    ) : null}
                    {u.suspended ? (
                      <Badge size="xs" variant="light" color="orange" radius="sm">
                        Suspended
                      </Badge>
                    ) : null}
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Switch
                    size="xs"
                    checked={!u.suspended}
                    color="teal"
                    loading={busyId === uid(u)}
                    onChange={() => void setUserFlag(u, { suspended: !u.suspended })}
                  />
                </Table.Td>
                <Table.Td>
                  <Text size="sm" c="dimmed">
                    {fmtDate(u.signUpDate)}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" c="dimmed">
                    {fmtDate(u.lastActive)}
                  </Text>
                </Table.Td>
                <Table.Td style={{ textAlign: 'right' }}>
                  <Menu width={240} position="bottom-end">
                    <Menu.Target>
                      <ActionIcon variant="subtle" aria-label="Actions" style={{ cursor: 'pointer' }}>
                        <Icon name="more_vert" size={18} />
                      </ActionIcon>
                    </Menu.Target>
                    <Menu.Dropdown>
                      <Menu.Item
                        icon={<Icon name="mail" size={16} />}
                        onClick={() =>
                          void (
                            postJSON(`/admin/user/${uid(u)}/send-activation`, { body: {} })
                              .then(() => notifications.show({ message: 'Activation email requested.', color: 'teal' }))
                              .catch((e: any) =>
                                notifications.show({
                                  message: (e?.data?.message as string) || 'Could not send activation.',
                                  color: 'red',
                                })
                              )
                          )
                        }
                      >
                        Send activation email
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
                      {u.suspended ? (
                        <Menu.Item
                          icon={<Icon name="restore" size={16} />}
                          color="teal"
                          onClick={() =>
                            void (
                              postJSON(`/admin/user/${uid(u)}/restore`, { body: {} })
                                .then(() => {
                                  notifications.show({ message: 'User restored.', color: 'teal' })
                                  load()
                                })
                                .catch((e: any) =>
                                  notifications.show({
                                    message: (e?.data?.message as string) || 'Could not restore.',
                                    color: 'red',
                                  })
                                )
                            )
                          }
                        >
                          Restore user
                        </Menu.Item>
                      ) : null}
                      <Menu.Divider />
                      <Menu.Item
                        icon={<Icon name="delete" size={16} />}
                        color="red"
                        onClick={() => setConfirmDel(u)}
                      >
                        Delete user
                      </Menu.Item>
                    </Menu.Dropdown>
                  </Menu>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      ) : null}

      <Modal
        opened={createOpen}
        onClose={() => setCreateOpen(false)}
        size="sm"
        title="New user"
      >
        <Stack gap="md">
          <div>
            <Text size="sm" fw={600} mb={6}>
              Email <span style={{ color: 'var(--mantine-color-red-6)' }}>*</span>
            </Text>
            <TextInput value={cEmail} onChange={e => setCEmail(e.currentTarget.value)} placeholder="name@example.org" />
          </div>
          <Group gap="md" wrap="wrap">
            <div style={{ flex: 1, minWidth: 160 }}>
              <Text size="sm" fw={600} mb={6}>
                First name
              </Text>
              <TextInput value={cFirst} onChange={e => setCFirst(e.currentTarget.value)} />
            </div>
            <div style={{ flex: 1, minWidth: 160 }}>
              <Text size="sm" fw={600} mb={6}>
                Last name
              </Text>
              <TextInput value={cLast} onChange={e => setCLast(e.currentTarget.value)} />
            </div>
          </Group>
          <Group gap="lg" wrap="wrap">
            <Switch checked={cAdmin} onChange={setCAdmin} label="Administrator" color="ollitex" />
            <Switch checked={cTemplates} onChange={setCTemplates} label="Can manage templates" color="ollitex" />
          </Group>
          <Text size="xs" c="dimmed">
            The user must confirm their account via the activation email.
          </Text>
          {createErr ? <Text size="sm" c="red">{createErr}</Text> : null}
          <Group justify="flex-end" gap="xs" mt="sm">
            <Button variant="default" onClick={() => setCreateOpen(false)} disabled={creating}>
              Cancel
            </Button>
            <Button color="ollitex" loading={creating} onClick={() => void createUser()}>
              Create user
            </Button>
          </Group>
        </Stack>
      </Modal>

      <ConfirmModal
        open={!!confirmDel}
        title="Delete user?"
        body={
          confirmDel
            ? `“${confirmDel.email}” will be removed. Projects they owned will follow the instance deletion policy.`
            : ''
        }
        confirmLabel="Delete user"
        danger
        loading={deleting}
        onCancel={() => setConfirmDel(null)}
        onConfirm={() => void doDelete()}
      />
    </Stack>
  )
}
