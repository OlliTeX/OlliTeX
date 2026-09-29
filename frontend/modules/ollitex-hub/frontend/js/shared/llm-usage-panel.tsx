import React, { useEffect, useState } from 'react'
import { Group, Paper, SimpleGrid, Stack, Text } from '@mantine/core'
import { getJSON } from '@/infrastructure/fetch-json'

/**
 * llm-usage-panel (2026-09-08, owner request): the LLM usage view for BOTH
 * hub leaves — `mysettings.llm.usage` (user scope, GET /user/llm-usage) and
 * `site.llm.usage` (admin scope, GET /admin/llm/usage).
 *
 * The shared LLMUsageMeter renders with the classic page's `llm-ui.scss`
 * classes, which the hub bundle does not ship — the bars were invisible and
 * the layout read as a wall of unstyled text. This panel renders the SAME
 * data with hub-native, theme-aware Mantine primitives:
 *   - headline numbers (requests / input / output / total tokens)
 *   - the daily token bar graph (CSS bars, hub brand colors)
 *   - by-feature and by-model breakdowns
 */
type DayPoint = { day: string; calls: number; totalTokens: number }
type Breakdown = { action?: string; model?: string; calls: number; totalTokens: number }
type Summary = {
  days: number
  calls: number
  inputTokens: number
  outputTokens: number
  totalTokens: number
  byDay: DayPoint[]
  byAction: Breakdown[]
  byModel: Breakdown[]
}

function fmtTokens(n: number | null | undefined) {
  const v = Math.max(0, Number(n) || 0)
  if (v >= 1000000) return `${(v / 1000000).toFixed(1)}M`
  if (v >= 1000) return `${(v / 1000).toFixed(1)}k`
  return String(v)
}

export default function LlmUsagePanel({ scope, days = 30 }: { scope: 'admin' | 'user'; days?: number }) {
  const [state, setState] = useState<'loading' | 'ready' | 'unavailable'>('loading')
  const [data, setData] = useState<Summary | null>(null)
  const [unavailableMsg, setUnavailableMsg] = useState('')

  useEffect(() => {
    let alive = true
    const url = scope === 'admin' ? `/admin/llm/usage?days=${days}` : `/user/llm-usage?days=${days}`
    getJSON(url)
      .then(res => {
        if (!alive) return
        if (res && res.ok && Array.isArray(res.byDay)) {
          setData(res as unknown as Summary)
          setState('ready')
        } else {
          setUnavailableMsg((res && (res.message || res.error)) || '')
          setState('unavailable')
        }
      })
      .catch(err => {
        if (!alive) return
        setUnavailableMsg(typeof err === 'string' ? err : (err?.message as string) || '')
        setState('unavailable')
      })
    return () => {
      alive = false
    }
  }, [scope, days])

  if (state === 'loading') {
    return <Text size="sm" c="dimmed" style={{ padding: '10px 2px' }}>Loading usage statistics…</Text>
  }

  if (state === 'unavailable' || !data) {
    return (
      <Text size="sm" c="dimmed" style={{ padding: '10px 2px' }}>
        Usage statistics are not available right now.
        {unavailableMsg ? ` (${unavailableMsg})` : ''}
      </Text>
    )
  }

  const byDay = data.byDay || []
  const maxDay = Math.max(1, ...byDay.map(d => d.totalTokens))
  const labelEvery = Math.max(1, Math.ceil(byDay.length / 6))

  const stats: { value: string | number; label: string }[] = [
    { value: data.calls, label: 'Requests' },
    { value: fmtTokens(data.inputTokens), label: 'Input tokens' },
    { value: fmtTokens(data.outputTokens), label: 'Output tokens' },
    { value: fmtTokens(data.totalTokens), label: 'Total tokens' },
  ]

  return (
    <Stack gap="md">
      {/* headline numbers */}
      <SimpleGrid cols={{ base: 2, sm: 4 }} spacing="sm">
        {stats.map(s => (
          <Paper key={s.label} withBorder radius="lg" p="sm" w="100%">
            <Text size="xl" fw={700}>
              {s.value}
            </Text>
            <Text size="xs" c="dimmed" mt={2}>
              {s.label}
            </Text>
          </Paper>
        ))}
      </SimpleGrid>

      {/* the daily token bar graph (owner: "put the bar graph back") */}
      {byDay.length > 0 ? (
        <Paper withBorder radius="lg" p="md" w="100%">
          <Group justify="space-between" wrap="nowrap" mb="xs">
            <Text size="sm" fw={700}>
              Daily token usage
            </Text>
            <Text size="xs" c="dimmed">
              last {data.days || days} days · newest day rightmost
            </Text>
          </Group>
          <div
            role="img"
            aria-label={`Bar graph of daily LLM token usage over the last ${byDay.length} days`}
            style={{
              display: 'flex',
              alignItems: 'flex-end',
              gap: 3,
              height: 96,
              padding: '4px 2px 2px',
              borderBottom: '1px solid var(--mantine-color-border, var(--mantine-color-default-4))',
            }}
          >
            {byDay.map(d => {
              const h = Math.max(d.totalTokens ? 6 : 0, Math.round((d.totalTokens / maxDay) * 100))
              return (
                <div
                  key={d.day}
                  title={`${d.day}: ${d.calls} request(s), ${d.totalTokens.toLocaleString()} tokens`}
                  style={{
                    flex: '1 1 0',
                    minWidth: 2,
                    maxWidth: 16,
                    height: `${h}%`,
                    borderRadius: '2px 2px 0 0',
                    background: d.totalTokens
                      ? 'var(--mantine-color-ollitex-5)'
                      : 'color-mix(in srgb, var(--mantine-color-dimmed) 30%, transparent)',
                    opacity: d.totalTokens ? 0.9 : 1,
                  }}
                />
              )
            })}
          </div>
          <div style={{ display: 'flex', justifyContent: 'space-between', padding: '2px 2px 0' }}>
            {byDay
              .map((d, i) => (i % labelEvery === 0 || i === byDay.length - 1 ? d.day : null))
              .filter(Boolean)
              .map(d => (
                <Text key={d as string} size="xs" c="dimmed" style={{ whiteSpace: 'nowrap' }}>
                  {d}
                </Text>
              ))}
          </div>
        </Paper>
      ) : (
        <Text size="sm" c="dimmed" style={{ padding: '4px 2px' }}>
          No LLM activity in the last {days} days.
        </Text>
      )}

      {/* breakdowns */}
      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="md">
        <Paper withBorder radius="lg" p="md" w="100%">
          <Text size="sm" fw={700} mb="xs">
            By feature
          </Text>
          {(data.byAction || []).length ? (
            (data.byAction || []).slice(0, 6).map(a => (
              <Group key={a.action || 'row'} justify="space-between" gap="sm" wrap="nowrap" style={{ padding: '3px 0' }}>
                <Text size="sm" truncate>
                  {a.action || '—'}
                </Text>
                <Text size="sm" c="dimmed" style={{ whiteSpace: 'nowrap' }}>
                  {a.calls} · {fmtTokens(a.totalTokens)} tokens
                </Text>
              </Group>
            ))
          ) : (
            <Text size="sm" c="dimmed">
              No per-feature usage recorded.
            </Text>
          )}
        </Paper>

        <Paper withBorder radius="lg" p="md" w="100%">
          <Text size="sm" fw={700} mb="xs">
            By model
          </Text>
          {(data.byModel || []).length ? (
            (data.byModel || []).slice(0, 6).map(m => (
              <Group key={m.model || 'row'} justify="space-between" gap="sm" wrap="nowrap" style={{ padding: '3px 0' }}>
                <Text size="sm" truncate style={{ fontFamily: 'var(--mantine-font-family-monospace, monospace)' }}>
                  {m.model || '—'}
                </Text>
                <Text size="sm" c="dimmed" style={{ whiteSpace: 'nowrap' }}>
                  {m.calls} · {fmtTokens(m.totalTokens)} tokens
                </Text>
              </Group>
            ))
          ) : (
            <Text size="sm" c="dimmed">
              No per-model usage recorded.
            </Text>
          )}
        </Paper>
      </SimpleGrid>
    </Stack>
  )
}
