import React from 'react'
import { Alert, Button, Group, Paper, Stack, Text, Title } from '@mantine/core'
import { getJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import { useSectionData } from '../../shared/use-section-data'

interface ActiveUser {
  name?: string
  email?: string
  _id?: string
}
interface ActiveProject {
  name?: string
  _id?: string
  activeUsers?: ActiveUser[]
}

const LABEL = 'GET /admin/active-projects'

/**
 * Active projects — parity of the legacy /admin/panel "Active projects" pane
 * (the real-time "who is editing right now" list; NOT the project manager).
 *
 * overleaf-lab #6 (2026-09-08): LIVE pane — 30 s heartbeat + tab-activation
 * refetch (useSectionData), plus "last updated" and an instant refresh
 * button. The legacy page only refetched on page load.
 *
 * Contract (admin-tools active-projects.js):
 *   GET /admin/active-projects → [{ name, activeUsers: [{ name, email }] }]
 * Empty state: "Great news! No projects are currently being (actively) edited."
 */
export default function ActiveProjectsSection() {
  const {
    data: items,
    loading,
    error,
    lastUpdated,
    refetch,
  } = useSectionData<ActiveProject[]>(
    async () => {
      const d: any = await getJSON('/admin/active-projects')
      return Array.isArray(d) ? d : d?.projects || []
    },
    { label: LABEL, live: true },
  )

  return (
    <Stack gap="md" w="100%">
      <div>
        <Group justify="space-between" align="flex-start" wrap="wrap" gap="xs">
          <div>
            <Title order={2} fw={800}>
              Active projects
            </Title>
            <Text size="sm" c="dimmed" mt={4}>
              Real-time information from the editor service showing who is actively working on
              which projects. overleaf-lab #6: live —
              {lastUpdated ? ` last updated ${new Date(lastUpdated).toLocaleTimeString()}` : ' auto-refreshes every 30 s while visible'}.
            </Text>
          </div>
          <Button size="xs" variant="light" onClick={() => void refetch(true)} loading={loading}>
            <span className="material-symbols" style={{ fontSize: 14, verticalAlign: '-2px' }}>
              refresh
            </span>{' '}
            Refresh now
          </Button>
        </Group>
      </div>

      {error ? (
        <Alert color="red" icon={<Icon name="error" size={18} />}>
          Failed to load active projects — {error}
        </Alert>
      ) : !items ? (
        <Paper p="md">
          <Text size="sm" c="dimmed">
            Loading…
          </Text>
        </Paper>
      ) : items.length === 0 ? (
        <Paper p="md" withBorder>
          <Text fw={700} c="teal" size="sm">
            ✔ Great news!
          </Text>
          <Text size="sm" mt={4}>
            No projects are currently being actively edited. This list updates automatically as
            users start and stop editing.
          </Text>
        </Paper>
      ) : (
        <Stack gap="sm">
          {items.map(p => (
            <Paper key={p._id || p.name} p="md" withBorder>
              <Text fw={700}>
                {p.name || '(unnamed project)'}
              </Text>
              <Group gap={6} wrap="wrap" mt={6}>
                {(p.activeUsers || []).map((u, i) => (
                  <span
                    key={u._id || u.email || i}
                    style={{
                      padding: '2px 10px',
                      borderRadius: 999,
                      background: 'var(--mantine-color-default-hover)',
                      fontSize: 12,
                    }}
                  >
                    {u.name || u.email}
                  </span>
                ))}
                {!(p.activeUsers || []).length ? (
                  <Text size="xs" c="dimmed">
                    no user details reported
                  </Text>
                ) : null}
              </Group>
            </Paper>
          ))}
        </Stack>
      )}
    </Stack>
  )
}
