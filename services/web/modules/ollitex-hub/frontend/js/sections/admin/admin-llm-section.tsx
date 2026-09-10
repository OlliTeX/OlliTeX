import React, { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Group,
  NativeSelect,
  Stack,
  Switch,
  Table,
  Text,
  Textarea,
  TextInput,
} from '@mantine/core'
import { notify } from '../../shared/notify'

import LlmUsagePanel from '../../shared/llm-usage-panel'
import { getJSON, postJSON } from '@/infrastructure/fetch-json'
import { PageError, PageLoading } from '../../shared/page-state'
// overleaf-lab: the shared usage meter (admin scope → /admin/llm/usage)

type Section = 'features' | 'connection' | 'models' | 'prompt' | 'prompts' | 'usage'

type LlmAdmin = {
  systemPrompt?: string
  llmApiUrl?: string
  llmApiType?: string
  hasLlmApiKey?: boolean
  allowedModels?: string[]
  knownModels?: string[]
  chatEnabled?: boolean
  completionEnabled?: boolean
  reviewEnabled?: boolean
  llmDisabledByAdmin?: boolean
  languageToolUrl?: string
  languageToolDisabledByAdmin?: boolean
  maxContextTokens?: number
  reviewMaxTokens?: number
  askAiSystemPrompt?: string
  errorPrompt?: string
  reviewSystemPrompt?: string
  askAiActionPrompts?: Record<string, string>
  promptDefaults?: {
    askAiSystemPrompt?: string
    errorPrompt?: string
    reviewSystemPrompt?: string
    askAiActionPrompts?: Record<string, string>
  }
  llmApiUrlFromEnv?: boolean
  llmApiTypeFromEnv?: boolean
}

/**
 * Loads the full admin LLM settings once and exposes a merge-save so every
 * hub section view (features / connection / models / prompt / prompts / usage)
 * edits only its slice while the payload stays the complete settings object
 * (zero parameter loss — unknown fields are preserved).
 */
function useAdminLlm() {
  const [state, setState] = useState<LlmAdmin | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    setError(null)
    try {
      const s = (await getJSON('/admin/llm/settings/json')) || {}
      setState({
        systemPrompt: s.systemPrompt || '',
        llmApiUrl: s.llmApiUrl || '',
        llmApiType: s.llmApiType || 'openai',
        hasLlmApiKey: !!s.hasLlmApiKey,
        allowedModels: Array.isArray(s.allowedModels) ? s.allowedModels : [],
        knownModels: Array.isArray(s.knownModels) ? s.knownModels : [],
        chatEnabled: s.chatEnabled !== false,
        completionEnabled: s.completionEnabled !== false,
        reviewEnabled: s.reviewEnabled !== false,
        llmDisabledByAdmin: !!s.llmDisabledByAdmin,
        languageToolUrl: s.languageToolUrl || '',
        languageToolDisabledByAdmin: s.languageToolDisabledByAdmin === true,
        maxContextTokens: typeof s.maxContextTokens === 'number' ? s.maxContextTokens : null,
        reviewMaxTokens: typeof s.reviewMaxTokens === 'number' ? s.reviewMaxTokens : null,
        askAiSystemPrompt: s.askAiSystemPrompt || '',
        errorPrompt: s.errorPrompt || '',
        reviewSystemPrompt: s.reviewSystemPrompt || '',
        askAiActionPrompts:
          s.askAiActionPrompts && typeof s.askAiActionPrompts === 'object'
            ? s.askAiActionPrompts
            : {},
        promptDefaults:
          s.promptDefaults && typeof s.promptDefaults === 'object' ? s.promptDefaults : undefined,
        llmApiUrlFromEnv: !!s.llmApiUrlFromEnv,
        llmApiTypeFromEnv: !!s.llmApiTypeFromEnv,
      })
    } catch (err: any) {
      setError((err?.data?.message as string) || String(err?.message || err))
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const save = async (patch: Record<string, unknown>, extra: Record<string, unknown> = {}) => {
    if (!state) return
    setSaving(true)
    try {
      const body: Record<string, unknown> = {
        // complete state → merge-save keeps every parameter intact
        systemPrompt: patch.systemPrompt !== undefined ? patch.systemPrompt : state.systemPrompt,
        llmApiUrl: patch.llmApiUrl !== undefined ? patch.llmApiUrl : state.llmApiUrl,
        llmApiType: patch.llmApiType !== undefined ? patch.llmApiType : state.llmApiType,
        allowedModels: patch.allowedModels !== undefined ? patch.allowedModels : state.allowedModels,
        knownModels: state.knownModels,
        chatEnabled: patch.chatEnabled !== undefined ? patch.chatEnabled : state.chatEnabled,
        completionEnabled: patch.completionEnabled !== undefined ? patch.completionEnabled : state.completionEnabled,
        reviewEnabled: patch.reviewEnabled !== undefined ? patch.reviewEnabled : state.reviewEnabled,
        llmDisabledByAdmin:
          patch.llmDisabledByAdmin !== undefined ? patch.llmDisabledByAdmin : state.llmDisabledByAdmin,
        languageToolUrl:
          patch.languageToolUrl !== undefined ? patch.languageToolUrl : state.languageToolUrl,
        languageToolDisabledByAdmin:
          patch.languageToolDisabledByAdmin !== undefined
            ? patch.languageToolDisabledByAdmin
            : state.languageToolDisabledByAdmin,
        maxContextTokens:
          patch.maxContextTokens !== undefined ? patch.maxContextTokens : state.maxContextTokens,
        reviewMaxTokens:
          patch.reviewMaxTokens !== undefined ? patch.reviewMaxTokens : state.reviewMaxTokens,
        askAiSystemPrompt:
          patch.askAiSystemPrompt !== undefined ? patch.askAiSystemPrompt : state.askAiSystemPrompt,
        errorPrompt: patch.errorPrompt !== undefined ? patch.errorPrompt : state.errorPrompt,
        reviewSystemPrompt:
          patch.reviewSystemPrompt !== undefined ? patch.reviewSystemPrompt : state.reviewSystemPrompt,
        askAiActionPrompts:
          patch.askAiActionPrompts !== undefined
            ? patch.askAiActionPrompts
            : state.askAiActionPrompts,
        ...extra,
      }
      await postJSON('/admin/llm/settings', { body })
      notify({ message: 'LLM instance settings saved.', color: 'teal' })
      await load()
    } catch (err: any) {
      notify({
        message: (err?.data?.message as string) || 'Could not save LLM settings.',
        color: 'red',
      })
      throw err
    } finally {
      setSaving(false)
    }
  }

  const checkConnection = async (llmApiUrl: string, llmApiKey: string) => {
    const [res, err] = await Promise.all([
      postJSON('/admin/llm/settings/check', {
        body: { llmApiUrl: llmApiUrl || '', llmApiKey: llmApiKey || undefined },
      }),
      Promise.resolve(null),
    ]).catch((e): [undefined, any] => [undefined, e])
    if (err) return { ok: false, text: (err?.data?.message as string) || 'Connection check failed.' }
    const ok = res?.ok !== false && res?.status !== 'error'
    return {
      ok,
      text: (res?.message || res?.model || res?.status || (ok ? 'Connection OK' : 'Check failed')) as string,
    }
  }

  const checkLanguageTool = async (url: string) => {
    try {
      const res: any = await postJSON('/admin/languagetool/check', { body: { serverUrl: url || '' } })
      return { ok: res?.ok !== false && res?.status !== 'error', message: res?.message || (res?.ok ? 'LanguageTool reachable' : 'LanguageTool check failed') }
    } catch (e: any) {
      return { ok: false, message: (e?.data?.message as string) || 'LanguageTool check failed.' }
    }
  }

  // Owner #29 (2026-09-13): provider model scan (same endpoint as the legacy
  // admin page — POST /admin/llm/models {apiUrl, apiType}).
  const scanModels = async (apiUrl: string, apiType: string) => {
    try {
      const res: any = await postJSON('/admin/llm/models', {
        body: { apiUrl: apiUrl || '', apiType: apiType || 'openai' },
      })
      if (res?.success && Array.isArray(res.models)) return { ok: true, models: res.models as string[] }
      return { ok: false, models: [] as string[] }
    } catch (e: any) {
      return { ok: false, models: [] as string[], message: (e?.data?.message as string) || 'Model scan failed.' }
    }
  }

  return { state, setState, error, saving, load, save, checkConnection, checkLanguageTool, scanModels }
}

function SectionCard({
  title,
  help,
  children,
  actions,
}: {
  title: string
  help?: string
  children: React.ReactNode
  actions?: React.ReactNode
}) {
  return (
    <Card withBorder paddings="lg" radius="lg">
      <Stack gap="md">
        <div>
          <Text fw={700}>{title}</Text>
          {help ? <Text size="sm" c="dimmed" mt={4}>{help}</Text> : null}
        </div>
        {children}
        {actions ? <Group gap="xs" wrap="wrap" mt="xs">{actions}</Group> : null}
      </Stack>
    </Card>
  )
}

/**
 * Admin LLM instance settings (owner 2026-09-07 #1: each site.llm.* leaf now
 * shows ONLY its own section — before, all six leaves rendered the same
 * combined page). Sections mirror the legacy /admin LLM page (features,
 * connection, models, prompt, prompts, usage) and save the full settings
 * object, so nothing is lost between views.
 */
export default function AdminLlmSection({ section = 'all' }: { section?: Section | 'all' }) {
  const api = useAdminLlm()
  const { state, error, saving } = api

  const [apiKey, setApiKey] = useState('')
  const [clearKey, setClearKey] = useState(false)
  const [checking, setChecking] = useState(false)
  const [testResult, setTestResult] = useState<{ ok: boolean; text: string } | null>(null)
  const [ltBusy, setLtBusy] = useState(false)
  const [ltResult, setLtResult] = useState<{ ok: boolean; message: string } | null>(null)
  const [actionKeys, setActionKeys] = useState<string[]>([])
  const [scanned, setScanned] = useState<string[]>([])
  const [scanBusy, setScanBusy] = useState(false)
  const [scanMsg, setScanMsg] = useState<string | null>(null)

  useEffect(() => {
    if (state) setActionKeys(Object.keys(state.askAiActionPrompts || {}))
  }, [state])

  if (!state && !error) return <PageLoading label="Loading LLM instance settings…" />
  if (error)
    return <PageError label="Couldn’t load LLM settings" detail={error} onRetry={() => void api.load()} />
  if (!state) return null

  const show = (s: Section) => section === 'all' || section === s

  const doConnectionTest = async () => {
    setChecking(true)
    setTestResult(null)
    try {
      const res = await api.checkConnection(state.llmApiUrl || '', apiKey.trim())
      setTestResult(res)
    } finally {
      setChecking(false)
    }
  }

  return (
    <Stack gap="md">
      <Alert icon={null} variant="light" color="gray" withBorder radius="md">
        <Text size="sm">
          Instance-wide LLM endpoint. User bring-your-own providers (managed in My settings → LLM)
          flow through this instance configuration when the feature is enabled.
        </Text>
      </Alert>

      {/* ── Features ─────────────────────────────────────────────────────── */}
      {show('features') ? (
        <SectionCard
          title="Enable AI features instance-wide"
          help="Per-feature availability for every user (the force-off switches live under Services below)."
          actions={
            <Button color="ollitex" loading={saving} size="sm" onClick={() => {
              void api.save({
                chatEnabled: state!.chatEnabled,
                completionEnabled: state!.completionEnabled,
                reviewEnabled: state!.reviewEnabled,
              }).catch(() => undefined)
            }}>
              Save
            </Button>
          }
        >
          <Group gap="lg" wrap="wrap">
            <Switch checked={state.chatEnabled} onChange={() => setState(s => (s ? { ...s, chatEnabled: state.chatEnabled === true } : s))} label="Chat (Ask AI)" color="ollitex" />
            <Switch checked={state.completionEnabled} onChange={() => setState(s => (s ? { ...s, completionEnabled: state.completionEnabled === true } : s))} label="Inline completion" color="ollitex" />
            <Switch checked={state.reviewEnabled} onChange={() => setState(s => (s ? { ...s, reviewEnabled: state.reviewEnabled === true } : s))} label="Review panel" color="ollitex" />
          </Group>
        </SectionCard>
      ) : null}

      {/* ── Services (availability) — owner #32: LanguageTool gets its own card ── */}
      {show('features') ? (
        <SectionCard
          title="Services (availability)"
          help="Force the AI or LanguageTool grammar checking off for every user, even configured / BYO."
          actions={
            <Group gap="xs" align="flex-end">
              <Button variant="default" size="sm" h={34} loading={ltBusy} onClick={() => {
                setLtBusy(true); setLtResult(null)
                api.checkLanguageTool(state!.languageToolUrl).then(r => { setLtResult(r); setLtBusy(false) })
              }}>
                Check LanguageTool connection
              </Button>
              <Button color="ollitex" loading={saving} size="sm" h={34} onClick={() => {
                void api.save({
                  llmDisabledByAdmin: state!.llmDisabledByAdmin,
                  languageToolDisabledByAdmin: state!.languageToolDisabledByAdmin,
                  languageToolUrl: state!.languageToolUrl,
                }).catch(() => undefined)
              }}>
                Save
              </Button>
            </Group>
          }
        >
          <Group justify="space-between" wrap="nowrap" gap="sm">
            <Text size="sm" fw={600}>Master switch</Text>
            <Switch
              checked={!state.llmDisabledByAdmin}
              onChange={() => setState(s => (s ? { ...s, llmDisabledByAdmin: state.llmDisabledByAdmin === true } : s))}
              color="ollitex"
              label={state.llmDisabledByAdmin ? 'AI disabled' : 'AI enabled'}
            />
          </Group>
          <Group justify="space-between" wrap="nowrap" gap="sm">
            <Text size="sm" fw={600}>
              Disable LanguageTool for all users (force off)
            </Text>
            <Switch
              checked={!state.languageToolDisabledByAdmin}
              onChange={() => setState(s => (s ? { ...s, languageToolDisabledByAdmin: state.languageToolDisabledByAdmin === true } : s))}
              color="ollitex"
            />
          </Group>
          <Group gap="xs" align="flex-end">
            <div style={{ flex: 1, minWidth: 260 }}>
              <TextInput
                label="LanguageTool server URL"
                placeholder="http://languagetool:8010"
                value={state.languageToolUrl || ''}
                onChange={e => setState(s => (s ? { ...s, languageToolUrl: e.currentTarget.value } : s))}
              />
            </div>
            <Text size="xs" c="dimmed" pr={2}>Blank = env fallbacks only</Text>
          </Group>
          {ltResult ? (
            <Alert icon={null} variant="light" color={ltResult.ok ? 'teal' : 'red'}>
              <Text size="sm">{ltResult.message}</Text>
            </Alert>
          ) : null}
        </SectionCard>
      ) : null}

      {/* ── API Connection ───────────────────────────────────────────────── */}
      {show('connection') ? (
        <SectionCard
          title="API Connection"
          help="The endpoint every AI feature calls. Keys are stored encrypted; leaving the field blank keeps the stored key."
          actions={
            <>
              <Button color="ollitex" loading={saving} size="sm"
                onClick={() => {
                  void api.save(
                    {
                      llmApiUrl: state!.llmApiUrl,
                      llmApiType: state!.llmApiType,
                    },
                    {
                      llmApiKey: apiKey.trim() || undefined,
                      clearLlmApiKey: clearKey,
                    }
                  ).then(() => { setApiKey(''); setClearKey(false) }).catch(() => undefined)
                }}>
                Save connection
              </Button>
              <Button variant="default" size="sm" loading={checking} onClick={() => void doConnectionTest()}>
                Test connection
              </Button>
            </>
          }
        >
          <Group gap="md" wrap="wrap">
            <div style={{ minWidth: 220 }}>
              <Text size="sm" fw={600} mb={6}>API type</Text>
              <NativeSelect
                value={state.llmApiType || 'openai'}
                onChange={e => setState(s => (s ? { ...s, llmApiType: e.currentTarget.value } : s))}
                data={[
                  { value: 'openai', label: 'OpenAI' },
                  { value: 'openaiCompatible', label: 'OpenAI-compatible' },
                  { value: 'anthropic', label: 'Anthropic' },
                ]}
              />
            </div>
            <div style={{ flex: 1, minWidth: 240 }}>
              <Text size="sm" fw={600} mb={6}>API base URL</Text>
              <TextInput
                aria-label="API base URL"
                value={state.llmApiUrl || ''}
                onChange={e => setState(s => (s ? { ...s, llmApiUrl: e.currentTarget.value } : s))}
                placeholder="https://api.openai.com/v1"
              />
            </div>
          </Group>
          <div>
            <Text size="sm" fw={600} mb={6}>
              API key{' '}
              <Text span size="xs" c="dimmed" fw={400}>
                {state.hasLlmApiKey ? '(a key is stored)' : '(no key stored)'} — stored encrypted
              </Text>
            </Text>
            <Group gap="xs" wrap="wrap">
              <div style={{ flex: 1, minWidth: 260 }}>
                <TextInput
                  type="password"
                  aria-label="API key"
                  value={apiKey}
                  onChange={e => {
                    setApiKey(e.currentTarget.value)
                    if (e.currentTarget.value) setClearKey(false)
                  }}
                  placeholder={state.hasLlmApiKey ? 'Replace with a new key…' : 'sk-…'}
                />
              </div>
              <Button
                variant="default"
                size="sm"
                color="red"
                disabled={!state.hasLlmApiKey}
                onClick={() => setClearKey(true)}
                style={{ alignSelf: 'flex-end' }}
              >
                Remove stored key
              </Button>
            </Group>
          </div>
          {testResult ? (
            <Alert icon={null} variant="light" color={testResult.ok ? 'teal' : 'red'}>
              <Text size="sm">{testResult.text}</Text>
            </Alert>
          ) : null}
        </SectionCard>
      ) : null}

      {/* ── Model Selection (owner #29: scan fills a table, checkbox enables) ── */}
      {show('models') ? (
        <SectionCard
          title="Model Selection"
          help="Scan the API for available models, then choose which ones users can access."
          actions={
            <Group gap="xs" align="flex-end">
              <Button variant="default" size="sm" loading={scanBusy} onClick={() => {
                setScanBusy(true); setScanMsg(null)
                api.scanModels(state!.llmApiUrl, state!.llmApiType).then(r => {
                  if (r.ok) {
                    setScanned(prev => Array.from(new Set([...prev, ...r.models])))
                    setScanMsg(`Scanned — ${r.models.length} model(s) found.`)
                  } else {
                    setScanMsg((r as any).message || 'Scan failed — check the API URL/type under API Connection.')
                  }
                  setScanBusy(false)
                })
              }}>
                Scan for Models
              </Button>
              <Button color="ollitex" loading={saving} size="sm" onClick={() => {
                void api.save({
                  allowedModels: (state!.allowedModels || []),
                }).catch(() => undefined)
              }}>
                Save
              </Button>
            </Group>
          }
        >
          {scanMsg ? (
            <Alert icon={null} variant="light" withBorder color={/found/.test(scanMsg) ? 'teal' : 'red'} style={{ minHeight: 0 }}>
              <Text size="sm">{scanMsg}</Text>
            </Alert>
          ) : null}
          {(() => {
            const allowed = new Set(state.allowedModels || [])
            const known = Array.from(new Set([...(state.knownModels || []), ...scanned, ...(state.allowedModels || [])]))
            if (known.length === 0) {
              return (
                <Text size="sm" c="dimmed">
                  No models known yet — run the scan, or save models manually via the provider docs.
                </Text>
              )
            }
            const toggle = (m: string) =>
              setState(s => (s ? { ...s, allowedModels: allowed.has(m) ? s.allowedModels.filter(x => x !== m) : [...(s.allowedModels || []), m] } : s))
            return (
              <div>
                <Table striped withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
                  <Table.Thead>
                    <Table.Tr>
                      <Table.Th>Model</Table.Th>
                      <Table.Th style={{ width: 100, textAlign: 'right' }}>Enabled</Table.Th>
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {known.map(m => (
                      <Table.Tr key={m}>
                        <Table.Td><Text size="sm" c="text" style={{ fontFamily: 'var(--mantine-font-family-mono)', whiteSpace: 'pre' }}>{m}</Text></Table.Td>
                        <Table.Td style={{ textAlign: 'right' }}>
                          <Checkbox
                            label={null}
                            aria-label={`Enable ${m}`}
                            checked={allowed.has(m)}
                            onChange={() => toggle(m)}
                            color="ollitex"
                          />
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
                <Group gap="sm" mt="xs">
                  <Button variant="subtle" size="xs" onClick={() => setState(s => (s ? { ...s, allowedModels: known } : s))}>
                    Select all
                  </Button>
                  <Button variant="subtle" size="xs" onClick={() => setState(s => (s ? { ...s, allowedModels: [] } : s))}>
                    Unselect all
                  </Button>
                  <Text size="xs" c="dimmed">{(state.allowedModels || []).length}/{known.length} enabled</Text>
                </Group>
              </div>
            )
          })()}
        </SectionCard>
      ) : null}

      {/* ── System Prompt (owner #30: counter + Reset + taller) ────────── */}
      {show('prompt') ? (
        <SectionCard
          title="Editor System Prompt"
          help="The base system prompt for in-editor AI (Ask AI / completion). Leave empty for the built-in default."
          actions={
            <Group gap="xs" align="flex-end">
              <Button variant="subtle" size="sm" onClick={() => setState(s => (s ? { ...s, systemPrompt: '' } : s))}>
                Reset to default
              </Button>
              <Button color="ollitex" loading={saving} size="sm"
                onClick={() => { void api.save({ systemPrompt: state!.systemPrompt }).catch(() => undefined) }}>
                Save
              </Button>
            </Group>
          }
        >
          <Textarea
            label="System prompt (optional)"
            value={state.systemPrompt || ''}
            onChange={e => setState(s => (s ? { ...s, systemPrompt: e.currentTarget.value } : s))}
            minRows={8}
            maxRows={16}
            placeholder="You are a helpful LaTeX assistant…"
          />
          <Text size="xs" c="dimmed" ta="right">
            {(state.systemPrompt || '').length}/4000 characters
          </Text>
        </SectionCard>
      ) : null}

      {/* ── AI Prompts ──────────────────────────────────────────────────── */}
      {show('prompts') ? (
        <SectionCard
          title="AI Prompts"
          help="Customize the prompts and token budgets behind each AI feature. Empty fields use the built-in defaults."
          actions={
            <Button color="ollitex" loading={saving} size="sm"
              onClick={() => {
                void api.save({
                  askAiSystemPrompt: state!.askAiSystemPrompt,
                  errorPrompt: state!.errorPrompt,
                  reviewSystemPrompt: state!.reviewSystemPrompt,
                  askAiActionPrompts: state!.askAiActionPrompts,
                  maxContextTokens: state!.maxContextTokens ?? undefined,
                  reviewMaxTokens: state!.reviewMaxTokens ?? undefined,
                }).catch(() => undefined)
              }}>
              Save
            </Button>
          }
        >
          <div>
            <Text size="xs" fw={700} c="dimmed" tt="uppercase" style={{ letterSpacing: '0.08em' }} mb={4}>
              Ask AI
            </Text>
            <Textarea
              label="Ask AI system prompt"
              value={state.askAiSystemPrompt || ''}
              onChange={e => setState(s => (s ? { ...s, askAiSystemPrompt: e.currentTarget.value } : s))}
              minRows={6}
              maxRows={14}
              placeholder="…(empty = built-in default)"
            />
            <Group justify="flex-end">
              <Button variant="subtle" size="xs" onClick={() => setState(s => (s ? { ...s, askAiSystemPrompt: '' } : s))}>
                Reset to default
              </Button>
            </Group>
          </div>
          {actionKeys.length > 0 ? (
            <Stack gap="sm">
              <Group justify="space-between" wrap="nowrap">
                <Text size="sm" fw={600}>Ask AI action prompts</Text>
                <Button variant="subtle" size="xs" onClick={() => setState(s => (s ? { ...s, askAiActionPrompts: {} } : s))}>
                  Reset to default
                </Button>
              </Group>  {actionKeys.map(k => (
                <Textarea
                  key={k}
                  label={k.replace(/_/g, ' ')}
                  value={state.askAiActionPrompts?.[k] || ''}
                  onChange={e =>
                    setState(s =>
                      s
                        ? { ...s, askAiActionPrompts: { ...(s.askAiActionPrompts || {}), [k]: e.currentTarget.value } }
                        : s
                    )
                  }
                  minRows={3}
                  maxRows={8}
                />
              ))}
            </Stack>
          ) : null}
          <div>
            <Text size="xs" fw={700} c="dimmed" tt="uppercase" style={{ letterSpacing: '0.08em' }} mb={4}>
              Review panel
            </Text>
            <Textarea
              label="Error / compile-fix prompt"
              value={state.errorPrompt || ''}
              onChange={e => setState(s => (s ? { ...s, errorPrompt: e.currentTarget.value } : s))}
              minRows={6}
              maxRows={14}
              placeholder="…(empty = built-in default)"
            />
            <Group justify="flex-end">
              <Button variant="subtle" size="xs" onClick={() => setState(s => (s ? { ...s, errorPrompt: '' } : s))}>
                Reset to default
              </Button>
            </Group>
            <Textarea
              mt="sm"
              label="Compliance review prompt"
              value={state.reviewSystemPrompt || ''}
              onChange={e => setState(s => (s ? { ...s, reviewSystemPrompt: e.currentTarget.value } : s))}
              minRows={6}
              maxRows={14}
            />
            <Group justify="flex-end">
              <Button variant="subtle" size="xs" onClick={() => setState(s => (s ? { ...s, reviewSystemPrompt: '' } : s))}>
                Reset to default
              </Button>
            </Group>
          </div>
          <Group gap="md" wrap="wrap">
            <div style={{ width: 200 }}>
              <TextInput
                label="Max context tokens (chat)"
                type="number"
                value={state.maxContextTokens ?? ''}
                onChange={e => setState(s => (s ? { ...s, maxContextTokens: e.currentTarget.value === '' ? null : Number(e.currentTarget.value) } : s))}
              />
            </div>
            <div style={{ width: 200 }}>
              <TextInput
                label="Review max tokens"
                type="number"
                value={state.reviewMaxTokens ?? ''}
                onChange={e => setState(s => (s ? { ...s, reviewMaxTokens: e.currentTarget.value === '' ? null : Number(e.currentTarget.value) } : s))}
              />
            </div>
          </Group>
        </SectionCard>
      ) : null}

      {/* ── Usage ───────────────────────────────────────────────────────── */}
      {show('usage') ? (
        <SectionCard title="Usage" help="LLM usage across the instance for the last 30 days.">
          <LlmUsagePanel scope="admin" />
        </SectionCard>
      ) : null}
    </Stack>
  )
}
