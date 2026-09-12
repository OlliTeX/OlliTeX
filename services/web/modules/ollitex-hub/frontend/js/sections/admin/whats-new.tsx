import React, { useEffect, useState } from 'react'
import { Card, Group, Stack, Text } from '@mantine/core'

/**
 * 2026-09-16 (owner task T10/#8, P1): "What's new" card on the hub
 * Overview. Renders docs/RELEASE_NOTES.md (served at /api/hub/notes) with a
 * deliberately tiny markdown subset: "## "/"### " section titles, "- "
 * bullets, **bold** spans. No markdown dependency, no HTML injection
 * (text-only).
 */

function boldParts(line: string): React.ReactNode[] {
  const out: React.ReactNode[] = []
  let rest = line
  let k = 0
  const re = /\*\*(.+?)\*\*/g
  let m: RegExpExecArray | null
  while ((m = re.exec(rest)) !== null) {
    if (m.index > 0) out.push(rest.slice(0, m.index))
    out.push(<b key={`b${k++}`}>{m[1]}</b>)
    rest = rest.slice(m.index + m[0].length)
  }
  if (rest) out.push(rest)
  return out
}

interface Sec {
  title: string
  bullets: string[]
}

function parse(md: string): Sec[] {
  const secs: Sec[] = []
  let cur: Sec | null = null
  for (const raw of md.split(/\r?\n/)) {
    const line = raw.trimEnd()
    if (!line.trim()) continue
    if (line.startsWith('# ')) continue // doc title
    if (line.startsWith('> ')) continue // maintainer note
    if (line.startsWith('## ') || line.startsWith('### ')) {
      cur = { title: line.replace(/^#{2,3}\s+/, ''), bullets: [] }
      secs.push(cur)
      continue
    }
    const b = line.match(/^\s*[-*]\s+(.*)$/)
    if (b && cur) {
      cur.bullets.push(b[1].trim())
      continue
    }
    if (cur && line && !line.startsWith('#')) cur.bullets.push(line)
  }
  return secs
}

export default function WhatSNew() {
  const [secs, setSecs] = useState<Sec[] | null>(null)

  useEffect(() => {
    let cancelled = false
    fetch('/api/hub/notes', { headers: { Accept: 'text/markdown' } })
      .then(r => (r.ok ? r.text() : null))
      .then(md => {
        if (cancelled || !md) return
        const parsed = parse(md).filter(s => s.bullets.length > 0 || true)
        setSecs(parsed)
      })
      .catch(() => {
        /* card simply stays hidden */
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (!secs || !secs.length) return null

  // First heading is the release banner (e.g. "2026-09 — OlliTeX 1.0");
  // the subsections carry the bullets.
  const banner = secs[0].bullets.length === 0 ? secs[0] : null
  const body = (banner ? secs.slice(1) : secs).slice(0, 4)

  return (
    <Card withBorder radius="lg" pb="sm">
      <Group align="center" gap="sm" justify="space-between" pb="xs">
        <div>
          <Text fw={700} size="md" lh={1.2}>
            What’s new
          </Text>
          {banner && (
            <Text size="xs" c="dimmed" mt={2}>
              {boldParts(banner.title)}
            </Text>
          )}
        </div>
      </Group>
      <Stack gap="xs">
        {body.map(s => (
          <Group key={s.title} align="start" gap="xs" wrap="wrap">
            <Text
              fw={600}
              size="sm"
              style={{ whiteSpace: 'nowrap' }}
              flexShrink={0}
              w={150}
            >
              {s.title}
            </Text>
            <Text size="sm" c="dimmed" style={{ flex: 1, minWidth: 200 }}>
              {boldParts(s.bullets.slice(0, 3).join('  ·  '))}
            </Text>
          </Group>
        ))}
      </Stack>
    </Card>
  )
}
