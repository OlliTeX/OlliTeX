import React, { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Group,
  NativeSelect,
  Stack,
  Switch,
  Text,
  Textarea,
  TextInput,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { getJSON, postJSON } from '@/infrastructure/fetch-json'
import { PageError, PageLoading } from '../../shared/page-state'
import Icon from '../../shared/icons'
// overleaf-lab: the shared usage meter (admin scope → /admin/llm/usage)
import LLMUsageMeter from '../../../../../llm/frontend/js/components/llm-usage-meter'

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
      const s = (await getJSON('/admin/llm/settings')) || {}
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
      notifications.show({ message: 'LLM instance settings saved.', color: 'teal' })
      await load()
    } catch (err: any) {
      notifications.show({
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

  return { state, setState, error, saving, load, save, checkConnection, checkLanguageTool }
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

  useEffect(() => {
    if (state) setActionKeys(Object.keys(state.askAiActionPrompts || {}))
  }, [state])

  if (!state && !error) return <PageLoading label="Loading LLM instance settings…" />
  if (error)
    return <PageError label="Couldn’t load LLM settings" detail={error} onRetry={() => void api.load()} />
  if (!state) return null

  const show = (s: Section) => section === 'all' || section === s

  const modelsText = (state.allowedModels || []).join('\n')

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
          help="Turning the master switch off force-disables chat, completion, and review for every user."
          actions={
            <>
              <Button color="ollitex" loading={saving} size="sm"
                onClick={() => {
                  void api.save({
                    llmDisabledByAdmin: state!.llmDisabledByAdmin,
                    chatEnabled: state!.chatEnabled,
                    completionEnabled: state!.completionEnabled,
                    reviewEnabled: state!.reviewEnabled,
                    languageToolDisabledByAdmin: state!.languageToolDisabledByAdmin,
                    languageToolUrl: state!.languageToolUrl,
                  }).catch(() => undefined)
                }}>
                Save features
              </Button>
            </>
          }
        >
          <Group justify="space-between" wrap="nowrap" gap="sm">
            <Text size="sm" fw={600}>Master switch</Text>
            <Switch
              checked={!state.llmDisabledByAdmin}
              onChange={v => setState(s => (s ? { ...s, llmDisabledByAdmin: !v } : s))}
              color="ollitex"
              label={state.llmDisabledByAdmin ? 'AI disabled' : 'AI enabled'}
            />
          </Group>
          <Group gap="lg" wrap="wrap">
            <Switch checked={state.chatEnabled} onChange={v => setState(s => (s ? { ...s, chatEnabled: v } : s))} label="Chat (Ask AI)" color="ollitex" />
            <Switch checked={state.completionEnabled} onChange={v => setState(s => (s ? { ...s, completionEnabled: v } : s))} label="Inline completion" color="ollitex" />
            <Switch checked={state.reviewEnabled} onChange={v => setState(s => (s ? { ...s, reviewEnabled: v } : s))} label="Review panel" color="ollitex" />
          </Group>
          <Group justify="space-between" wrap="nowrap" gap="sm">
            <Text size="sm" fw={600}>
              LanguageTool (grammar checking)
              <Text span size="xs" c="dimmed" fw={400}> force-off for the whole instance</Text>
            </Text>
            <Switch
              checked={!state.languageToolDisabledByAdmin}
              onChange={v => setState(s => (s ? { ...s, languageToolDisabledByAdmin: !v } : s))}
              color="ollitex"
            />
          </Group>
          <Group gap="xs" wrap="wrap">
            <div style={{ flex: 1, minWidth: 260 }}>
              <TextInput
                label="LanguageTool instance URL (optional)"
                placeholder="https://languagetool.example.org"
                value={state.languageToolUrl || ''}
                onChange={e => setState(s => (s ? { ...s, languageToolUrl: e.currentTarget.value } : s))}
              />
            </div>
            <Button variant="default" size="sm" loading={ltBusy} onClick={() => {
              setLtBusy(true); setLtResult(null)
              api.checkLanguageTool(state!.languageToolUrl).then(r => { setLtResult(r); setLtBusy(false) })
            }}>
              Check LT
            </Button>
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

      {/* ── Model Selection ─────────────────────────────────────────────── */}
      {show('models') ? (
        <SectionCard
          title="Model Selection"
          help="Users can only pick from the models listed here (one per line). Keep this list current for the configured API type."
          actions={
            <Button color="ollitex" loading={saving} size="sm"
              onClick={() => {
                void api.save({
                  allowedModels: modelsText.split(/[\n,]/).map(s => s.trim()).filter(Boolean),
                }).catch(() => undefined)
              }}>
              Save models
            </Button>
          }
        >
          <Textarea
            label="Allowed models (one per line)"
            value={modelsText}
            onChange={e => setState(s => (s ? { ...s, allowedModels: e.currentTarget.value.split(/[\n,]/).map(x => x.trim()).filter(Boolean) } : s))}
            minRows={3}
            maxRows={8}
            placeholder={'gpt-4o-mini\ngemini-2.0-flash'}
          />
          {state.knownModels?.length ? (
            <Text size="xs" c="dimmed">
              Known models from the provider: {state.knownModels.join(', ')}
            </Text>
          ) : null}
        </SectionCard>
      ) : null}

      {/* ── System Prompt ───────────────────────────────────────────────── */}
      {show('prompt') ? (
        <SectionCard
          title="Editor System Prompt"
          help="The base system prompt for in-editor AI (Ask AI / completion). Leave empty for the built-in default."
          actions={
            <Button color="ollitex" loading={saving} size="sm"
              onClick={() => { void api.save({ systemPrompt: state!.systemPrompt }).catch(() => undefined) }}>
              Save prompt
            </Button>
          }
        >
          <Textarea
            label="System prompt (optional)"
            value={state.systemPrompt || ''}
            onChange={e => setState(s => (s ? { ...s, systemPrompt: e.currentTarget.value } : s))}
            minRows={3}
            maxRows={8}
            placeholder="You are a helpful LaTeX assistant…"
          />
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
              Save prompts
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
              minRows={2}
              maxRows={5}
              placeholder="…(empty = built-in default)"
            />
          </div>
          {actionKeys.length > 0 ? (
            <Stack gap="sm">
              <Text size="sm" fw={600}>Ask AI action prompts</Text>
              {actionKeys.map(k => (
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
                  minRows={1}
                  maxRows={3}
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
              minRows={2}
              maxRows={5}
              placeholder="…(empty = built-in default)"
            />
            <Textarea
              mt="sm"
              label="Compliance review prompt"
              value={state.reviewSystemPrompt || ''}
              onChange={e => setState(s => (s ? { ...s, reviewSystemPrompt: e.currentTarget.value } : s))}
              minRows={2}
              maxRows={5}
            />
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
          <LLMUsageMeter scope="admin" />
        </SectionCard>
      ) : null}
    </Stack>
  )
}
