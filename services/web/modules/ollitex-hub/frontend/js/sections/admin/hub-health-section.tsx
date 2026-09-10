import React, { useCallback, useEffect, useState } from 'react'
import {
  Badge,
  Button,
  Card,
  Group,
  Stack,
  Text,
  Title,
} from '@mantine/core'
import { getCollectedErrors, startErrorCollector } from '../../shared/error-collector'
import { notify } from '../../shared/notify'
import { PageLoading } from '../../shared/page-state'
import { useHubT } from '../../shared/hub-i18n'

interface ProbeSpec {
  label: string
  method: string
  path: string
  /** accepted statuses (deterministic contract, not "2xx-or-die") */
  expect: number[]
  json: boolean
}

/**
 * The endpoints the /hub actually consumes (admin surface). Probing them is
 * read-only + idempotent — the same manifest the e2e leaf-completeness and
 * endpoint-contract tests use (hub-health, 2026-09-08).
 */
const PROBES: ProbeSpec[] = [
  { label: 'User settings page (HTML)', method: 'GET', path: '/user/settings', expect: [200], json: false },
  { label: 'Site settings (JSON)', method: 'GET', path: '/admin/site-settings', expect: [200], json: true },
  { label: 'Template admins', method: 'GET', path: '/admin/site/template-admins', expect: [200], json: true },
  { label: 'LLM admin settings', method: 'GET', path: '/admin/llm/settings/json', expect: [200], json: true },
  { label: 'LLM usage', method: 'GET', path: '/admin/llm/usage', expect: [200], json: true },
  {
    label: 'Instance stats series',
    method: 'GET',
    path: '/admin/instance-stats/api/series?metric=user_count&window=month',
    expect: [200],
    json: true,
  },
  { label: 'Instance stats alert config', method: 'GET', path: '/admin/instance-stats/api/alert-config', expect: [200], json: true },
  { label: 'Active projects', method: 'GET', path: '/admin/active-projects', expect: [200], json: true },
  { label: 'System messages', method: 'GET', path: '/system/messages', expect: [200], json: true },
  { label: 'Template categories', method: 'GET', path: '/api/template/categories', expect: [200], json: true },
  { label: 'Hub page shell', method: 'GET', path: '/hub', expect: [200], json: false },
  { label: 'Hub core health (this endpoint)', method: 'GET', path: '/api/hub/health', expect: [200], json: true },
]

interface ProbeResult {
  spec: ProbeSpec
  status: number | 'error'
  ms: number
  contentType: string
  ok: boolean
  note?: string
}

interface CoreHealth {
  ok?: boolean
  uptimeSec?: number
  platform?: { node?: string; arch?: string; pid?: number; os?: string }
  instance?: { appName?: string; env?: string; siteUrl?: string }
  mongo?: { readyState?: number | null; state?: string; pingMs?: number | null; error?: string }
  featureGates?: Record<string, unknown>
  error?: string
}

function fmtUptime(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) return '—'
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d}d ${h}h ${m}m`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

function passFail(ok: boolean, status: number | 'error'): React.ReactNode {
  if (ok) {
    return (
      <Badge variant="light" color="teal" radius="sm" size="sm">
        PASS
      </Badge>
    )
  }
  return (
    <Badge variant="light" color="red" radius="sm" size="sm">
      FAIL {status}
    </Badge>
  )
}

function truthy(v: unknown): boolean {
  return v === true
}

export default function HubHealthSection() {
  // overleaf-lab #11: i18n pattern (opt-in keys + safe English default) —
  // see ROADMAP.md for the extraction backlog
  const tt = useHubT()
  const [results, setResults] = useState<ProbeResult[] | null>(null)
  const [core, setCore] = useState<CoreHealth | null>(null)
  const [coreError, setCoreError] = useState<string | null>(null)
  const [errors, setErrors] = useState<ReturnType<typeof getCollectedErrors>>([])
  const [running, setRunning] = useState(true)

  const run = useCallback(async () => {
    setRunning(true)
    setResults(null)
    setCore(null)
    setCoreError(null)
    const out: ProbeResult[] = []
    // run probes concurrently (they are independent reads)
    await Promise.all(
      PROBES.map(async spec => {
        const t0 = Date.now()
        let status: number | 'error' = 'error'
        let ct = ''
        let ok = false
        let note: string | undefined
        try {
          const res = await fetch(spec.path, {
            headers: { Accept: spec.json ? 'application/json' : 'text/html' },
            credentials: 'same-origin',
          })
          status = res.status
          ct = (res.headers.get('content-type') || '').toLowerCase()
          ok = spec.expect.includes(res.status) && (!spec.json || ct.includes('json'))
          if (!ok && spec.expect.includes(res.status) && spec.json && !ct.includes('json')) {
            note = `status ${res.status} ok, but content-type is "${ct || 'empty'}"`
          }
          res.body?.cancel?.()
        } catch (err) {
          note = String((err as any)?.message || err)
        }
        out.push({ spec, status, ms: Date.now() - t0, contentType: ct, ok, note })
      })
    )
    out.sort((a, b) => PROBES.indexOf(a.spec) - PROBES.indexOf(b.spec))
    setResults(out)

    let c: CoreHealth | null = null
    try {
      const res = await fetch('/api/hub/health', { headers: { Accept: 'application/json' } })
      if (res.ok) c = (await res.json()) as CoreHealth
      else {
        const body = await res.text().catch(() => '')
        setCoreError(res.status === 403 ? 'Forbidden (admin required)' : `HTTP ${res.status}: ${body.slice(0, 160)}`)
      }
    } catch (err: any) {
      setCoreError(err?.message || String(err))
    }
    setCore(c)
    setErrors(getCollectedErrors())
    setRunning(false)
  }, [])

  useEffect(() => {
    startErrorCollector()
    void run()
  }, [run])

  async function copyReport() {
    try {
      const lines = [
        `OlliTeX hub health report — ${new Date().toISOString()}`,
        core ? `server: uptime ${core.uptimeSec ?? '—'}s, node ${core.platform?.node}, mongo ${core.mongo?.state ?? '—'}` : 'server core: unavailable',
        '',
        'probes:',
        ...(results || []).map(r => `${r.ok ? 'PASS' : 'FAIL'} ${r.spec.method} ${r.spec.path} -> ${r.status} ${r.ms}ms ${r.note || ''}`),
        '',
        `captured client errors: ${errors.length}`,
        ...errors.map(e => `  [${e.kind}] ${new Date(e.at).toISOString()} ${e.text}`),
      ]
      const text = lines.join('\n')
      try {
        await navigator.clipboard.writeText(text)
        notify({ message: 'Health report copied to the clipboard.', color: 'teal' })
      } catch {
        // clipboard API may be blocked; fall back to a transient toast with the
        // counts (full text is in the page source / DOM table)
        notify({
          message:
            `Report ready (${(results || []).length} probes, ${errors.length} captured errors) — clipboard blocked, see the table above.`,
          color: 'yellow',
        })
      }
    } catch (err: any) {
      notify({ message: `Copy failed: ${err?.message || err}`, color: 'red' })
    }
  }

  const failing = (results || []).filter(r => !r.ok).length
  const passing = (results || []).filter(r => r.ok).length

  if (running) return <PageLoading label={tt('hub.health.loading', 'Probing the hub surface…')} />

  return (
    <Stack gap="md" w="100%">
      <div>
        <Title order={2} fw={800}>
          {tt('hub.health.title', 'Hub health')}
        </Title>
        <Text size="sm" c="dimmed" mt={4}>
          {tt(
            'hub.health.subtitle',
            'Live diagnostics for the unified /hub: server core, the admin endpoints the sections consume, and client-side state. Every probe is a read-only, idempotent GET.',
          )}
        </Text>
      </div>

      <Group gap="xs" wrap="wrap">
        <Button variant="light" size="xs" leftSection={<span className="material-symbols" style={{ fontSize: 15 }}>refresh</span>} onClick={() => void run()}>
          {tt('hub.health.rerun', 'Re-run probes')}
        </Button>
        <Button variant="light" size="xs" leftSection={<span className="material-symbols" style={{ fontSize: 15 }}>content_copy</span>} onClick={() => void copyReport()}>
          {tt('hub.health.copy', 'Copy report')}
        </Button>
        {results ? (
          <Badge variant="light" color={failing ? 'red' : 'teal'} radius="sm" size="sm">
            {passing} pass / {failing} fail
          </Badge>
        ) : null}
      </Group>

      {/* server core */}
      <Card withBorder paddings="md" radius="lg">
        <Group justify="space-between" wrap="wrap" gap="sm" align="flex-start">
          <div>
            <Text fw={700}>{tt('hub.health.serverCore', 'Server core')}</Text>
            {core ? (
              <Stack gap="xs" mt={6} w="100%">
                <Group gap="sm" wrap="wrap">
                  <Text size="sm">
                    uptime&nbsp;<b>{fmtUptime(core.uptimeSec ?? 0)}</b>
                  </Text>
                  <Text size="sm">
                    node&nbsp;<b>{core.platform?.node}</b>&nbsp;({core.platform?.arch})
                  </Text>
                  <Text size="sm">
                    mongo&nbsp;<b>{core.mongo?.state ?? '—'}</b>
                    {core.mongo?.pingMs != null ? ` · ping ${core.mongo.pingMs}ms` : ''}
                  </Text>
                  <Text size="sm">
                    app&nbsp;<b>{core.instance?.appName ?? 'OlliTeX'}</b> ({core.instance?.env ?? '?'})
                  </Text>
                </Group>
                {core.featureGates ? (
                  <div style={{ marginTop: 6 }}>
                    <Text size="xs" c="dimmed" mb={4}>
                      Feature gates (why sections/widgets may be hidden)
                    </Text>
                    <Group gap="xs" wrap="wrap">
                      {Object.entries(core.featureGates).map(([k, v]) => (
                        <Badge
                          key={k}
                          variant="light"
                          color={truthy(v) ? 'teal' : 'gray'}
                          radius="sm"
                          size="sm"
                          style={{ opacity: truthy(v) ? 1 : 0.75 }}
                        >
                          {k}
                        </Badge>
                      ))}
                    </Group>
                  </div>
                ) : null}
              </Stack>
            ) : null}
          </div>
          {!core && coreError ? (
            <Badge variant="light" color="red" radius="sm" size="sm">
              unavailable
            </Badge>
          ) : null}
        </Group>
        {coreError ? (
          <Text size="xs" c="dimmed" mt={8} style={{ wordBreak: 'break-word' }}>
            {coreError}
          </Text>
        ) : null}
      </Card>

      {/* endpoint probes */}
      <Card withBorder paddings="md" radius="lg">
        <Text fw={700} mb={8}>
          {tt('hub.health.endpoints', 'Endpoint probes')}
        </Text>
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ textAlign: 'left', color: 'var(--mantine-color-dimmed)' }}>
                <th style={{ padding: '4px 8px 4px 0', fontWeight: 600 }}>Probe</th>
                <th style={{ padding: '4px 8px', fontWeight: 600 }}>Request</th>
                <th style={{ padding: '4px 8px', fontWeight: 600 }}>Status</th>
                <th style={{ padding: '4px 8px', fontWeight: 600 }}>ms</th>
                <th style={{ padding: '4px 0 4px 8px', fontWeight: 600 }}>Result</th>
              </tr>
            </thead>
            <tbody>
              {(results || []).map(r => (
                <tr key={r.spec.path} style={{ borderTop: '1px solid var(--mantine-color-border-light)' }}>
                  <td style={{ padding: '6px 8px 6px 0' }}>{r.spec.label}</td>
                  <td style={{ padding: '6px 8px', fontFamily: 'ui-monospace, monospace', fontSize: 12, whiteSpace: 'nowrap' }}>
                    {r.spec.method} {r.spec.path}
                  </td>
                  <td style={{ padding: '6px 8px', whiteSpace: 'nowrap' }}>{r.status}</td>
                  <td style={{ padding: '6px 8px', whiteSpace: 'nowrap' }}>{r.ms}</td>
                  <td style={{ padding: '6px 0 6px 8px' }}>
                    {passFail(r.ok, r.status)}
                    {r.note ? (
                      <div style={{ color: 'var(--mantine-color-dimmed)', fontSize: 11, marginTop: 2, maxWidth: 340 }}>
                        {r.note}
                      </div>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>

      {/* client state */}
      <Card withBorder paddings="md" radius="lg">
        <Text fw={700} mb={8}>
          {tt('hub.health.clientState', 'Client state')}
        </Text>
        <Group gap="sm" wrap="wrap">
          <Text size="sm">
            captured client errors:&nbsp;<b>{errors.length}</b>
          </Text>
          <Text size="sm" c="dimmed">
            (uncaught window errors + unhandled promise rejections since page load — the tail of
            the ring buffer shown below)
          </Text>
        </Group>
        {errors.length ? (
          <Stack gap={4} mt="xs" style={{ maxHeight: 220, overflowY: 'auto' }}>
            {errors.map((e, i) => (
              <div key={i} style={{ fontSize: 12, fontFamily: 'ui-monospace, monospace', color: 'var(--mantine-color-dimmed)' }}>
                <Badge variant="light" color="yellow" radius="sm" size="xs" mr={6}>
                  {e.kind}
                </Badge>
                {new Date(e.at).toLocaleTimeString()} — {e.text}
              </div>
            ))}
          </Stack>
        ) : (
          <Text size="sm" c="teal" mt="xs" fw={600}>
            {tt('hub.health.noErrors', 'No client errors captured since page load.')}
          </Text>
        )}
      </Card>

      <Text size="xs" c="dimmed">
        overleaf-lab #14 (2026-09-08) — the “Hub health” diagnostics leaf. Backend: GET
        /api/hub/health (admin). The probe list above mirrors the e2e endpoint-contract manifest.
      </Text>
    </Stack>
  )
}
