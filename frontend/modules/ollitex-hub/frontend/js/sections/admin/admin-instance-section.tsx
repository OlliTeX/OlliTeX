import React, { useCallback, useEffect, useState } from 'react'
import {
  Card,
  Group,
  SimpleGrid,
  Stack,
  Text,
  Title,
} from '@mantine/core'
import { getJSON } from '@/infrastructure/fetch-json'
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

export default function AdminInstanceSection() {
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
    </Stack>
  )
}
