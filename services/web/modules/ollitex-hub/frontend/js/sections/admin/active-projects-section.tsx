import React, { useEffect, useState } from 'react'
import { Alert, Paper, Stack, Text, Title } from '@mantine/core'
import { getJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'

interface ActiveUser { name?: string; email?: string; _id?: string }
interface ActiveProject { name?: string; _id?: string; activeUsers?: ActiveUser[] }

/**
 * Active projects — parity of the legacy /admin/panel "Active projects" pane
 * (the real-time "who is editing right now" list; NOT the project manager).
 *
 * Contract (admin-tools active-projects.js):
 *   GET /admin/active-projects → [{ name, activeUsers: [{ name, email }] }]
 * Empty state: "Great news! No projects are currently being (actively) edited."
 */
export default function ActiveProjectsSection() {
  const [items, setItems] = useState<ActiveProject[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let alive = true
    getJSON('/admin/active-projects')
      .then((d: any) => {
        if (!alive) return
        setItems(Array.isArray(d) ? d : d?.projects || [])
      })
      .catch((e: any) => {
        if (alive) setError(e?.message || 'Failed to load active projects')
      })
    return () => {
      alive = false
    }
  }, [])

  return (
    <Stack gap="md" w="100%">
      <div>
        <Title order={2} fw={800}>Active projects</Title>
        <Text size="sm" c="dimmed" mt={4}>
          Real-time information from the editor service showing who is actively working on which
          projects.
        </Text>
      </div>

      {error ? (
        <Alert color="red" icon={<Icon name="error" size={18} />}>
          Failed to load active projects
        </Alert>
      ) : items === null ? (
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
            No projects are currently being actively edited.
          </Text>
        </Paper>
      ) : (
        <Stack gap="sm">
          {items.map((pr, i) => {
            const users = Array.isArray(pr.activeUsers) ? pr.activeUsers : []
            return (
              <Paper key={pr._id || i} p="md" withBorder>
                <div style={{ display: 'grid', gridTemplateColumns: '70% 30%', columnGap: '1rem' }}>
                  <div>
                    <Text fw={700}>{pr.name || '(unnamed project)'}</Text>
                    <Text size="xs" c="dimmed" mt={2}>
                      {users.length} active editor{users.length === 1 ? '' : 's'}
                    </Text>
                  </div>
                  <Stack gap={4} align="flex-end">
                    {users.map((u, j) =>
                      u.email ? (
                        <a key={j} href={`mailto:${u.email}`} style={{ textDecoration: 'none', fontSize: 13 }}>
                          {u.name || u.email}
                        </a>
                      ) : (
                        <Text key={j} size="sm">
                          {u.name || 'Unknown user'}
                        </Text>
                      ),
                    )}
                  </Stack>
                </div>
              </Paper>
            )
          })}
        </Stack>
      )}
    </Stack>
  )
}
