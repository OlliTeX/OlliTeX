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
  maxContextTokens?: number
  reviewMaxTokens?: number
  llmApiUrlFromEnv?: boolean
  llmApiTypeFromEnv?: boolean
}

export default function AdminLlmSection() {
  const [state, setState] = useState<LlmAdmin | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [apiKey, setApiKey] = useState('')
  const [clearKey, setClearKey] = useState(false)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ ok: boolean; text: string } | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      const data = await getJSON('/admin/llm/settings')
      const s = data || {}
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
        maxContextTokens: s.maxContextTokens ?? null as any,
        reviewMaxTokens: s.reviewMaxTokens ?? null as any,
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

  const modelsText = (state?.allowedModels || []).join('\n')

  const save = async () => {
    if (!state) return
    setSaving(true)
    setTestResult(null)
    try {
      const allowed = modelsText
        .split(/[\n,]/)
        .map(s => s.trim())
        .filter(Boolean)
      await postJSON('/admin/llm/settings', {
        body: {
          systemPrompt: state.systemPrompt || '',
          llmApiUrl: state.llmApiUrl || '',
          llmApiType: state.llmApiType,
          llmApiKey: apiKey.trim() || undefined,
          clearLlmApiKey: clearKey,
          allowedModels: allowed,
          knownModels: state.knownModels || [],
          chatEnabled: state.chatEnabled,
          completionEnabled: state.completionEnabled,
          reviewEnabled: state.reviewEnabled,
          llmDisabledByAdmin: state.llmDisabledByAdmin,
          languageToolUrl: state.languageToolUrl || '',
        },
      })
      notifications.show({ message: 'LLM instance settings saved.', color: 'teal' })
      setApiKey('')
      setClearKey(false)
      await load()
    } catch (err: any) {
      notifications.show({
        message: (err?.data?.message as string) || 'Could not save LLM settings.',
        color: 'red',
      })
    } finally {
      setSaving(false)
    }
  }

  const test = async () => {
    if (!state) return
    setTesting(true)
    setTestResult(null)
    try {
      const res = await postJSON('/admin/llm/settings/check', {
        body: {
          llmApiUrl: state.llmApiUrl || '',
          llmApiKey: apiKey.trim() || undefined,
        },
      })
      const ok = res?.ok !== false && res?.status !== 'error'
      setTestResult({
        ok,
        text: (res?.message || res?.model || res?.status || (ok ? 'Connection OK' : 'Check failed')) as string,
      })
    } catch (err: any) {
      setTestResult({ ok: false, text: (err?.data?.message as string) || 'Connection check failed.' })
    } finally {
      setTesting(false)
    }
  }

  if (!state && !error) return <PageLoading label="Loading LLM instance settings…" />
  if (error)
    return <PageError label="Couldn’t load LLM settings" detail={error} onRetry={() => void load()} />
  if (!state) return null

  return (
    <Stack gap="md">
      <Alert icon={null} variant="light" color="gray" withBorder radius="md">
        <Text size="sm">
          Instance-wide LLM endpoint. User bring-your-own providers (managed on the workspace
          side) also flow through this instance configuration when the feature is enabled.
        </Text>
      </Alert>

      <Card withBorder paddings="lg" radius="lg">
        <Stack gap="md">
          <Group justify="space-between" wrap="nowrap" gap="sm">
            <div>
              <Text fw={700}>Enable AI features instance-wide</Text>
              <Text size="sm" c="dimmed" mt={4}>
                Turning this off force-disables chat, completion, and review for every user.
              </Text>
            </div>
            <Switch
              checked={!state.llmDisabledByAdmin}
              onChange={v => setState(s => (s ? { ...s, llmDisabledByAdmin: !v } : s))}
              color="ollitex"
            />
          </Group>

          <Group gap="md" wrap="wrap">
            <div style={{ minWidth: 220 }}>
              <Text size="sm" fw={600} mb={6}>
                API type
              </Text>
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
              <Text size="sm" fw={600} mb={6}>
                API base URL
              </Text>
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

          <div>
            <Text size="sm" fw={600} mb={6}>
              Allowed models{' '}
              <Text span size="xs" c="dimmed" fw={400}>
                (one per line — users can only pick from this list)
              </Text>
            </Text>
            <Textarea
              value={modelsText}
              onChange={e =>
                setState(s =>
                  s
                    ? {
                        ...s,
                        allowedModels: e.currentTarget.value
                          .split(/[\n,]/)
                          .map(x => x.trim())
                          .filter(Boolean),
                      }
                    : s
                )
              }
              minRows={3}
              maxRows={8}
              placeholder={'gpt-4o-mini\ngemini-2.0-flash'}
            />
          </div>

          <div>
            <Text size="sm" fw={600} mb={6}>
              Editor system prompt (optional)
            </Text>
            <Textarea
              value={state.systemPrompt || ''}
              onChange={e => setState(s => (s ? { ...s, systemPrompt: e.currentTarget.value } : s))}
              minRows={2}
              maxRows={6}
              placeholder="You are a helpful LaTeX assistant…"
            />
          </div>

          <Group gap="lg" wrap="wrap">
            <Switch
              checked={state.chatEnabled}
              onChange={v => setState(s => (s ? { ...s, chatEnabled: v } : s))}
              label="Chat (Ask AI)"
              color="ollitex"
            />
            <Switch
              checked={state.completionEnabled}
              onChange={v => setState(s => (s ? { ...s, completionEnabled: v } : s))}
              label="Inline completion"
              color="ollitex"
            />
            <Switch
              checked={state.reviewEnabled}
              onChange={v => setState(s => (s ? { ...s, reviewEnabled: v } : s))}
              label="Review panel"
              color="ollitex"
            />
          </Group>

          <div style={{ maxWidth: 420 }}>
            <Text size="sm" fw={600} mb={6}>
              LanguageTool instance URL (optional)
            </Text>
            <TextInput
              value={state.languageToolUrl || ''}
              onChange={e => setState(s => (s ? { ...s, languageToolUrl: e.currentTarget.value } : s))}
              placeholder="https://languagetool.example.org"
            />
          </div>

          <Group gap="xs" wrap="wrap" mt="sm">
            <Button color="ollitex" loading={saving} onClick={() => void save()}>
              Save settings
            </Button>
            <Button variant="default" loading={testing} onClick={() => void test()}>
              Test connection
            </Button>
          </Group>
          {testResult ? (
            <Alert icon={null} variant="light" color={testResult.ok ? 'teal' : 'red'}>
              <Text size="sm">{testResult.text}</Text>
            </Alert>
          ) : null}
        </Stack>
      </Card>
    </Stack>
  )
}
