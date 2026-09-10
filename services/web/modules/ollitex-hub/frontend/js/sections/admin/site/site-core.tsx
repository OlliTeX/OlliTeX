// /hub → Site settings — native Mantine sections (owner #32): shared core.
//
// Contract (legacy admin-tools, verified):
//   GET  /admin/site-settings            → { 'sso-saml': {...masked}, email: {...}, ... }
//   PUT  /admin/site-settings/:section   → body = section object (secrets kept
//        when sent empty; server stores them encrypted, GET masks them and
//        returns <secret>Set: true flags)
//   POST /admin/site-settings/email/test → { to }

import React, { useCallback, useEffect, useState } from 'react'
import {
  Badge,
  Button,
  Card,
  Group,
  Text,
  TextInput,
  Textarea,
} from '@mantine/core'
import { notify } from '../../../shared/notify'
import { putJSON } from '@/infrastructure/fetch-json'
import { getSiteSettings, invalidateSiteSettingsCache } from '../../../shared/settings-cache'
import { PageError, PageLoading } from '../../../shared/page-state'

export type Flash = { saving: boolean; saved: boolean; error: string | null }
const IDLE: Flash = { saving: false, saved: false, error: null }

export function useSiteSettings(section: string) {
  const [data, setData] = useState<Record<string, unknown> | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [flash, setFlash] = useState<Flash>(IDLE)

  const load = useCallback(async () => {
    setError(null)
    try {
      // overleaf-lab #9: shared 30 s cache — moving between sections no
      // longer re-fetches the whole payload (and never serves failures)
      const all = await getSiteSettings()
      setData((all as any)?.[section] || {})
    } catch (err: any) {
      setError((err?.data?.message as string) || String(err?.message || err))
    }
  }, [section])

  useEffect(() => {
    setData(null)
    void load()
  }, [load])

  const save = useCallback(
    async (body: Record<string, unknown>) => {
      setFlash({ saving: true, saved: false, error: null })
      try {
        await putJSON(`/admin/site-settings/${section}`, { body })
        setFlash({ saving: false, saved: true, error: null })
        // overleaf-lab #9: a save must be visible in this editor immediately —
        // invalidate, then read fresh (bypasses the shared TTL copy)
        invalidateSiteSettingsCache()
        const fresh = await getSiteSettings(true)
        setData((fresh as any)?.[section] || {})
        notify({ message: 'Saved.', color: 'teal' })
        return true
      } catch (err: any) {
        setFlash({
          saving: false,
          saved: false,
          error: (err?.data?.message as string) || String(err?.message || err),
        })
        return false
      }
    },
    [section]
  )

  return { data, error, load, flash, save }
}

function str(v: unknown, fallback = ''): string {
  if (v === undefined || v === null) return fallback
  return String(v)
}
function num(v: unknown, fallback = 0): number {
  const n = parseInt(String(v), 10)
  return Number.isNaN(n) ? fallback : n
}
function bool(v: unknown, fallback = false): boolean {
  return v === undefined || v === null ? fallback : Boolean(v)
}

export const str0 = str
export const num0 = num
export const bool0 = bool

// Shell: title + optional enable switch + description + save footer.
export function SectionShell({
  title,
  badge,
  enabled,
  onEnabled,
  description,
  children,
  footerNote,
  flash,
  onSave,
}: {
  title: string
  badge?: string
  enabled?: boolean
  onEnabled?: (v: boolean) => void
  description?: React.ReactNode
  children: React.ReactNode
  footerNote?: string
  flash: Flash
  onSave: () => void
}) {
  return (
    <Card withBorder radius={12} padding="xl">
      <Group justify="space-between" wrap="wrap" mb="sm" gap="sm">
        <Group gap="sm">
          <Text size="lg" fw={700}>{title}</Text>
          {badge ? <Badge size="sm" variant="light" radius="sm">{badge}</Badge> : null}
        </Group>
        {onEnabled ? (
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
            <Text size="sm" c="dimmed">{enabled ? 'Enabled' : 'Disabled'}</Text>
            <input
              type="checkbox"
              role="switch"
              aria-label={`Enable ${title}`}
              checked={Boolean(enabled)}
              onChange={e => onEnabled(e.currentTarget.checked)}
              style={{ width: 18, height: 18, accentColor: 'var(--mantine-color-ol-6, #1e6b41)' }}
            />
          </label>
        ) : null}
      </Group>
      {description ? <Text size="sm" c="dimmed" mb="md">{description}</Text> : null}
      {children}
      <Group justify="flex-end" align="flex-end" gap="xs" mt="lg" wrap="wrap">
        {flash.error ? (
          <Text size="sm" c="red" style={{ marginRight: 'auto' }}>{flash.error}</Text>
        ) : null}
        {flash.saved && !flash.saving ? (
          <Text size="sm" c="teal">Saved.</Text>
        ) : null}
        {footerNote ? <Text size="xs" c="dimmed">{footerNote}</Text> : null}
        <Button color="ollitex" loading={flash.saving} onClick={onSave}>
          Save
        </Button>
      </Group>
    </Card>
  )
}

export function SectionTitle({ children, top }: { children: React.ReactNode; top?: boolean }) {
  return (
    <Text size="sm" fw={700} c="blue" mt={top ? 'lg' : 'sm'} mb={top ? 'xs' : 'xs'}>
      {children}
    </Text>
  )
}

export function Field({
  label,
  value,
  onChange,
  placeholder,
  hint,
  required,
  type = 'text',
  width = '50%',
}: {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  hint?: string
  required?: boolean
  type?: 'text' | 'number'
  width?: string
}) {
  return (
    <div style={{ width, minWidth: 160 }}>
      <Text size="xs" fw={600} mb={4}>
        {label} {required ? <span style={{ color: 'var(--mantine-color-red-6)' }}>*</span> : null}
      </Text>
      {type === 'number' ? (
        <TextInput
          type="number"
          value={value}
          onChange={e => onChange(e.currentTarget.value)}
          placeholder={placeholder}
        />
      ) : (
        <TextInput
          value={value}
          onChange={e => onChange(e.currentTarget.value)}
          placeholder={placeholder}
        />
      )}
      {hint ? <Text size="xs" c="dimmed" mt={4}>{hint}</Text> : null}
    </div>
  )
}

export function Area({
  label,
  value,
  onChange,
  placeholder,
  hint,
  rows = 3,
  required,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  hint?: string
  rows?: number
  required?: boolean
}) {
  return (
    <div style={{ width: '100%' }}>
      <Text size="xs" fw={600} mb={4}>
        {label} {required ? <span style={{ color: 'var(--mantine-color-red-6)' }}>*</span> : null}
      </Text>
      <Textarea w="100%" autosize minRows={rows} value={value} onChange={e => onChange(e.currentTarget.value)} placeholder={placeholder} />
      {hint ? <Text size="xs" c="dimmed" mt={4}>{hint}</Text> : null}
    </div>
  )
}

// Secret input: masked in GET; "(configured — leave empty to keep)" placeholder
// when the server confirms a stored value; sending empty keeps the stored one.
export function SecretField({
  label,
  set,
  value,
  onChange,
  hint,
  width = '50%',
}: {
  label: string
  set?: boolean
  value: string
  onChange: (v: string) => void
  hint?: string
  width?: string
}) {
  return (
    <div style={{ width, minWidth: 160 }}>
      <Text size="xs" fw={600} mb={4}>
        {label}
      </Text>
      <TextInput
        type="password"
        value={value}
        onChange={e => onChange(e.currentTarget.value)}
        placeholder={set ? '•••••• (configured — leave empty to keep)' : ''}
        autoComplete="new-password"
      />
      {hint ? <Text size="xs" c="dimmed" mt={4}>{hint}</Text> : null}
      {set && !value ? <Text size="xs" c="dimmed" mt={4}>A value is stored.</Text> : null}
    </div>
  )
}

export function SwitchRow({
  label,
  checked,
  onChange,
  hint,
}: {
  label: string
  checked: boolean
  onChange: (v: boolean) => void
  hint?: string
}) {
  return (
    <label style={{ display: 'inline-flex', alignItems: 'flex-start', gap: 8, cursor: 'pointer' }}>
      <input
        type="checkbox"
        checked={checked}
        onChange={e => onChange(e.currentTarget.checked)}
        aria-label={label}
        style={{ width: 16, height: 16, marginTop: 2, accentColor: 'var(--mantine-color-ol-6, #1e6b41)' }}
      />
      <span>
        <span style={{ fontSize: 14 }}>{label}</span>
        {hint ? <Text size="xs" c="dimmed" inline>{hint}</Text> : null}
      </span>
    </label>
  )
}

export function NativeSelectField({
  label,
  value,
  onChange,
  options,
  required,
  width = '50%',
}: {
  label: string
  value: string
  onChange: (v: string) => void
  options: { value: string; label: string }[]
  required?: boolean
  width?: string
}) {
  return (
    <div style={{ width, minWidth: 160 }}>
      <Text size="xs" fw={600} mb={4}>
        {label} {required ? <span style={{ color: 'var(--mantine-color-red-6)' }}>*</span> : null}
      </Text>
      <select
        value={value}
        onChange={e => onChange(e.currentTarget.value)}
        aria-label={label}
        style={{
          width: '100%',
          padding: '7px 8px',
          borderRadius: 8,
          border: '1px solid var(--mantine-color-gray-4)',
          background: 'var(--mantine-color-body)',
          color: 'inherit',
          fontSize: 14,
        }}
      >
        {options.map(o => (
          <option key={o.value} value={o.value}>{o.label}</option>
        ))}
      </select>
    </div>
  )
}

export { PageError, PageLoading }
export function ErrorBox({ label, detail, onRetry }: { label: string; detail: string; onRetry: () => void }) {
  return (
    <Group justify="space-between" wrap="wrap" style={{ padding: 12, border: '1px solid var(--mantine-color-red-4)', borderRadius: 10 }}>
      <div>
        <Text size="sm" fw={600} c="red">{label}</Text>
        <Text size="xs" c="dimmed">{detail}</Text>
      </div>
      <Button size="xs" variant="light" onClick={onRetry}>Retry</Button>
    </Group>
  )
}

export function useSyncValues<T>(
  data: T | null,
  init: (d: T) => Record<string, unknown>,
) {
  const [v, setV] = useState<Record<string, unknown>>(() =>
    data ? init(data) : {}
  )
  useEffect(() => {
    if (data) setV(init(data))
  }, [data]) // eslint-disable-line react-hooks/exhaustive-deps
  const up = useCallback((patch: Record<string, unknown>) => {
    setV(p => ({ ...p, ...patch }))
  }, [])
  return { v, up, setV }
}
