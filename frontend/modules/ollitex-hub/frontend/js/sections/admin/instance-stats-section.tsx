import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Badge,
  Button,
  Card,
  Group,
  NativeSelect,
  Paper,
  SimpleGrid,
  Stack,
  Text,
  TextInput,
} from '@mantine/core'
import { getJSON, putJSON, postJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import StatsChart from '../../shared/stats-chart'
import type { StatsSeriesPoint } from '../../shared/stats-chart'
import { fetchSeries } from '@modules/instance-stats/frontend/js/features/instance-stats/api'
import { WINDOW_OPTIONS, STAT_CONFIG } from '@modules/instance-stats/frontend/js/features/instance-stats/config'
import type { WindowKey } from '@modules/instance-stats/frontend/js/features/instance-stats/types'
import { notify, errorMessage } from '../../shared/notify'

interface AlertCfg {
  alertEmails: string[]
  alertEmail: string
  diskWarningPercent: number
  ramWarningPercent: number
}

const TAB_META: Record<string, { title: string; blurb: string }> = {
  user: {
    title: 'User',
    blurb: 'Active users, sign-ups and the total user count over time.',
  },
  project: {
    title: 'Projects',
    blurb: 'Active projects, total projects, files and external share tokens.',
  },
  storage: {
    title: 'Storage',
    blurb: 'MongoDB, project (Overleaf) and Redis memory/disk footprints.',
  },
  system: {
    title: 'System',
    blurb: 'Host-level signals: /var/lib/overleaf disk, RAM and CPU load.',
  },
}

/**
 * Hub leaf: Site settings → General → Instance statistics.
 *
 * overleaf-lab #7 (2026-09-08, wave two / owner request): full parity with
 * the classic /admin/instance-stats — the User / Projects / Storage / System
 * sub-sections, the time-series BAR/LINE charts (inline SVG — the hub does
 * not bundle Plotly or any link to the classic page, which is slated for
 * removal), the window switch (day … year, all), plus the alert
 * configuration and send-test that previously only lived there.
 */
export default function InstanceStatsSection() {
  const [windowKey, setWindowKey] = useState<WindowKey>('month')
  const [series, setSeries] = useState<Record<string, StatsSeriesPoint[]>>({})
  const [loaded, setLoaded] = useState(false)
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)

  // --- alert configuration (GET /alert-config round-trip) -----------------
  const [cfg, setCfg] = useState<AlertCfg | null>(null)
  const [emails, setEmails] = useState<string[]>([])
  const [newEmail, setNewEmail] = useState('')
  const [diskPct, setDiskPct] = useState('90')
  const [ramPct, setRamPct] = useState('90')
  const [savingCfg, setSavingCfg] = useState(false)
  const [sendingTest, setSendingTest] = useState(false)
  const [cfgError, setCfgError] = useState<string | null>(null)

  const load = useCallback(async (w: WindowKey) => {
    setLoading(true)
    setLoadError(null)
    const next: Record<string, StatsSeriesPoint[]> = {}
    let failed = 0
    await Promise.all(
      STAT_CONFIG.map(async def => {
        try {
          const res = await fetchSeries(def.metric, w)
          next[def.metric] = (res.points || []).map(p => ({
            day: Number(p.day),
            values: Array.isArray(p.values) ? p.values : [],
          }))
        } catch {
          failed += 1
          next[def.metric] = []
        }
      }),
    )
    if (failed === STAT_CONFIG.length) {
      setLoadError('The instance-stats API could not be reached from this browser session.')
    }
    setSeries(next)
    setLoaded(true)
    setLoading(false)
  }, [])

  useEffect(() => {
    void load(windowKey)
  }, [load, windowKey])

  useEffect(() => {
    let alive = true
    getJSON<AlertCfg>('/admin/instance-stats/api/alert-config')
      .then(c => {
        if (!alive) return
        setCfg(c)
        setEmails(Array.isArray(c.alertEmails) ? c.alertEmails : c.alertEmail ? [c.alertEmail] : [])
        setDiskPct(String(c.diskWarningPercent ?? 90))
        setRamPct(String(c.ramWarningPercent ?? 90))
      })
      .catch(err => {
        if (alive) setCfgError(errorMessage(err, 'Failed to load alert configuration'))
      })
    return () => {
      alive = false
    }
  }, [])

  function addEmail() {
    const v = newEmail.trim()
    if (!v) return
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v)) {
      notify({ message: 'That does not look like an email address.', color: 'red' })
      return
    }
    if (!emails.includes(v)) setEmails([...emails, v])
    setNewEmail('')
  }

  async function saveCfg() {
    const disk = Number(diskPct)
    const ram = Number(ramPct)
    if (!emails.length) {
      notify({ message: 'Add at least one alert recipient before saving.', color: 'red' })
      return
    }
    if (!Number.isFinite(disk) || disk < 1 || disk > 100 || !Number.isFinite(ram) || ram < 1 || ram > 100) {
      notify({ message: 'Thresholds must be numbers between 1 and 100.', color: 'red' })
      return
    }
    setSavingCfg(true)
    try {
      await putJSON('/admin/instance-stats/api/alert-config', {
        body: { alertEmails: emails, diskWarningPercent: disk, ramWarningPercent: ram },
      })
      notify({ message: 'Alert configuration saved.', color: 'teal' })
    } catch (err) {
      notify({ message: errorMessage(err, 'Saving the alert configuration failed.'), color: 'red' })
    } finally {
      setSavingCfg(false)
    }
  }

  async function sendTest() {
    if (!emails.length) {
      notify({ message: 'Save at least one recipient first.', color: 'red' })
      return
    }
    setSendingTest(true)
    try {
      const res: any = await postJSON('/admin/instance-stats/api/send-test-alert-email', {
        body: { emails },
      })
      notify({
        message: `Test alert sent to ${((res && (res as any).sentTo) || emails).join(', ')}.`,
        color: 'teal',
      })
    } catch (err) {
      notify({ message: errorMessage(err, 'Sending the test alert failed.'), color: 'red' })
    } finally {
      setSendingTest(false)
    }
  }

  const tabs = useMemo(
    () =>
      ['user', 'project', 'storage', 'system']
        .map(tabId => ({
          tabId,
          meta: TAB_META[tabId] || { title: tabId, blurb: '' },
          stats: STAT_CONFIG.filter(c => c.tabId === tabId),
        }))
        .filter(t => t.stats.length > 0),
    // STAT_CONFIG / TAB_META are static imports — no reactive deps
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  )

  const allEmpty =
    loaded &&
    Object.values(series).every(pts => !pts || pts.length === 0 || pts.every(p => !p.values.length))

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
              Time series from the instance-stats collector, in the same layout as the classic page.
            </Text>
          </div>
        </Group>
        <NativeSelect
          aria-label="Time window"
          value={windowKey}
          onChange={e => setWindowKey(e.currentTarget.value as WindowKey)}
          data={WINDOW_OPTIONS.map(o => ({ value: o.value, label: o.label }))}
          size="sm"
          style={{ width: 170 }}
        />
      </Group>

      {loadError ? (
        <Alert color="yellow" variant="light" title={loadError} icon={<Icon name="error" size={20} />} />
      ) : null}

      {loading && !loaded ? <Text size="sm" c="dimmed">Loading instance statistics…</Text> : null}

      {allEmpty ? (
        <Alert
          icon={<Icon name="hourglass_empty" size={20} />}
          title="No data in this window yet"
          color="yellow"
          variant="light"
        >
          The collector samples every 24 h — a freshly provisioned instance (or a
          very short window like “Last day”) has no points yet. Try a wider window;
          the first day of collection fills in within a day.
        </Alert>
      ) : null}

      {/* ── sub-sections: User / Projects / Storage / System ─────────────── */}
      {tabs.map(tab => (
        <Card key={tab.tabId} withBorder radius="lg" p="md">
          <Group justify="space-between" wrap="wrap" gap="sm" mb="sm" align="flex-start">
            <div>
              <Text fw={700}>{tab.meta.title}</Text>
              <Text size="sm" c="dimmed" mt={2}>
                {tab.meta.blurb}
              </Text>
            </div>
          </Group>
          <SimpleGrid cols={{ base: 1, lg: 2 }} spacing="md">
            {tab.stats.map(def => {
              const pts = series[def.metric] || []
              const last = pts.length ? pts[pts.length - 1] : null
              const lastV = last && last.values.length ? def.transform(last.values[0]) : null
              return (
                <Paper key={def.id} withBorder radius="lg" p="sm" w="100%">
                  <Group justify="space-between" wrap="nowrap" gap="xs" mb={6}>
                    <Text size="sm" fw={700} truncate>
                      {def.title}
                    </Text>
                    <Text size="sm" c="dimmed" style={{ whiteSpace: 'nowrap' }}>
                      latest{' '}
                      {lastV == null
                        ? '—'
                        : `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(lastV)}${def.ylabel ? ` ${def.ylabel}` : ''}`}
                    </Text>
                  </Group>
                  <StatsChart
                    points={pts}
                    colors={[def.colors?.y1, def.colors?.y2]}
                    labels={def.labels}
                    transform={def.transform}
                    ylabel={def.ylabel}
                    chartType={def.chartType === 'bar' ? 'bar' : 'line'}
                    height={150}
                  />
                </Paper>
              )
            })}
          </SimpleGrid>
        </Card>
      ))}

      {/* ── Alert settings ────────────────────────────────────────────────── */}
      <Card withBorder paddings="md" radius="lg">
        <Group justify="space-between" wrap="wrap" gap="sm" align="flex-start">
          <div>
            <Text fw={700}>Alert settings</Text>
            <Text size="sm" c="dimmed" mt={4}>
              Recipients and disk/RAM warning thresholds for the instance-stats
              alert checker. The “Send test” button emails the configured
              recipients without touching any threshold logic.
            </Text>
          </div>
          <Group gap="xs">
            <Button
              variant="light"
              size="xs"
              loading={sendingTest}
              disabled={!emails.length}
              onClick={() => void sendTest()}
            >
              Send test
            </Button>
            <Button size="xs" loading={savingCfg} onClick={() => void saveCfg()}>
              Save
            </Button>
          </Group>
        </Group>

        {cfgError ? (
          <Alert color="red" variant="light" title={cfgError} mt="xs" />
        ) : null}

        <Stack gap="xs" mt="md">
          <Group gap="xs" align="flex-start" wrap="wrap">
            <TextInput
              aria-label="Alert email address"
              value={newEmail}
              onChange={e => setNewEmail(e.currentTarget.value)}
              placeholder="recipient@example.org"
              size="sm"
              style={{ maxWidth: 280 }}
              onKeyDown={e => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  addEmail()
                }
              }}
            />
            <Button variant="light" size="xs" onClick={addEmail}>
              Add recipient
            </Button>
          </Group>
          {emails.length ? (
            <Group gap={6} wrap="wrap">
              {emails.map(e => (
                <Badge key={e} variant="light" radius="sm" size="sm" rightSection={
                  <Button
                    variant="subtle"
                    size="xs"
                    color="dimmed"
                    style={{ padding: 0 }}
                    onClick={() => setEmails(emails.filter(x => x !== e))}
                  >
                    ×
                  </Button>
                }>
                  {e}
                </Badge>
              ))}
            </Group>
          ) : (
            <Text size="xs" c="dimmed">
              No recipients yet (the checker defaults to the admin email when none
              is configured).
            </Text>
          )}
          <Group gap="sm" wrap="wrap" mt="xs">
            <TextInput
              label="Disk warning threshold (%)"
              type="number"
              value={diskPct}
              onChange={e => setDiskPct(e.currentTarget.value)}
              min={1}
              max={100}
              size="sm"
              style={{ width: 220 }}
            />
            <TextInput
              label="RAM warning threshold (%)"
              type="number"
              value={ramPct}
              onChange={e => setRamPct(e.currentTarget.value)}
              min={1}
              max={100}
              size="sm"
              style={{ width: 220 }}
            />
            {cfg ? (
              <Text size="xs" c="dimmed" ta="right" mt={30}>
                Loaded from the instance (last saved: disk {cfg.diskWarningPercent}% ·
                RAM {cfg.ramWarningPercent}% · {cfg.alertEmails?.length ?? 0}
                recipient(s)).
              </Text>
            ) : null}
          </Group>
        </Stack>
      </Card>

      {loaded ? (
        <Text size="xs" c="dimmed">
          Values and charts refresh when the window changes. The collector appends
          one daily sample per metric.
        </Text>
      ) : null}
    </Stack>
  )
}
