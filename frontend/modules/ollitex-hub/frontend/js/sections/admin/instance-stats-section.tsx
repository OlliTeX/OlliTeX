import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Anchor,
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

  // --- Grafana live pane (D22 kiosk opt-in — off unless the instance
  //     configured HUB_GRAFANA_EMBED_URL / monitoring.grafanaEmbedURL) ------
  const [grafana, setGrafana] = useState<{
    enabled: boolean
    dashboards: { id: string; title: string; kiosk: string; full: string }[]
  } | null>(null)
  const [grafanaSel, setGrafanaSel] = useState('ollitex-instance-stats')
  // AG (owner 2026-10-09): the pane's first listed dashboard
  // (ollitex-instance-stats) wins over the stale default.
  useEffect(() => {
    if (grafana && Array.isArray(grafana.dashboards) && grafana.dashboards.length) {
      if (!grafana.dashboards.some(d => d.id === grafanaSel)) {
        setGrafanaSel(grafana.dashboards[0].id)
      }
    }
  }, [grafana, grafanaSel])
  useEffect(() => {
    let alive = true
    getJSON<{ enabled: boolean; dashboards: { id: string; title: string; kiosk: string; full: string }[] }>(
      '/admin/instance-stats/api/grafana',
    )
      .then(g => {
        if (alive) setGrafana(g)
      })
      .catch(() => {
        if (alive) setGrafana(null) // non-admin section callers are the only audience; hidden = off
      })
    return () => {
      alive = false
    }
  }, [])
	const grafanaDash = Array.isArray(grafana?.dashboards)
		? grafana.dashboards.find(d => d.id === grafanaSel) || grafana.dashboards[0]
		: null

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
      {grafana && grafana.enabled && grafanaDash ? (
        <Card withBorder radius="lg" p="md">
          <Group justify="space-between" wrap="wrap" gap="sm" align="center" mb="sm">
            <div>
              <Text fw={700} size="md">
                Live dashboards
              </Text>
              <Text size="sm" c="dimmed" mt={2}>
                Grafana dashboards on this instance — served through the admin-gated OlliTeX proxy
                (no Grafana credentials in the browser; anonymous access is disabled).
              </Text>
            </div>
            <Group gap="xs">
              <NativeSelect
                aria-label="Dashboard"
                value={grafanaDash.id}
                onChange={e => setGrafanaSel(e.currentTarget.value)}
                data={grafana.dashboards.map(d => ({ value: d.id, label: d.title }))}
                size="xs"
                style={{ width: 210 }}
              />
              <Anchor href={grafanaDash.full} target="_blank" rel="noreferrer noopener" size="sm">
                Open in Grafana ↗
              </Anchor>
            </Group>
          </Group>
          <iframe
            title={grafanaDash.title}
            src={grafanaDash.kiosk}
            loading="lazy"
            sandbox="allow-scripts allow-same-origin"
            style={{ width: '100%', height: 520, border: '1px solid var(--mantine-color-gray-3)', borderRadius: 8 }}
          />
        </Card>
      ) : null}
      {/* ── AG (owner 2026-10-09): alert recipients — the rules fire from the
          monitoring stack and mail these addresses; threshold inputs are retired
          (managed with the monitoring rules). ── */}
            {/* ── Alert settings ────────────────────────────────────────────────── */}
      <Card withBorder paddings="md" radius="lg">
        <Group justify="space-between" wrap="wrap" gap="sm" align="flex-start">
          <div>
            <Text fw={700}>Alert settings</Text>
            <Text size="sm" c="dimmed" mt={4}>
              Recipients for the instance-stats alerts. The alerts fire from the
              monitoring stack (disk / RAM rules at the 90% defaults, migrated off the
              native admin page) and are mailed to these addresses. “Send test” emails the
              list without firing a rule; thresholds are managed with the monitoring
              stack, not here.
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
                    {cfg ? (
            <Text size="xs" c="dimmed" mt="xs">
              Loaded from the instance ({cfg.alertEmails?.length ?? 0} recipient(s)).
            </Text>
          ) : null}
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
