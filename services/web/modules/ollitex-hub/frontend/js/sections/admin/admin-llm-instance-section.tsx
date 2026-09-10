import React, { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Group,
  Stack,
  Switch,
  TextInput,
  Text,
} from '@mantine/core'
import { getJSON, putJSON } from '@/infrastructure/fetch-json'
import { notify } from '../../shared/notify'
import { PageError, PageLoading } from '../../shared/page-state'

type LlmSiteSection = {
  enabled?: boolean
  allowUserSettings?: boolean
  userRatePerMinute?: number
  adminRatePerMinute?: number
  userDailyTokens?: number
  [key: string]: unknown
}

/**
 * 2026-09-16 (owner queue item 1): the "Instance LLM settings (admin)" card,
 * ported to the hub. The card lived only on the legacy /user/llm-settings
 * page (and the old /admin/site LLM tab) and drove the *site-settings*
 * `llm` section — a separate, instance-wide flag set from the per-feature
 * LLM admin settings on the neighbouring site.llm.* leaves:
 *
 *   enabled            – master switch for all LLM features (read at web
 *                        service start; applies on the next container cycle)
 *   allowUserSettings  – let users bring their own API provider (BYO keys)
 *   userRatePerMinute  – per-user request rate (0 = unlimited)
 *   adminRatePerMinute – per-admin request rate (0 = unlimited)
 *   userDailyTokens    – per-user daily token budget (0 = unlimited)
 *
 * Endpoints (same as the legacy card, unchanged):
 *   GET /admin/site-settings          → { llm: { … } }
 *   PUT /admin/site-settings/llm      → replaces the section (validated:
 *                                       booleans + integers >= 0)
 */
export default function AdminLlmInstanceSection() {
  const [section, setSection] = useState<LlmSiteSection | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    setError(null)
    setSection(null)
    try {
      const all = (await getJSON<Record<string, LlmSiteSection>>('/admin/site-settings')) || {}
      setSection(all.llm || {})
    } catch (err: any) {
      setError((err?.data?.message as string) || String(err?.message || err))
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  if (!section && !error) {
    return <PageLoading label="Loading instance LLM settings…" />
  }
  if (error) {
    return (
      <PageError
        label="Couldn’t load instance LLM settings"
        detail={error}
        onRetry={() => void load()}
      />
    )
  }
  if (!section) return null

  const enabled = section.enabled !== false
  const allowUser = section.allowUserSettings !== false
  const userRate = String(section.userRatePerMinute ?? 10)
  const adminRate = String(section.adminRatePerMinute ?? 0)
  const userDaily = String(section.userDailyTokens ?? 0)

  const patch = (p: LlmSiteSection) => setSection(s => (s ? { ...s, ...p } : s))

  const save = async () => {
    setSaving(true)
    try {
      await putJSON('/admin/site-settings/llm', {
        body: {
          enabled,
          allowUserSettings: allowUser,
          userRatePerMinute: parseInt(userRate, 10) || 0,
          adminRatePerMinute: parseInt(adminRate, 10) || 0,
          userDailyTokens: parseInt(userDaily, 10) || 0,
        },
      })
      notify({ message: 'Instance LLM settings saved.', color: 'teal' })
      await load()
    } catch (err: any) {
      notify({
        message: (err?.data?.message as string) || 'Could not save instance LLM settings.',
        color: 'red',
      })
      throw err
    } finally {
      setSaving(false)
    }
  }

  return (
    <Stack gap="md">
      <Card withBorder paddings="lg" radius="lg">
        <Stack gap="md">
          <div>
            <Text fw={700}>Instance LLM settings (admin)</Text>
            <Text size="sm" c="dimmed" mt={4}>
              Instance-wide LLM flags. The master switch is read when the web
              service starts (applies on the next container cycle); the BYO
              flag and the rate limits apply to new calls right away.
            </Text>
          </div>

          <Alert icon={null} variant="light" color="gray" withBorder radius="md">
            <Text size="sm">
              These instance flags are independent of the per-feature toggles
              under “Features” and the BYO providers users configure in
              My settings → LLM — an API call must pass both.
            </Text>
          </Alert>

          <Group justify="space-between" wrap="nowrap" gap="sm">
            <Text size="sm" fw={600}>
              LLM features enabled (Ask AI, LLM grammar, review)
            </Text>
            <Switch
              checked={enabled}
              onChange={() => patch({ enabled: !enabled })}
              color="ollitex"
              label={enabled ? 'Enabled' : 'Disabled'}
            />
          </Group>

          <Group justify="space-between" wrap="nowrap" gap="sm">
            <Text size="sm" fw={600}>
              Allow users to bring their own API provider (BYO keys)
            </Text>
            <Switch
              checked={allowUser}
              onChange={() => patch({ allowUserSettings: !allowUser })}
              color="ollitex"
              label={allowUser ? 'Allowed' : 'Not allowed'}
            />
          </Group>

          <Group gap="md" wrap="wrap">
            <div style={{ minWidth: 210, flex: 1 }}>
              <TextInput
                label="User request rate (per minute)"
                hint="0 = unlimited"
                type="number"
                min={0}
                value={userRate}
                onChange={e => patch({ userRatePerMinute: parseInt(e.currentTarget.value, 10) || 0 })}
              />
            </div>
            <div style={{ minWidth: 210, flex: 1 }}>
              <TextInput
                label="Admin request rate (per minute)"
                hint="0 = unlimited"
                type="number"
                min={0}
                value={adminRate}
                onChange={e => patch({ adminRatePerMinute: parseInt(e.currentTarget.value, 10) || 0 })}
              />
            </div>
            <div style={{ minWidth: 210, flex: 1 }}>
              <TextInput
                label="User daily token budget"
                hint="0 = unlimited"
                type="number"
                min={0}
                value={userDaily}
                onChange={e => patch({ userDailyTokens: parseInt(e.currentTarget.value, 10) || 0 })}
              />
            </div>
          </Group>

          <Group gap="xs" wrap="wrap" mt="xs">
            <Button color="ollitex" loading={saving} size="sm" onClick={() => void save().catch(() => undefined)}>
              Save
            </Button>
            <Text size="xs" c="dimmed">
              Master switch applies on the next container cycle.
            </Text>
          </Group>
        </Stack>
      </Card>
    </Stack>
  )
}
