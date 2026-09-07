import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ActionIcon,
    Badge,
  Button,
  Group,
  Menu,
  NativeSelect,
  Stack,
  Table,
  Text,
  TextInput,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { deleteJSON, getJSON, postJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import ConfirmModal from '../../shared/confirm-modal'
import { EmptyState, PageError, PageLoading } from '../../shared/page-state'

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
  return p?.owner_ref || p?.ownerId || p?.owner?.email || ''
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
  const [confirmPurge, setConfirmPurge] = useState<AdminProject | null>(null)
  const [purging, setPurging] = useState(false)

  useEffect(() => {
    void (async () => {
      try {
        const data = await postJSON('/admin/users', { body: { sort: { by: 'signUpDate', order: 'desc' } } })
        const list = Array.isArray(data?.users) ? data.users : []
        setUsers(list.filter((u: any) => u && (u.email || u._id || u.id)))
        for (const u of list) if (u._id && u.email) ownerEmails[u._id] = u.email
      } catch {
        setUsers([])
      }
    })()
  }, [])

  const load = useCallback(async () => {
    setError(null)
    try {
      const userId = scope === 'all' ? 'null' : scope
      const data = await postJSON(`/admin/user/${encodeURIComponent(userId)}/projects`, {
        body: { sort: { by: 'lastUpdated', order: 'desc' } },
      })
      const list = Array.isArray(data?.projects) ? data.projects : []
      setProjects(list)
    } catch (err: any) {
      setProjects([])
      setError((err?.data?.message as string) || String(err?.message || err))
    }
  }, [scope])

  useEffect(() => {
    setProjects(null)
    void load()
  }, [load])

  const filtered = useMemo(() => {
    if (!projects) return []
    const q = search.trim().toLowerCase()
    const INACTIVE_MS = 15 * 24 * 3600 * 1000
    const isInactive = (p: AdminProject) => {
      const t0 = p.lastOpened || p.lastActive
      if (!t0) return true
      const t = new Date(t0).getTime()
      if (Number.isNaN(t)) return true
      return Date.now() - t > INACTIVE_MS
    }
    return projects.filter(p => {
      switch (view) {
        case 'deleted':
          return Boolean(p.deleted || p.deletedAt)
        case 'trashed':
          return Boolean(p.trashed) && !(p.deleted || p.deletedAt)
        case 'inactive':
          return !p.trashed && !(p.deleted || p.deletedAt) && isInactive(p)
        default:
          return !p.trashed && !(p.deleted || p.deletedAt)
      }
    }).filter(p => {
      if (!q) return true
      return (pname(p) || '').toLowerCase().includes(q)
    })
  }, [projects, search, view])

  const act = async (fn: () => Promise<unknown>, okMsg: string) => {
    try {
      await fn()
      notifications.show({ message: okMsg, color: 'teal' })
      load()
    } catch (err: any) {
      notifications.show({
        message: (err?.data?.message as string) || 'Action failed.',
        color: 'red',
      })
    }
  }

  const userLabel = (u: any) => (u ? `${u.first_name || ''} ${u.last_name || ''} ${u.email || ''}`.trim() || u._id || u.id || '' : '')

  const ownerSelectData = [
    { value: 'all', label: 'All users' },
    ...users.map((u: any) => ({
      value: u._id || u.id,
      label: (u.email || 'user') + (u.first_name ? ` (${userLabel(u)})` : ''),
    })),
  ]

  if (!projects && !error) return <PageLoading label="Loading projects…" />

  return (
    <Stack gap="md">
      <Group justify="space-between" wrap="wrap" gap="sm">
        <Group gap="xs" wrap="wrap">
          <div style={{ width: 280 }}>
            <NativeSelect
              value={scope}
              onChange={e => setScope(e.currentTarget.value)}
              data={ownerSelectData}
              size="sm"
            />
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

      {projects && filtered.length === 0 ? (
        <EmptyState icon="folder_off" title="No projects" hint={search ? `Nothing matched “${search}”.` : 'No projects found for this scope yet.'} />
      ) : null}

      {projects && filtered.length > 0 ? (
        <Table striped highlightOnHover withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
          <Table.Thead>
            <Table.Tr>
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
              <Table.Tr key={pid(p)}>
                <Table.Td>
                  <Text size="sm" fw={600} ellipsis>
                    {pname(p)}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" c="dimmed" ellipsis>
                    {ownerEmails[powner(p)] || powner(p) || '—'}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" c="dimmed">
                    {pdate(p, 'lastOpened')}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" c="dimmed">
                    {pdate(p, 'lastUpdated')}
                  </Text>
                </Table.Td>
                <Table.Td>
                  {p.deleted ? (
                    <Badge size="xs" color="red" radius="sm" variant="light">
                      Deleted
                    </Badge>
                  ) : p.trashed ? (
                    <Badge size="xs" color="orange" radius="sm" variant="light">
                      Trash
                    </Badge>
                  ) : (
                    <Badge size="xs" color="teal" radius="sm" variant="light">
                      Active
                    </Badge>
                  )}
                </Table.Td>
                <Table.Td style={{ textAlign: 'right' }}>
                  <Menu width={250} position="bottom-end">
                    <Menu.Target>
                      <ActionIcon variant="subtle" aria-label="Actions" style={{ cursor: 'pointer' }}>
                        <Icon name="more_vert" size={18} />
                      </ActionIcon>
                    </Menu.Target>
                    <Menu.Dropdown>
                      <Menu.Item
                        icon={<Icon name="open_in_new" size={16} />}
                        component="a"
                        href={`/project/${pid(p)}`}
                        target="_blank"
                        rel="noreferrer"
                      >
                        Open project
                      </Menu.Item>
                      {!p.trashed && !p.deleted ? (
                        <Menu.Item
                          icon={<Icon name="delete" size={16} />}
                          color="orange"
                          onClick={() =>
                            void act(
                              () => postJSON(`/admin/project/${pid(p)}/trash`, { body: { userId: powner(p) } }),
                              'Moved to trash.'
                            )
                          }
                        >
                          Move to trash
                        </Menu.Item>
                      ) : null}
                      {p.trashed && !p.deleted ? (
                        <Menu.Item
                          icon={<Icon name="restore_from_trash" size={16} />}
                          color="teal"
                          onClick={() =>
                            void act(
                              () => postJSON(`/admin/project/${pid(p)}/untrash`, { body: { userId: powner(p) } }),
                              'Restored.'
                            )
                          }
                        >
                          Restore
                        </Menu.Item>
                      ) : null}
                      {!p.deleted ? (
                        <Menu.Item
                          icon={<Icon name="delete" size={16} />}
                          color="red"
                          onClick={() =>
                            void act(
                              () => deleteJSON(`/admin/project/${pid(p)}`),
                              'Deleted (recoverable from trash).'
                            )
                          }
                        >
                          Delete
                        </Menu.Item>
                      ) : null}
                      {p.deleted ? (
                        <>
                          <Menu.Item
                            icon={<Icon name="restore_from_trash" size={16} />}
                            color="teal"
                            onClick={() =>
                              void act(
                                () => postJSON(`/admin/project/${pid(p)}/undelete`, { body: { userId: powner(p) } }),
                                'Undeleted.'
                              )
                            }
                          >
                            Undelete
                          </Menu.Item>
                          <Menu.Divider />
                          <Menu.Item
                            icon={<Icon name="delete_forever" size={16} />}
                            color="red"
                            onClick={() => setConfirmPurge(p)}
                          >
                            Purge permanently
                          </Menu.Item>
                        </>
                      ) : null}
                    </Menu.Dropdown>
                  </Menu>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      ) : null}

      <ConfirmModal
        open={!!confirmPurge}
        title="Purge project permanently?"
        body={
          confirmPurge
            ? `“${pname(confirmPurge)}” and all of its documents will be irreversibly deleted.`
            : ''
        }
        confirmLabel="Purge"
        danger
        loading={purging}
        onCancel={() => setConfirmPurge(null)}
        onConfirm={() => {
          setPurging(true)
          void act(() => deleteJSON(`/admin/project/${pid(confirmPurge as AdminProject)}/purge`), 'Project purged.')
            .then(() => setConfirmPurge(null))
            .catch(() => setConfirmPurge(null))
            .finally(() => setPurging(false))
        }}
      />
    </Stack>
  )
}
