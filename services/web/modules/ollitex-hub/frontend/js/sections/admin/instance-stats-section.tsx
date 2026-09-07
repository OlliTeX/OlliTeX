import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Anchor, Button, Card, Group, NativeSelect, SimpleGrid, Stack, Text } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import Icon from '../../shared/icons'
import { fetchSeries } from '@modules/instance-stats/frontend/js/features/instance-stats/api'
import { WINDOW_OPTIONS } from '@modules/instance-stats/frontend/js/features/instance-stats/config'
import type { WindowKey } from '@modules/instance-stats/frontend/js/features/instance-stats/types'

type MetricDef = { metric: string; label: string; unit?: 'bytes-gb' | 'pct' | 'none' }

const METRICS: MetricDef[] = [
  { metric: 'user_count', label: 'Users' },
  { metric: 'new_users', label: 'New users' },
  { metric: 'active_users', label: 'Active users' },
  { metric: 'project_count', label: 'Projects' },
  { metric: 'active_projects', label: 'Active projects' },
  { metric: 'file_count', label: 'Files' },
  { metric: 'disk_usage', label: 'Disk usage', unit: 'bytes-gb' },
  { metric: 'ram_usage', label: 'RAM usage', unit: 'bytes-gb' },
]

function fmt(value: number, unit?: MetricDef['unit']): string {
  if (unit === 'bytes-gb') {
    const gb = value / (1024 ** 3)
    return `${gb >= 100 ? gb.toFixed(0) : gb.toFixed(1)} GB`
  }
  return new Intl.NumberFormat().format(value)
}

/**
 * Hub leaf: Site settings → General → Instance statistics (legacy
 * /admin/instance-stats, Mantine-native summary).
 *
 * Uses the same admin JSON API the classic charts page uses
 * (GET /admin/instance-stats/api/series?metric&window) and links to the
 * full charts + alerting page — nothing is lost by landing here.
 */
export default function InstanceStatsSection() {
  const [windowKey, setWindowKey] = useState<WindowKey>('month')
  const [values, setValues] = useState<Record<string, number | null>>({})
  const [loaded, setLoaded] = useState(false)
  const [loading, setLoading] = useState(false)

  const load = useCallback(async (w: WindowKey) => {
    setLoading(true)
    const next: Record<string, number | null> = {}
    await Promise.all(
      METRICS.map(async def => {
        try {
          const res = await fetchSeries(def.metric, w)
          const pts = res.points || []
          const last = pts[pts.length - 1]
          next[def.metric] = last && Array.isArray((last as { values: number[] }).values) ? (last as { values: number[] }).values[0] : null
        } catch {
          next[def.metric] = null
        }
      }),
    )
    setValues(next)
    setLoaded(true)
    setLoading(false)
  }, [])

  useEffect(() => {
    void load(windowKey)
  }, [load, windowKey])

  const cards = useMemo(
    () =>
      METRICS.map(def => ({
        ...def,
        value: values[def.metric] ?? null,
      })),
    [values]
  )

  return (
    <Stack gap="md">
      <Group justify="space-between" wrap="wrap" gap="sm" align="center">
        <Group gap={10} wrap="nowrap">
          <Icon name="monitoring" size={22} style={{ color: 'var(--mantine-color-ollitex-6)' }} />
          <div>
            <Text fw={700} size="md">
              Instance statistics
            </Text>
            <Text size="sm" c="dimmed" mt={4}>
              Latest values over the selected window, from the instance-stats
              collector.
            </Text>
          </div>
        </Group>
        <Group gap="xs">
          <NativeSelect
            value={windowKey}
            onChange={e => setWindowKey(e.currentTarget.value as WindowKey)}
            data={WINDOW_OPTIONS.map(o => ({ value: o.value, label: o.label }))}
            size="sm"
            style={{ width: 170 }}
          />
          <Anchor href="/admin/instance-stats" size="sm" target="_blank" style={{ textDecoration: 'none' }}>
            <Group gap={6}>
              <Icon name="timeline" size={16} />
              Full charts & alerts
            </Group>
          </Anchor>
        </Group>
      </Group>

      {loading && !loaded ? <Text size="sm" c="dimmed">Loading instance statistics…</Text> : null}

      <SimpleGrid cols={{ base: 2, sm: 3, xl: 4 }} spacing="md">
        {cards.map(def => (
          <Card key={def.metric} withBorder paddings="sm" radius="lg" miw={160}>
            <Text size="xs" fw={700} tt="uppercase" c="dimmed" style={{ letterSpacing: '0.06em' }}>
              {def.label}
            </Text>
            <Text size="xl" fw={700} mt={4}>
              {def.value == null ? '—' : fmt(def.value, def.unit)}
            </Text>
          </Card>
        ))}
      </SimpleGrid>
      {loaded ? (
        <Text size="xs" c="dimmed">
          Values refresh when the window changes. Historical time series,
          threshold alerts and email notifications live on the full instance
          statistics page.
        </Text>
      ) : null}
    </Stack>
  )
}
