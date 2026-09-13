// /hub → Site settings → Compilation: Typst (owner 2026-09-11).
// Native Mantine section mirroring the "Git integration" / "Sandboxed
// compiles" admin areas — the admin can toggle Typst compiles and point the
// dedicated clsi_typst service URL. Persisted as the `typst` site-settings
// section (SiteSettingsManager) and hydrated to COMPILE_TYPEST_ENABLED /
// CLSI_TYPEST_URL via the EnvHydrator (applies on the next container cycle).

import React from 'react'
import { Anchor, Group, Text } from '@mantine/core'
import {
  Field,
  PageLoading,
  SectionShell,
  bool0,
  str0,
  useSyncValues,
  useSiteSettings,
} from './site-core'

export function TypstSection() {
  const { data, error, flash, save, load } = useSiteSettings('typst')
  const { v, up } = useSyncValues(data, d => ({
    // Default ON — matches the product default (COMPILE_TYPEST_ENABLED !== 'false').
    enabled: bool0((d as any).enabled, true),
    url: str0((d as any).url),
  }))
  if (!data && !error) return <PageLoading label="Loading Typst settings…" />
  if (error && !data) {
    return (
      <Group>
        <Text size="sm" c="red">Typst: {error}</Text>
        <Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor>
      </Group>
    )
  }
  return (
    <SectionShell
      title="Typst compiles"
      badge="typst"
      enabled={Boolean(v.enabled)}
      onEnabled={x => up({ enabled: x })}
      description="Typst compilation for .typ projects (dedicated clsi_typst service). On by default (owner 2026-09-10) — the toggle is the rollback path."
      footerNote="Changes apply on the next container cycle."
      flash={flash}
      onSave={() => void save({
        enabled: Boolean(v.enabled),
        url: String(v.url || ''),
      })}
    >
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field
          label="clsi_typst service URL"
          value={String(v.url || '')}
          onChange={x => up({ url: x })}
          placeholder="http://clsi_typst:3014"
          hint="Endpoint that Typst compiles are sent to. Leave empty for the build default."
          width="100%"
        />
      </Group>
    </SectionShell>
  )
}
