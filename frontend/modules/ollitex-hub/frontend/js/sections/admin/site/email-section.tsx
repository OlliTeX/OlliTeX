// /hub → Site settings → Services: E-mail (owner #32).
// Native Mantine remake of the legacy EmailTab — same fields, same section
// ("email"), incl. SMTP/SES driver switch + one-off test e-mail endpoint.

import React, { useState } from 'react'
import { Button, Group, Text } from '@mantine/core'
import { postJSON } from '@/infrastructure/fetch-json'
import {
  Field,
  NativeSelectField,
  PageLoading,
  SectionShell,
  SectionTitle,
  SecretField,
  SwitchRow,
  bool0,
  num0,
  str0,
  useSyncValues,
  useSiteSettings,
} from './site-core'

export function EmailSection() {
  const { data, error, flash, save, load } = useSiteSettings('email')
  const { v, up } = useSyncValues(data, d => ({
    driver: str0((d as any).driver, 'smtp'),
    fromAddress: str0((d as any).fromAddress),
    replyTo: str0((d as any).replyTo),
    host: str0((d as any).host),
    port: String(num0((d as any).port, 587)),
    secure: bool0((d as any).secure),
    ignoreTLS: bool0((d as any).ignoreTLS),
    name: str0((d as any).name),
    user: str0((d as any).user),
    pass: '',
    tlsRejectUnauth: bool0((d as any).tlsRejectUnauth, true),
    accessKeyId: str0((d as any).accessKeyId),
    sesSecret: '',
    sesRegion: str0((d as any).sesRegion),
    skipConfirmation: bool0((d as any).skipConfirmation),
    adminEmail: str0((d as any).adminEmail),
    customFooter: str0((d as any).customFooter),
  }))
  const passSet = Boolean((data as any)?.passSet)
  const sesSecretSet = Boolean((data as any)?.sesSecretSet)
  const [testTo, setTestTo] = useState('')
  const [testBusy, setTestBusy] = useState(false)
  const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(null)

  if (!data && !error) return <PageLoading label="Loading e-mail settings…" />
  if (error && !data) {
    return (
      <Group justify="space-between" gap="xs" wrap="wrap">
        <Text size="sm" c="red">Couldn’t load e-mail settings — {error}</Text>
        <Button size="xs" variant="light" onClick={() => void load()}>Retry</Button>
      </Group>
    )
  }
  const driver = String(v.driver || 'smtp')
  const runTest = async () => {
    const to = testTo.trim()
    if (!to || testBusy) return
    setTestBusy(true)
    setTestResult(null)
    try {
      await postJSON('/admin/site-settings/email/test', { body: { to } })
      setTestResult({ ok: true, msg: `Test e-mail delivered to ${to}.` })
    } catch (err: any) {
      setTestResult({ ok: false, msg: (err?.data?.message as string) || 'Test e-mail failed.' })
    } finally {
      setTestBusy(false)
    }
  }
  return (
    <SectionShell
      title="E-mail"
      badge="SMTP / SES"
      description="Outbound mail delivery for sign-ups, invites and notifications."
      footerNote="Changes apply on the next container cycle."
      flash={flash}
      onSave={() => void save({
        driver,
        fromAddress: String(v.fromAddress || ''),
        replyTo: String(v.replyTo || ''),
        host: String(v.host || ''),
        port: num0(v.port, 587),
        secure: driver === 'smtp' ? Boolean(v.secure) : false,
        ignoreTLS: driver === 'smtp' ? Boolean(v.ignoreTLS) : false,
        name: String(v.name || ''),
        user: String(v.user || ''),
        pass: String(v.pass || ''),
        tlsRejectUnauth: Boolean(v.tlsRejectUnauth),
        accessKeyId: driver === 'ses' ? String(v.accessKeyId || '') : '',
        sesSecret: driver === 'ses' ? String(v.sesSecret || '') : '',
        sesRegion: driver === 'ses' ? String(v.sesRegion || '') : '',
        skipConfirmation: Boolean(v.skipConfirmation),
        adminEmail: String(v.adminEmail || ''),
        customFooter: String(v.customFooter || ''),
      })}
    >
      <SwitchRow
        label="Skip sign-up confirmation e-mails"
        checked={Boolean(v.skipConfirmation)}
        onChange={x => up({ skipConfirmation: x })}
        hint="Users can log in immediately after account creation."
      />
      <SectionTitle top>General</SectionTitle>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="From address" required value={String(v.fromAddress || '')} onChange={x => up({ fromAddress: x })} placeholder="noreply@example.com" hint="Address shown as the sender." />
        <Field label="Reply-to" value={String(v.replyTo || '')} onChange={x => up({ replyTo: x })} placeholder="support@example.com" />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Admin contact" value={String(v.adminEmail || '')} onChange={x => up({ adminEmail: x })} placeholder="admin@example.com" hint="Used in outgoing mail footers." />
        <Field label="Custom footer" value={String(v.customFooter || '')} onChange={x => up({ customFooter: x })} placeholder="© Example University" />
      </Group>
      <SectionTitle top>Delivery driver</SectionTitle>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <NativeSelectField label="Driver" value={driver} onChange={x => up({ driver: x })} options={[
          { value: 'smtp', label: 'SMTP' },
          { value: 'ses', label: 'AWS SES' },
        ]} />
      </Group>
      {driver === 'smtp' ? (
        <>
          <SectionTitle top>SMTP configuration</SectionTitle>
          <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
            <Field label="Host" required value={String(v.host || '')} onChange={x => up({ host: x })} placeholder="smtp.example.com" />
            <Field label="Port" value={String(v.port || '')} onChange={x => up({ port: x.replace(/[^\d]/g, '') })} placeholder="587" width="33%" />
            <Field label="SMTP name" value={String(v.name || '')} onChange={x => up({ name: x })} placeholder="Your server name" width="33%" />
          </Group>
          <Group wrap="wrap" gap="sm" mb="xs">
            <SwitchRow label="Secure (TLS)" checked={Boolean(v.secure)} onChange={x => up({ secure: x })} />
            <SwitchRow label="Ignore TLS verification" checked={Boolean(v.ignoreTLS)} onChange={x => up({ ignoreTLS: x })} />
            <SwitchRow label="Reject unauthenticated TLS" checked={Boolean(v.tlsRejectUnauth)} onChange={x => up({ tlsRejectUnauth: x })} />
          </Group>
          <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
            <Field label="Username" value={String(v.user || '')} onChange={x => up({ user: x })} />
            <SecretField label="Password" set={Boolean(passSet)} value={String(v.pass || '')} onChange={x => up({ pass: x })} hint="Leave empty to keep the stored password." />
          </Group>
        </>
      ) : (
        <>
          <SectionTitle top>AWS SES configuration</SectionTitle>
          <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
            <Field label="Access key ID" required value={String(v.accessKeyId || '')} onChange={x => up({ accessKeyId: x })} />
            <SecretField label="Secret access key" set={Boolean(sesSecretSet)} value={String(v.sesSecret || '')} onChange={x => up({ sesSecret: x })} hint="Leave empty to keep the stored key." />
          </Group>
          <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
            <Field label="Region" value={String(v.sesRegion || '')} onChange={x => up({ sesRegion: x })} placeholder="eu-west-1" />
          </Group>
        </>
      )}
      <SectionTitle top>Send a test e-mail</SectionTitle>
      <Group wrap="wrap" gap="sm" align="flex-end" mb="xs">
        <div style={{ width: 320, maxWidth: '100%' }}>
          <Field label="Recipient" value={testTo} onChange={setTestTo} placeholder="admin@example.com" hint="Sent through the stored configuration (after saving)." />
        </div>
        <Button color="ollitex" loading={testBusy} disabled={!testTo.trim()} onClick={() => void runTest()}>
          Send test
        </Button>
      </Group>
      {testResult ? (
        <Text size="sm" c={testResult.ok ? 'teal' : 'red'}>
          {testResult.msg}
        </Text>
      ) : null}
    </SectionShell>
  )
}
