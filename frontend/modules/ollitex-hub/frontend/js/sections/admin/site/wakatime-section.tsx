// /hub → Site settings → Integrations → WakaTime (owner request G,
// 2026-10-09: "add WakaTime settings; self-hosted wakapi is the DEFAULT
// WakaTime server; users get wakapi accounts auto-created from their
// OlliTeX email").
//
// Wire:
//   - enable + server URL persist as the `wakatime` site section
//     (standard settings store, PUT /admin/site-settings/wakatime);
//   - "Check connection" → POST /admin/wakatime/check (Go wakatime
//     feature: GET <base>/ health probe);
//   - "Provision wakapi user" → POST /admin/wakatime/provision {email}
//     — the Go driver runs the wakapi v2 form flow (signup → login →
//     reset_apikey) with username = the email's local part, stores the
//     key encrypted for that user; from then on their editor heartbeats
//     relay through OlliTeX to their wakapi account (browser never talks
//     to wakapi; the key never leaves the server).
//
// Style: same SectionShell/Field idioms as the sibling sections
// (simple-sections.tsx).

import React, { useState } from 'react'
import { Alert, Button, Group, Text, Anchor, TextInput } from '@mantine/core'
import {
  Field,
  PageLoading,
  SectionShell,
  bool0,
  str0,
  useSyncValues,
  useSiteSettings,
} from './site-core'
import { postJSON } from '@/infrastructure/fetch-json'

function originDefault(): string {
  try {
    return window.location.origin + '/wakapi'
  } catch {
    return 'https://psintern.neuro.uni-bremen.de/wakapi'
  }
}

export function WakatimeSection() {
  const { data, error, flash, save, load } = useSiteSettings('wakatime')
  const [check, setCheck] = useState<{ busy: boolean; ok?: boolean; message?: string }>({ busy: false })
  const [provEmail, setProvEmail] = useState('')
  const [prov, setProv] = useState<{ busy: boolean; ok?: boolean; message?: string }>({ busy: false })
  const { v, up } = useSyncValues(data, d => ({
    enabled: bool0((d as any).enabled, false),
    serverUrl: str0((d as any).serverUrl) || originDefault(),
  }))

  if (!data && !error) return <PageLoading label="Loading WakaTime settings…" />
  if (error && !data) {
    return (
      <Group>
        <Text size="sm" c="red">WakaTime: {error}</Text>
        <Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor>
      </Group>
    )
  }

  return (
    <SectionShell
      title="WakaTime · coding activity"
      badge="wakatime"
      enabled={Boolean(v.enabled)}
      onEnabled={x => up({ enabled: x })}
      description="Editor activity tracking on the self-hosted wakapi (default: this instance's /wakapi route). Per-user wakapi accounts are auto-provisioned from the OlliTeX email (username = the email local part); users opt in on their My settings → WakaTime page. Heartbeats relay through OlliTeX — the browser never contacts wakapi, and the API key never leaves the server."
      footerNote="Users' own dashboard: /wakapi (e.g. https://this-host/wakapi). Feature enable also exposes the user opt-in."
      flash={flash}
      onSave={() => void save({
        enabled: Boolean(v.enabled),
        serverUrl: String(v.serverUrl || '').trim(),
      })}
    >
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field
          label="Wakapi server URL"
          value={String(v.serverUrl || '')}
          onChange={x => up({ serverUrl: x })}
          placeholder={originDefault()}
          hint="Self-hosted wakapi (default: this instance's /wakapi). Off-box wakapi/wakatime endpoints also work."
        />
      </Group>

      <Group gap="sm" wrap="wrap" align="flex-start">
        <Group gap="xs" wrap="wrap">
          <Button
            variant="default"
            size="sm"
            loading={check.busy}
            onClick={async () => {
              setCheck({ busy: true, ok: false, message: '' })
              const url = String(v.serverUrl || '').trim()
              try {
                const r: any = await postJSON('/admin/wakatime/check', { body: { serverUrl: url } })
                setCheck({ busy: false, ok: Boolean(r?.ok), message: String(r?.message || '') })
              } catch (e: any) {
                setCheck({
                  busy: false,
                  ok: false,
                  message: e?.data?.message || e?.message || 'request failed',
                })
              }
            }}
          >
            Check connection
          </Button>
          {check.busy ? (
            <Text size="sm" c="dimmed">Probing {v.serverUrl}…</Text>
          ) : check.message ? null : null}
        </Group>
        {check.message ? (
          <Text size="sm" c={check.ok ? 'teal' : 'red'}>{check.message}</Text>
        ) : null}
      </Group>

      {/* 2026-10-09 (owner item AE): (1) button aligned with the input row
          (was a misaligned sibling of the Field label+hint column); (2) the
          hint now states explicitly that manual provisioning is IN ADDITION
          TO the automatic on-first-heartbeat provisioning. */}
      <div style={{ width: '100%', minWidth: 260, marginTop: 8 }}>
        <Text size="xs" fw={600} mb={4}>
          Provision for user (email)
        </Text>
        <Group gap="xs" wrap="nowrap" align="center">
          <TextInput
            style={{ flex: 1, minWidth: 220 }}
            value={provEmail}
            onChange={x => setProvEmail(x.currentTarget.value)}
            placeholder="j.doe@uni-bremen.de"
          />
          <Button
            variant="default"
            size="sm"
            loading={prov.busy}
            onClick={async () => {
              const email = provEmail.trim()
              if (!email) { setProv({ busy: false, ok: false, message: 'enter the user email first' }); return }
              setProv({ busy: true, ok: false, message: '' })
              try {
                const r: any = await postJSON('/admin/wakatime/provision', { body: { email } })
                setProv({ busy: false, ok: Boolean(r?.ok), message: String(r?.message || '') })
                if (r?.ok) setProvEmail('')
              } catch (e: any) {
                setProv({
                  busy: false,
                  ok: false,
                  message: e?.data?.message || e?.message || 'request failed',
                })
              }
            }}
          >
            Provision wakapi user
          </Button>
        </Group>
        <Text size="xs" c="dimmed" mt={4}>
          In addition to the automatic provisioning — opted-in users get their
          wakapi account created automatically on their first heartbeat — this
          button pre-creates (or re-creates) the account for a specific email:
          username = the local part, API key stored for that user. Use it to
          onboard before the first heartbeat or to regenerate the key without
          waiting for their first edit.
        </Text>
      </div>
      {prov.message ? (
        prov.ok
          ? <Text size="sm" c="teal" mt="xs">{prov.message}</Text>
          : <Alert icon={undefined} color="red" variant="light" mt="xs" style={{ width: '100%', minWidth: 240 }}>
              <Text size="sm">{prov.message}</Text>
            </Alert>
      ) : null}
    </SectionShell>
  )
}
