import React, { useCallback, useEffect, useState } from 'react'
import {
  ActionIcon,
  Anchor,
  Badge,
  Button,
  Card,
  Group,
  SimpleGrid,
  Stack,
  Text,
  Title,
} from '@mantine/core'
import { getJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import { PageError, PageLoading } from '../../shared/page-state'

const METRICS = [
  { key: 'user_count', label: 'Users', suffix: '' },
  { key: 'project_count', label: 'Projects', suffix: '' },
  { key: 'active_users', label: 'Active users (24h)', suffix: '' },
  { key: 'active_projects', label: 'Active projects (24h)', suffix: '' },
  { key: 'overleaf_storage', label: 'Project storage', bytes: true },
  { key: 'mongodb_storage', label: 'Database storage', bytes: true },
]

function fmtBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i += 1
  }
  return `${v.toFixed(v >= 100 ? 0 : 1)} ${units[i]}`
}

function KpiCard({ label, value, icon }: { label: string; value: string; icon: string }) {
  return (
    <Card withBorder paddings="md" radius="lg">
      <Group justify="space-between" wrap="nowrap">
        <div>
          <Text size="xs" c="dimmed" fw={600}>
            {label}
          </Text>
          <Title order={3} style={{ margin: '6px 0 0', fontSize: 22, fontWeight: 700 }} truncate>
            {value}
          </Title>
        </div>
        <span className="material-symbols" style={{ fontSize: 28, color: 'var(--mantine-color-ollitex-6)' }}>
          {icon}
        </span>
      </Group>
    </Card>
  )
}

export default function AdminInstanceSection({ onNavigate }: { onNavigate?: (id: string) => void }) {
  const [values, setValues] = useState<Record<string, number | null>>({})
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    const results: Record<string, number | null> = {}
    for (const m of METRICS) {
      try {
        const data = await getJSON(
          `/admin/instance-stats/api/series?metric=${m.key}&window=month`
        )
        const points = Array.isArray(data?.points) ? data.points : []
        const last = points[points.length - 1]
        const vals = last?.values
        results[m.key] = Array.isArray(vals) && vals.length ? Number(vals[vals.length - 1]) : null
      } catch {
        results[m.key] = null
      }
    }
    setValues(results)
    setLoading(false)
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const icons: Record<string, string> = {
    user_count: 'person',
    project_count: 'folder',
    active_users: 'groups',
    active_projects: 'layers',
    overleaf_storage: 'storage',
    mongodb_storage: 'dns',
  }

  const navLinks = [
    { id: 'site', label: 'Site settings', icon: 'settings' },
    { id: 'users', label: 'Manage users', icon: 'manage_accounts' },
    { id: 'projects', label: 'Manage projects', icon: 'folder_managed' },
    { id: 'templates', label: 'Templates', icon: 'extension' },
    { id: 'llm', label: 'LLM instance', icon: 'smart_toy' },
  ]

  if (loading) return <PageLoading label="Collecting instance statistics…" />

  return (
    <Stack gap="md">
      {error ? <PageError label="Couldn’t load instance stats" detail={error} onRetry={() => void load()} /> : null}
      <SimpleGrid cols={{ base: 1, sm: 2, xl: 3 }} spacing="md">
        {METRICS.map(m => (
          <KpiCard
            key={m.key}
            label={m.label}
            icon={icons[m.key]}
            value={
              values[m.key] == null
                ? '—'
                : m.bytes
                  ? fmtBytes(Number(values[m.key]))
                  : String(Math.round(Number(values[m.key])))
            }
          />
        ))}
      </SimpleGrid>

      <Card withBorder paddings="md" radius="lg">
        <Group justify="space-between" wrap="wrap" gap="sm">
          <div>
            <Text fw={700}>Instance management</Text>
            <Text size="sm" c="dimmed" mt={4}>
              Shortcuts to the administration sections of this hub.
            </Text>
          </div>
          <Group gap="xs" wrap="wrap">
            {navLinks.map(link => (
              <Button
                key={link.id}
                size="sm"
                variant="light"
                color="ollitex"
                leftSection={<Icon name={link.icon} size={16} />}
                onClick={() => onNavigate?.(link.id)}
              >
                {link.label}
              </Button>
            ))}
          </Group>
        </Group>
      </Card>

      <Card withBorder paddings="md" radius="lg">
        <Stack gap="sm">
          <Text fw={700}>About this instance</Text>
          <Group gap="md" wrap="wrap">
            <Badge variant="light" color="blue" radius="sm" size="sm">
              LibreLeaf 6.3.0 (Community Edition base)
            </Badge>
            <Text size="sm" c="dimmed">
              Full instance stats (time series, alerts, retention) remain available on the
              dedicated page.
            </Text>
          </Group>
          <Group gap="xs">
            <Anchor href="/admin/instance-stats" target="_blank" size="sm">
              Open full instance stats
            </Anchor>
          </Group>
        </Stack>
      </Card>
    </Stack>
  )
}
