// OlliTeX /hub admin — Email templates section (remember.md P7-post
// item 3: "Manage the email template under /hub admin settings (rebrand
// them templates to OlliTeX too). Text areas with save and reset to
// default").
//
// One card per outbound mail slot. Each card exposes textareas for the
// editable parts (subject / text / html — html only when the slot has an
// editable HTML part) against the slot's DEFAULTS; a per-slot Save pushes
// the changed fields to PUT /api/hub/email-templates/<name> (a field reset
// to its default is sent as "" = server-side reset of that field), and
// "Reset slot" issues DELETE /api/hub/email-templates/<name> (back to the
// shipped OlliTeX defaults). Allowed {{variables}} are shown per card —
// the server rejects any other variable with a 400 (the mail can never be
// broken).
import React, { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Badge,
  Button,
  Card,
  Group,
  Stack,
  Text,
  Textarea,
  TextInput,
} from '@mantine/core'
import { deleteJSON, getJSON, putJSON } from '@/infrastructure/fetch-json'
import { notify } from '../shared/notify'

type Slot = {
  name: string
  label: string
  help: string
  variables: string[]
  default: { subject: string; text: string; html: string }
  current: { subject: string; text: string; html: string }
  overridden: string[]
  updatedAt: string
  updatedBy: string
}

type Draft = { subject: string; text: string; html: string }

function changedFields(draft: Draft, current: Slot['current']): {
  subject?: string
  text?: string
  html?: string
} {
  const out: { subject?: string; text?: string; html?: string } = {}
  for (const f of ['subject', 'text', 'html'] as const) {
    if (f === 'html' && current.html === '') continue // no editable html part
    if (draft[f] === current[f]) continue // unchanged
    out[f] = draft[f] // "" (cleared textarea) = reset that field to default
  }
  return out
}

export default function EmailTemplatesSection() {
  const [slots, setSlots] = useState<Slot[] | null>(null)
  const [loadErr, setLoadErr] = useState<string | null>(null)
  const [drafts, setDrafts] = useState<Record<string, Draft>>({})
  const [busy, setBusy] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const data = await getJSON('/api/hub/email-templates')
      const list: Slot[] = data?.templates ?? []
      setSlots(list)
      const d: Record<string, Draft> = {}
      for (const s of list) d[s.name] = { ...s.current }
      setDrafts(d)
      setLoadErr(null)
    } catch (e: any) {
      setLoadErr(e?.message || 'failed to load email templates')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const setField = (slot: Slot, field: keyof Draft, value: string) => {
    setDrafts((prev) => ({ ...prev, [slot.name]: { ...prev[slot.name], [field]: value } }))
  }

  const save = async (slot: Slot) => {
    const draft = drafts[slot.name]
    if (!draft) return
    const body = changedFields(draft, slot.current)
    setBusy(slot.name + ':save')
    try {
      await putJSON('/api/hub/email-templates/' + slot.name, { body })
      notify({ message: 'Saved ' + slot.label, color: 'teal' })
      await load()
    } catch (e: any) {
      notify({
        message: e?.message || 'save failed',
        color: 'red',
        title: 'Email template save failed',
      })
    } finally {
      setBusy(null)
    }
  }

  const reset = async (slot: Slot) => {
    setBusy(slot.name + ':reset')
    try {
      await deleteJSON('/api/hub/email-templates/' + slot.name)
      notify({ message: 'Reset ' + slot.label + ' to OlliTeX defaults', color: 'teal' })
      await load()
    } catch (e: any) {
      notify({
        message: e?.message || 'reset failed',
        color: 'red',
        title: 'Email template reset failed',
      })
    } finally {
      setBusy(null)
    }
  }

  if (loadErr) {
    return (
      <Alert color="red" title="Could not load email templates">
        {loadErr}
      </Alert>
    )
  }
  if (!slots) {
    return <Text c="dimmed">Loading email templates…</Text>
  }

  return (
    <Stack gap={16}>
      <Text size="sm" c="dimmed">
        These are the outbound e-mails OlliTeX sends (invites, password resets, security notes,
        test mails). Edit a field and Save, or Reset a slot back to the shipped OlliTeX default.
        Templates may use the listed{' '}
        <code>{'{{variables}}'}</code>; any other variable is rejected.
      </Text>
      {slots.map((slot) => {
        const draft = drafts[slot.name]
        return (
          <Card key={slot.name} withBorder padding="md" radius="md">
            <Stack gap={10}>
              <Group justify="space-between" align="center" wrap="nowrap">
                <Group gap={8} align="center" wrap="nowrap">
                  <Text fw={600}>{slot.label}</Text>
                  <Badge
                    variant={slot.overridden.length ? 'filled' : 'light'}
                    color={slot.overridden.length ? 'orange' : 'gray'}
                  >
                    {slot.overridden.length
                      ? 'custom: ' + slot.overridden.join(', ')
                      : 'default'}
                  </Badge>
                </Group>
                <Group gap={6} wrap="nowrap">
                  {slot.overridden.length > 0 && (
                    <Button
                      size="xs"
                      variant="subtle"
                      onClick={() => void reset(slot)}
                      loading={busy === slot.name + ':reset'}
                    >
                      Reset to default
                    </Button>
                  )}
                  <Button
                    size="xs"
                    onClick={() => void save(slot)}
                    loading={busy === slot.name + ':save'}
                  >
                    Save
                  </Button>
                </Group>
              </Group>
              <Text size="xs" c="dimmed">
                {slot.help}
              </Text>
              <TextInput
                label="Subject"
                value={draft?.subject ?? ''}
                onChange={(e) => setField(slot, 'subject', e.currentTarget.value)}
                disabled={!draft}
              />
              <Textarea
                label="Text (plain)"
                value={draft?.text ?? ''}
                onChange={(e) => setField(slot, 'text', e.currentTarget.value)}
                minRows={4}
                maxRows={14}
                autosize
                disabled={!draft}
              />
              {slot.default.html !== '' && (
                <Textarea
                  label="HTML (text/html part)"
                  value={draft?.html ?? ''}
                  onChange={(e) => setField(slot, 'html', e.currentTarget.value)}
                  minRows={6}
                  maxRows={20}
                  autosize
                  disabled={!draft}
                />
              )}
              <Group gap={6} wrap="wrap">
                <Text size="xs" c="dimmed" tt="lower" fz={11}>
                  allowed variables:
                </Text>
                {slot.variables.map((v) => (
                  <Badge key={v} variant="outline" color="gray">
                    {'{{' + v + '}}'}
                  </Badge>
                ))}
              </Group>
            </Stack>
          </Card>
        )
      })}
    </Stack>
  )
}
