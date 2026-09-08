import React, { useCallback, useEffect, useState } from 'react'
import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Group,
  Modal,
  NativeSelect,
  Stack,
  Switch,
  Table,
  Text,
  Textarea,
  TextInput,
  Tooltip,
} from '@mantine/core'
import { notify } from '../../shared/notify'
import { getJSON, postJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import ConfirmModal from '../../shared/confirm-modal'
import { EmptyState, PageError, PageLoading } from '../../shared/page-state'

type ProviderRow = {
  id: string
  name: string
  providerType: 'openai' | 'anthropic' | 'openaiCompatible'
  baseUrl: string
  hasKey: boolean
  models: string[]
  completionModel: string
  enabled: boolean
}

type Draft = {
  isNew: boolean
  id: string
  name: string
  providerType: string
  baseUrl: string
  apiKey: string
  keepKey: boolean
  storedKey: boolean
  models: string
  completionModel: string
  enabled: boolean
}

function typeLabel(t: string): string {
  if (t === 'openai') return 'OpenAI'
  if (t === 'anthropic') return 'Anthropic'
  return 'OpenAI-compatible'
}

export default function LlmSettingsSection() {
  const [rows, setRows] = useState<ProviderRow[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [draftNotice, setDraftNotice] = useState<{ ok: boolean; text: string } | null>(null)
  const [busyKey, setBusyKey] = useState<string | null>(null)
  const [toDelete, setToDelete] = useState<ProviderRow | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    setError(null)
    try {
      const data = await getJSON('/user/llm-providers')
      const list = Array.isArray(data) ? data : data?.providers ?? data?.items ?? []
      setRows(
        list.map((r: any) => ({
          id: r.id || r._id,
          name: r.name || 'Provider',
          providerType: r.providerType || 'openaiCompatible',
          baseUrl: r.baseUrl || '',
          hasKey: !!r.hasKey,
          models: Array.isArray(r.models) ? r.models : [],
          completionModel: r.completionModel || '',
          enabled: r.enabled !== false,
        }))
      )
    } catch (e: any) {
      setRows([])
      setError((e?.data?.message as string) || String(e?.message || e))
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const openNew = () => {
    setDraft({
      isNew: true,
      id: '',
      name: '',
      providerType: 'openaiCompatible',
      baseUrl: '',
      apiKey: '',
      keepKey: true,
      storedKey: false,
      models: '',
      completionModel: '',
      enabled: true,
    })
    setDraftNotice(null)
  }

  const openEdit = (row: ProviderRow) => {
    setDraft({
      isNew: false,
      id: row.id,
      name: row.name,
      providerType: row.providerType,
      baseUrl: row.baseUrl,
      apiKey: '',
      keepKey: row.hasKey,
      storedKey: row.hasKey,
      models: (row.models || []).join('\n'),
      completionModel: row.completionModel || '',
      enabled: row.enabled,
    })
    setDraftNotice(null)
  }

  const checkConnection = async () => {
    if (!draft) return
    setBusyKey('check')
    setDraftNotice(null)
    try {
      const body: Record<string, unknown> = {}
      if (draft.baseUrl) body.baseUrl = draft.baseUrl.trim()
      if (draft.apiKey.trim()) body.apiKey = draft.apiKey.trim()
      if (!draft.isNew) body.rowId = draft.id
      const res = await postJSON('/user/llm-providers/check', { body })
      if (res?.ok === false || res?.status === 'error') {
        setDraftNotice({ ok: false, text: res?.message || 'Connection check failed.' })
      } else {
        const detected = res?.detectedProviderType
        if (detected && detected !== draft.providerType) setDraft(d => (d ? { ...d, providerType: detected as string } : d))
        setDraftNotice({ ok: true, text: 'Connection OK' + (detected ? ` (detected: ${typeLabel(detected)})` : '') })
      }
    } catch (e: any) {
      setDraftNotice({ ok: false, text: (e?.data?.message as string) || 'Connection check failed.' })
    } finally {
      setBusyKey(null)
    }
  }

  const scanModels = async () => {
    if (!draft) return
    setBusyKey('scan')
    setDraftNotice(null)
    try {
      const body: Record<string, unknown> = {}
      if (draft.baseUrl) body.baseUrl = draft.baseUrl.trim()
      if (draft.apiKey.trim()) body.apiKey = draft.apiKey.trim()
      if (!draft.isNew && draft.keepKey) body.rowId = draft.id
      const res = await postJSON('/user/llm-providers/scan', { body })
      const found: string[] = Array.isArray(res?.models) ? res.models : []
      if (found.length === 0) {
        setDraftNotice({ ok: false, text: 'The backend reported no models.' })
      } else {
        setDraft(d => (d ? { ...d, models: found.join('\n') } : d))
        setDraftNotice({ ok: true, text: `Fetched ${found.length} models into the list.` })
      }
    } catch (e: any) {
      setDraftNotice({ ok: false, text: (e?.data?.message as string) || 'Model scan failed.' })
    } finally {
      setBusyKey(null)
    }
  }

  const saveDraft = async () => {
    if (!draft) return
    const models = draft.models
      .split(/[\n,]/)
      .map(s => s.trim())
      .filter(Boolean)
    const problems: string[] = []
    if (!draft.name.trim()) problems.push('Provider name is required.')
    if (models.length === 0) problems.push('Add at least one model (one per line).')
    if (draft.providerType === 'openaiCompatible' && !draft.baseUrl.trim()) problems.push('Base URL is required for OpenAI-compatible endpoints.')
    if (draft.completionModel && !models.includes(draft.completionModel)) problems.push('Completion model must be one of the listed models.')
    if (problems.length) {
      setDraftNotice({ ok: false, text: problems.join(' ') })
      return
    }
    setSaving(true)
    setDraftNotice(null)
    try {
      const payload: Record<string, unknown> = {
        name: draft.name.trim(),
        providerType: draft.providerType,
        baseUrl: draft.baseUrl.trim(),
        models,
        enabled: draft.enabled,
      }
      if (draft.completionModel) payload.completionModel = draft.completionModel
      if (draft.apiKey.trim()) payload.apiKey = draft.apiKey.trim()
      if (!draft.isNew && !draft.keepKey && !draft.apiKey.trim()) payload.clearApiKey = true
      if (draft.isNew) {
        await postJSON('/user/llm-providers', { body: payload })
      } else {
        await postJSON(`/user/llm-providers/${draft.id}`, { body: payload })
      }
      notify({ message: 'LLM provider saved.', color: 'teal' })
      setDraft(null)
      await load()
    } catch (e: any) {
      setDraftNotice({ ok: false, text: (e?.data?.message as string) || 'Could not save the provider.' })
    } finally {
      setSaving(false)
    }
  }

  const doDelete = async () => {
    if (!toDelete) return
    setDeleting(true)
    try {
      await postJSON(`/user/llm-providers/${toDelete.id}/delete`, { body: {} })
      notify({ message: `Deleted “${toDelete.name}”.`, color: 'gray' })
      setToDelete(null)
      await load()
    } catch (e: any) {
      notify({ message: (e?.data?.message as string) || 'Delete failed.', color: 'red' })
      setToDelete(null)
    } finally {
      setDeleting(false)
    }
  }

  if (!rows && !error) return <PageLoading label="Loading your LLM providers…" />

  return (
    <Stack gap="md">
      <Alert icon={null} variant="light" color="gray" withBorder radius="md">
        <Text size="sm">
          Bring-your-own LLM endpoints: connect an OpenAI-compatible or Anthropic API and choose
          which models are available in this instance's editor. API keys are encrypted at rest and
          never sent back to the browser.
        </Text>
      </Alert>

      <Group justify="space-between" wrap="wrap">
        <Text size="sm" c="dimmed">
          {rows ? `${rows.length} provider${rows.length === 1 ? '' : 's'}` : ''}
        </Text>
        <Button size="md" color="ollitex" leftSection={<Icon name="add" size={18} />} onClick={openNew}>
          Add provider
        </Button>
      </Group>

      {error ? <PageError label="Couldn’t load providers" detail={error} onRetry={() => void load()} /> : null}

      {rows && rows.length === 0 ? (
        <EmptyState
          icon="smart_toy"
          title="No LLM providers configured"
          hint="Connect your own OpenAI-compatible or Anthropic endpoint to enable AI features in the editor."
          action={
            <Button size="sm" color="ollitex" leftSection={<Icon name="add" size={16} />} onClick={openNew}>
              Add provider
            </Button>
          }
        />
      ) : null}

      {rows && rows.length > 0 ? (
        <Table striped highlightOnHover withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Provider</Table.Th>
              <Table.Th style={{ width: 160 }}>Type</Table.Th>
              <Table.Th>Base URL</Table.Th>
              <Table.Th style={{ width: 120 }}>Models</Table.Th>
              <Table.Th style={{ width: 90 }}>Key</Table.Th>
              <Table.Th style={{ width: 90 }}>On</Table.Th>
              <Table.Th style={{ width: 120, textAlign: 'right' }}>Actions</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {rows.map(row => (
              <Table.Tr key={row.id}>
                <Table.Td>
                  <Text size="sm" fw={600}>{row.name}</Text>
                  {row.completionModel ? (
                    <Text size="xs" c="dimmed">completion: {row.completionModel}</Text>
                  ) : null}
                </Table.Td>
                <Table.Td>
                  <Badge size="xs" variant="light" radius="sm" color="blue">
                    {typeLabel(row.providerType)}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" c="dimmed" style={{ wordBreak: 'break-all' }}>{row.baseUrl || '—'}</Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">{row.models.length}</Text>
                  <Text size="xs" c="dimmed" ellipsis style={{ maxWidth: 140 }}>
                    {row.models.slice(0, 3).join(', ')}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" c={row.hasKey ? 'teal' : 'dimmed'}>
                    {row.hasKey ? '••••' : '—'}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Switch
                    size="xs"
                    checked={row.enabled !== false}
                    color="ollitex"
                    onChange={async v => {
                      const next = { ...row, enabled: v }
                      setRows(rs => (rs ? rs.map(r => (r.id === row.id ? next : r)) : rs))
                      try {
                        await postJSON(`/user/llm-providers/${row.id}`, { body: { enabled: v } })
                      } catch (e: any) {
                        notify({ message: (e?.data?.message as string) || 'Could not update.', color: 'red' })
                      }
                    }}
                  />
                </Table.Td>
                <Table.Td style={{ textAlign: 'right' }}>
                  <Group gap={4} justify="flex-end">
                    <Tooltip label="Edit">
                      <ActionIcon variant="subtle" aria-label="Edit" onClick={() => openEdit(row)} style={{ cursor: 'pointer' }}>
                        <Icon name="edit" size={18} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="Delete">
                      <ActionIcon variant="subtle" color="red" aria-label="Delete" onClick={() => setToDelete(row)} style={{ cursor: 'pointer' }}>
                        <Icon name="delete" size={18} />
                      </ActionIcon>
                    </Tooltip>
                  </Group>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      ) : null}

      <Modal
        opened={!!draft}
        onClose={() => setDraft(null)}
        size="md"
        title={<Text fw={700}>{draft?.isNew ? 'Add LLM provider' : `Edit “${draft?.name}”`}</Text>}
        // PG-HF-1 (parity/e2e): Mantine's focus trap raced the controlled
        // re-render — the second field fill would unmount the whole modal
        // (observed in e2e: one fill OK, second fill → dialog gone).
        // trapFocus=false keeps Esc/outside-click close semantics without the
        // racing focus checks.
        trapFocus={false}
      >
        <Stack gap="md">
          <Group gap="md" wrap="wrap">
            <div style={{ flex: 1, minWidth: 200 }}>
              <Text size="sm" fw={600} mb={6}>
                Name
              </Text>
              <TextInput
                aria-label="LLM profile name"
                value={draft?.name || ''}
                onChange={e => { const v = e.currentTarget.value; setDraft(d => (d ? { ...d, name: v } : d)) }}
                placeholder="e.g. Internal LLM gateway"
              />
            </div>
            <div style={{ width: 210 }}>
              <Text size="sm" fw={600} mb={6}>
                API type
              </Text>
              <NativeSelect
                value={draft?.providerType || 'openaiCompatible'}
                onChange={e => { const v = e.currentTarget.value; setDraft(d => (d ? { ...d, providerType: v } : d)) }}
                data={[
                  { value: 'openaiCompatible', label: 'OpenAI-compatible' },
                  { value: 'openai', label: 'OpenAI' },
                  { value: 'anthropic', label: 'Anthropic' },
                ]}
              />
            </div>
          </Group>

          <div>
            <Text size="sm" fw={600} mb={6}>
              Base URL {draft?.providerType === 'openaiCompatible' ? <span style={{ color: 'var(--mantine-color-red-6)' }}>*</span> : null}
            </Text>
            <TextInput
              aria-label="API base URL"
              value={draft?.baseUrl || ''}
              onChange={e => { const v = e.currentTarget.value; setDraft(d => (d ? { ...d, baseUrl: v } : d)) }}
              placeholder="https://…/v1"
            />
          </div>

          <div>
            <Text size="sm" fw={600} mb={6}>
              API key {draft && !draft.isNew ? '(leave blank to keep the stored key)' : '(encrypted at rest)'}
            </Text>
            <TextInput type="password"
              aria-label="API key"
              value={draft?.apiKey || ''}
              onChange={e => { const v = e.currentTarget.value; setDraft(d => (d ? { ...d, apiKey: v, keepKey: !!v || d.keepKey } : d)) }}
              placeholder="sk-…"
            />
          </div>

          <div>
            <Text size="sm" fw={600} mb={6}>
              Models <span style={{ color: 'var(--mantine-color-red-6)' }}>*</span>{' '}
              <Text span size="xs" c="dimmed" fw={400}>(one per line)</Text>
            </Text>
            <Textarea
              aria-label="Models (one per line)"
              value={draft?.models || ''}
              onChange={e => { const v = e.currentTarget.value; setDraft(d => (d ? { ...d, models: v } : d)) }}
              minRows={3}
              maxRows={8}
              placeholder={'gpt-4o-mini\nllama3.1-70b'}
            />
          </div>

          <div style={{ maxWidth: 320 }}>
            <Text size="sm" fw={600} mb={6}>
              Inline-completion model (optional)
            </Text>
            <NativeSelect
              value={draft?.completionModel || ''}
              onChange={e => { const v = e.currentTarget.value; setDraft(d => (d ? { ...d, completionModel: v } : d)) }}
              data={[
                { value: '', label: 'None' },
                ...(draft ? draft.models.split(/[\n,]/).map(s => s.trim()).filter(Boolean).map(m => ({ value: m, label: m })) : []),
              ]}
            />
          </div>

          <Group justify="space-between" wrap="nowrap">
            <Group wrap="nowrap" gap="xs">
              <Switch
                checked={draft?.enabled !== false}
                onChange={() => setDraft(d => (d ? { ...d, enabled: d.enabled === true } : d))}
                label="Enabled for this instance"
                color="ollitex"
              />
            </Group>
            <Group gap="xs">
              <Button variant="default" size="sm" loading={busyKey === 'check'} onClick={() => void checkConnection()} disabled={!draft?.baseUrl && (draft?.isNew || !draft?.apiKey)}>
                Check connection
              </Button>
              <Button variant="default" size="sm" loading={busyKey === 'scan'} onClick={() => void scanModels()} disabled={!draft?.baseUrl && (draft?.isNew || !draft?.apiKey)}>
                Scan models
              </Button>
            </Group>
          </Group>

          {draftNotice ? (
            <Alert icon={null} variant="light" color={draftNotice.ok ? 'teal' : 'red'}>
              <Text size="sm">{draftNotice.text}</Text>
            </Alert>
          ) : null}

          <Group justify="flex-end" gap="xs" mt="sm">
            <Button variant="default" onClick={() => setDraft(null)} disabled={saving}>
              Cancel
            </Button>
            <Button color="ollitex" loading={saving} onClick={() => void saveDraft()}>
              Save provider
            </Button>
          </Group>
        </Stack>
      </Modal>

      <ConfirmModal
        open={!!toDelete}
        title="Delete provider?"
        body={toDelete ? `“${toDelete.name}” and its stored API key will be removed from the instance.` : ''}
        confirmLabel="Delete"
        danger
        loading={deleting}
        onCancel={() => setToDelete(null)}
        onConfirm={() => void doDelete()}
      />
    </Stack>
  )
}
