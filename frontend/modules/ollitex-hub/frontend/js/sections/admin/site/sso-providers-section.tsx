// /hub → Site settings → Integrations: SSO providers (owner #46 / TODO-4f5bdfa4).
// Multi-provider SSO manager per reference checkout `/sso` (fe4ceb6): one
// ordered provider list (OIDC + SAML), per-provider enable + endpoints +
// keys, add/remove/reorder, per-provider connection test, LDAP
// searchAttributes, local-login safety valve note. Talks to the
// /admin/sso/* endpoints (go/services/web/features/sso/admin.go — the
// reference API surface: config GET/POST, provider add/delete/reorder,
// test provider/ldap).
//
// Secrets: GET returns the sentinel "••••••••" for stored secrets;
// POSTing the sentinel (or empty) keeps the stored value (Go
// sanitizeConfigDoc). So the UI keeps masked values in state verbatim and
// never blanks them.

import React, { useCallback, useEffect, useState } from 'react'
import { Anchor, Button, Group, Stack, Text } from '@mantine/core'

import {
  Area,
  Field,
  SectionShell,
  SecretField,
  SwitchRow,
} from './site-core'

const MASK = '••••••••'

type Provider = Record<string, any> & { id: string; type: 'oidc' | 'saml' }
type SsoDoc = { ldap: Record<string, any> | null; providers: Provider[]; spMetadata: any; masked: Record<string, string[]> }
const LDAP_SECRET_KEYS = ['bindCredentials']
const PROVIDER_SECRET_KEYS = ['clientSecret', 'privateKey', 'decryptionPvk', 'idpCert', 'decryptionCert', 'publicCert']

function keysWhoseValue(v: Record<string, any>, allowed: string[]): string[] {
  return allowed.filter(k => String(v[k] || '') === MASK)
}

const IDLE = { busy: false, saved: false, message: '' }
type Flash = typeof IDLE
type TestOut = { ok: boolean; text: string }

function csrfHeaders (): Record<string, string> {
  const m = document.querySelector('meta[name="ol-csrfToken"]') as HTMLMetaElement | null
  const token = m ? m.content : ''
  return token ? { 'X-Csrf-Token': token } : {}
}

async function ssoFetch (path: string, opts: { method?: string; body?: unknown } = {}): Promise<any> {
  const res = await fetch(path, {
    method: opts.method || 'GET',
    credentials: 'same-origin',
    headers: { ...(opts.body === undefined ? {} : { 'Content-Type': 'application/json' }), ...csrfHeaders() },
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
  })
  const text = await res.text()
  let data: any = null
  try { data = text ? JSON.parse(text) : null } catch { data = text }
  if (!res.ok) {
    const msg = (data && (data.error || data.message)) || data || `${res.status}`
    throw new Error(typeof msg === 'string' ? msg : JSON.stringify(msg))
  }
  return data
}

function str (v: unknown): string {
  if (v === undefined || v === null) return ''
  const s = String(v)
  return s === MASK ? '' : s
}
function bool (v: unknown): boolean {
  return v === true
}

export function SsoProvidersSection () {
  const [doc, setDoc] = useState<SsoDoc | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [flash, setFlash] = useState<Flash>(IDLE)
  const [providerTest, setProviderTest] = useState<Record<string, TestOut>>({})
  const [ldapTest, setLdapTest] = useState<TestOut | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      const cfg = await ssoFetch('/admin/sso/config')
      const providers: Provider[] = Array.isArray(cfg?.providers) ? cfg.providers : []
      providers.sort((a, b) => (Number(a.order) || 0) - (Number(b.order) || 0))
      const masked: Record<string, string[]> = {}
      for (const pv of providers) {
        masked[pv.id] = keysWhoseValue(pv, PROVIDER_SECRET_KEYS)
        // strip the sentinel from display state; the save transform puts it
        // back where the user left the field empty (Go restores only MASK)
        for (const k of masked[pv.id]) pv[k] = ''
      }
      const ldap = cfg?.ldap || null
      const ldapMasked: string[] = ldap ? keysWhoseValue(ldap, LDAP_SECRET_KEYS) : []
      if (ldap) for (const k of ldapMasked) ldap[k] = ''
      masked.__ldap__ = ldapMasked
      setDoc({ ldap, providers, spMetadata: cfg?.spMetadata || null, masked })
    } catch (e: any) {
      setError(e?.message || String(e))
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const save = useCallback(async () => {
    if (!doc) return
    setFlash({ busy: true, saved: false, message: '' })
    try {
      // Masked-secret round trip: Go's sanitizeConfigDoc restores stored
      // secrets ONLY for the sentinel; "" erases them. So: field still empty
      // and was masked on load ⇒ send the sentinel back; user typed a new
      // value ⇒ send it (replaces the stored one).
      const d = doc as SsoDoc
      const out = {
        ldap: d.ldap && { ...d.ldap },
        providers: d.providers.map(pv => {
          const masked = d.masked[pv.id] || []
          const cp: Record<string, any> = { ...pv }
          for (const k of masked) if (cp[k] === '') cp[k] = MASK
          return cp
        }),
        spMetadata: d.spMetadata || null,
      } as Record<string, any>
      if (out.ldap) {
        for (const k of d.masked.__ldap__ || []) if (out.ldap[k] === '') out.ldap[k] = MASK
      }
      await ssoFetch('/admin/sso/config', { method: 'POST', body: out })
      await load()
      setFlash({ busy: false, saved: true, message: 'Saved.' })
    } catch (e: any) {
      setFlash({ busy: false, saved: false, message: `Save failed: ${e?.message || e}` })
    }
  }, [doc, load])

  const addProvider = useCallback(async (type: 'oidc' | 'saml') => {
    setFlash({ busy: true, saved: false, message: `Adding ${type.toUpperCase()} provider…` })
    try {
      await ssoFetch('/admin/sso/provider', { method: 'POST', body: { type } })
      await load()
      setFlash({ busy: false, saved: true, message: `${type.toUpperCase()} provider added (disabled by default). Fill it in, then Save.` })
    } catch (e: any) {
      setFlash({ busy: false, saved: false, message: `Add failed: ${e?.message || e}` })
    }
  }, [load])

  const removeProvider = useCallback(async (id: string) => {
    if (!window.confirm(`Delete SSO provider ${id}? This cannot be undone.`)) return
    setFlash({ busy: true, saved: false, message: 'Deleting…' })
    try {
      await ssoFetch(`/admin/sso/provider/${encodeURIComponent(id)}`, { method: 'DELETE' })
      await load()
      setFlash({ busy: false, saved: true, message: 'Provider deleted.' })
    } catch (e: any) {
      setFlash({ busy: false, saved: false, message: `Delete failed: ${e?.message || e}` })
    }
  }, [load])

  const moveProvider = useCallback(async (idx: number, dir: -1 | 1) => {
    if (!doc) return
    const to = idx + dir
    if (to < 0 || to >= doc.providers.length) return
    const next = [...doc.providers]
    ;[next[idx], next[to]] = [next[to], next[idx]]
    setDoc({ ...doc, providers: next })
    const body = { providers: next.map((p, i) => ({ id: p.id, order: i + 1 })) }
    try {
      await ssoFetch('/admin/sso/providers/reorder', { method: 'POST', body })
      setFlash({ busy: false, saved: true, message: 'Order saved.' })
    } catch (e: any) {
      setFlash({ busy: false, saved: false, message: `Reorder failed: ${e?.message || e}` })
      await load()
    }
  }, [doc, load])

  const testProvider = useCallback(async (id: string) => {
    setProviderTest(m => ({ ...m, [id]: { ok: false, text: 'Testing…' } }))
    try {
      const out = await ssoFetch(`/admin/sso/test/provider/${encodeURIComponent(id)}`, { method: 'POST'})
      const detail = (out?.details && typeof out.details === 'object'
        ? Object.entries(out.details).map(([k, v]) => `${k}=${v}`).join(' ')
        : '')
      setProviderTest(m => ({ ...m, [id]: { ok: !!out?.success, text: `${out?.message || 'done'}${detail ? ' — ' + detail : ''}` }}))
    } catch (e: any) {
      setProviderTest(m => ({ ...m, [id]: { ok: false, text: e?.message || String(e) }}))
    }
  }, [])

  const testLdap = useCallback(async () => {
    setLdapTest({ ok: false, text: 'Testing…' })
    try {
      const out = await ssoFetch('/admin/sso/test/ldap', { method: 'POST' })
      setLdapTest({ ok: !!out?.success, text: out?.message || 'done' })
    } catch (e: any) {
      setLdapTest({ ok: false, text: e?.message || String(e) })
    }
  }, [])

  if (!doc && !error) {
    return (
      <SectionShell
        title="SSO providers (multi)"
        badge="sso"
        description="Loading…"
        flash={{ saving: true, saved: false, error: null }}
        onSave={() => undefined}
      >
        <Text size="sm" c="dimmed">Loading SSO provider configuration…</Text>
      </SectionShell>
    )
  }
  if (error) {
    return (
      <SectionShell
        title="SSO providers (multi)"
        badge="sso"
        description="Failed to load — retry."
        flash={{ saving: false, saved: false, error }}
        onSave={() => void load()}
      >
        <Text size="sm" c="red">{error}</Text>
        <Button size="xs" onClick={() => void load()}>Retry</Button>
      </SectionShell>
    )
  }

  const providers = (doc as SsoDoc).providers
  const ldap = (doc as SsoDoc).ldap || {}
  const upP = (i: number, key: string, val: unknown) => {
    const next = providers.map((p, j) => (j === i ? { ...p, [key]: val } : p))
    setDoc({ ...(doc as SsoDoc), providers: next })
  }
  const upL = (key: string, val: unknown) => {
    const l = { ...(doc as SsoDoc).ldap, [key]: val }
    setDoc({ ...(doc as SsoDoc), ldap: l })
  }
  const maskedOf = (pid: string, key: string): boolean => {
    const d = doc as SsoDoc
    const pv = d.providers.find(x => x.id === pid) || ({} as Provider)
    return (d.masked?.[pid] || []).includes(key) && String(pv[key] ?? '') === ''
  }
  const ldapMasked = (key: string): boolean => {
    const d = doc as SsoDoc
    return (d.masked?.__ldap__ || []).includes(key) && String((d.ldap || {})[key] ?? '') === ''
  }

  return (
    <SectionShell
      title="SSO providers (multi)"
      badge="sso"
      enabled={providers.length > 0 || bool((doc as SsoDoc).ldap?.enabled)}
      description="One ordered list of identity providers (OIDC + SAML) + LDAP directory login, per the reference sso (fe4ceb6) model. Enabled providers render on /login in order. Local username+password login (the emergency safety valve) stays active at all times — even with every provider disabled."
      footerNote="Changes apply at the next login attempt (no container restart required). Secrets are masked: leave a secret field empty to keep the stored value."
      flash={{ saving: flash.busy, saved: flash.saved, error: flash.saved ? null : flash.message }}
      onSave={() => void save()}
    >
      <Text size="sm" c="dimmed" mb={16}>
        One ordered list of Identity-Provider logins (OIDC + SAML). Everything shown on the
        login page comes from this list: <b>enabled</b> providers render one button each, in
        order. Per-provider endpoints, keys and timeouts below. <br />
        <b>Safety valve:</b> local username+password login on <Anchor href="/login">/login</Anchor> is
        always active — even with every provider disabled, local accounts can always sign in.
      </Text>

      <Group mb={20} gap={8}>
        <Button size="xs" variant="light" loading={flash.busy} onClick={() => void addProvider('oidc')}>+ Add OIDC provider</Button>
        <Button size="xs" variant="light" loading={flash.busy} onClick={() => void addProvider('saml')}>+ Add SAML provider</Button>
        {providers.length === 0 && <Text size="xs" c="dimmed">No database providers yet — the login button(s) come from environment configuration (OVERLEAF_OIDC_*/SAML*).</Text>}
      </Group>

      <Stack gap={16}>
        {providers.map((p, i) => (
          <div key={p.id || i} style={{ border: '1px solid var(--mantine-color-gray-3)', borderRadius: 8, padding: 16, opacity: p.enabled ? 1 : 0.7 }}>
            <Group mb={12} gap={10} justify="space-between" wrap="wrap">
              <Group gap={10}>
                <span style={{ fontSize: 13, fontWeight: 700, padding: '2px 8px', borderRadius: 6, background: 'var(--mantine-color-gray-1)', color: 'var(--mantine-color-gray-7)' }}>{(p.type || '').toUpperCase()}</span>
                <span style={{ fontSize: 13, fontWeight: 600 }}>{str(p.name) || p.id}</span>
                <span style={{ fontSize: 12, color: p.enabled ? 'var(--mantine-color-teal-6)' : 'var(--mantine-color-gray-5)' }}>{p.enabled ? 'ON' : 'OFF'}</span>
              </Group>
              <Group gap={6}>
                <Button size="xs" variant="subtle" disabled={i === 0} onClick={() => void moveProvider(i, -1)}>↑</Button>
                <Button size="xs" variant="subtle" disabled={i === providers.length - 1} onClick={() => void moveProvider(i, 1)}>↓</Button>
                <Button size="xs" variant="subtle" onClick={() => void testProvider(p.id)}>Test connection</Button>
                <Button size="xs" color="red" variant="subtle" onClick={() => void removeProvider(p.id)}>Delete</Button>
              </Group>
            </Group>
            {providerTest[p.id] ? (
              <Text size="xs" mb={10} c={providerTest[p.id].ok ? 'teal' : 'orange'}>
                {p.type === 'saml' ? 'SAML: ' : 'OIDC discovery: '}{providerTest[p.id].text}
              </Text>
            ) : null}
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))', gap: '12px 16px' }}>
              <SwitchRow label="Enabled" checked={bool(p.enabled)} onChange={v => upP(i, 'enabled', v)} />
              <Field label="Name (button text)" value={str(p.buttonLabel || p.identityServiceName)} onChange={x => upP(i, 'buttonLabel', x)} placeholder="Log in with …" width="100%" />
              {p.type === 'oidc' ? (
                <>
                  <Field label="Issuer" value={str(p.issuer)} onChange={x => upP(i, 'issuer', x)} placeholder="https://accounts.example.com" width="100%" />
                  <Field label="Authorization URL" value={str(p.authorizationURL)} onChange={x => upP(i, 'authorizationURL', x)} placeholder="auto-discovered from issuer" />
                  <Field label="Token URL" value={str(p.tokenURL)} onChange={x => upP(i, 'tokenURL', x)} placeholder="auto-discovered from issuer" />
                  <Field label="User info URL" value={str(p.userInfoURL)} onChange={x => upP(i, 'userInfoURL', x)} placeholder="auto-discovered from issuer" />
                  <Field label="Client ID" value={str(p.clientID)} onChange={x => upP(i, 'clientID', x)} />
                  <SecretField label="Client secret" set={maskedOf(p.id, 'clientSecret')} value={str(p.clientSecret)} onChange={x => upP(i, 'clientSecret', x)} />
                  <Field label="Scope" value={str(p.scope)} onChange={x => upP(i, 'scope', x)} placeholder="openid profile email" />
                  <Field label="Logout URL" value={str(p.logoutURL)} onChange={x => upP(i, 'logoutURL', x)} />
                  <Field label="User ID field" value={str(p.userIdField)} onChange={x => upP(i, 'userIdField', x)} placeholder="sub" />
                  <Field label="Email field" value={str(p.emailField)} onChange={x => upP(i, 'emailField', x)} placeholder="email" />
                  <Field label="Admin field" value={str(p.adminField)} onChange={x => upP(i, 'adminField', x)} placeholder="groups" />
                  <Field label="Admin value" value={str(p.adminValue)} onChange={x => upP(i, 'adminValue', x)} placeholder="admins" />
                  <Field label="Allowed email domains" value={str(p.allowedEmailDomains)} onChange={x => upP(i, 'allowedEmailDomains', x)} placeholder="corp.example.com (empty = any)" width="100%" />
                </>
              ) : (
                <>
                  <Field label="SP entity id (issuer)" value={str(p.issuer)} onChange={x => upP(i, 'issuer', x)} width="100%" />
                  <Field label="IdP entry point" value={str(p.entryPoint)} onChange={x => upP(i, 'entryPoint', x)} placeholder="https://idp.example.com/sso/saml" width="100%" />
                  <Field label="Audience" value={str(p.audience)} onChange={x => upP(i, 'audience', x)} />
                  <Field label="NameID format" value={str(p.identifierFormat)} onChange={x => upP(i, 'identifierFormat', x)} placeholder="urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress" />
                  <Field label="Authn context" value={str(p.authnContext)} onChange={x => upP(i, 'authnContext', x)} />
                  <Field label="Clock skew (ms)" value={str(p.acceptedClockSkewMs)} onChange={x => upP(i, 'acceptedClockSkewMs', x)} placeholder="0" />
                  <Field label="IdP logout URL" value={str(p.logoutUrl)} onChange={x => upP(i, 'logoutUrl', x)} />
                  <Field label="User ID attribute" value={str(p.attUserId)} onChange={x => upP(i, 'attUserId', x)} />
                  <Field label="Email attribute" value={str(p.attEmail)} onChange={x => upP(i, 'attEmail', x)} />
                  <Field label="First name attribute" value={str(p.attFirstName)} onChange={x => upP(i, 'attFirstName', x)} />
                  <Field label="Last name attribute" value={str(p.attLastName)} onChange={x => upP(i, 'attLastName', x)} />
                  <Field label="Admin attribute" value={str(p.attAdmin)} onChange={x => upP(i, 'attAdmin', x)} />
                  <Field label="Admin value" value={str(p.valAdmin)} onChange={x => upP(i, 'valAdmin', x)} />
                  <SecretField label="IdP certificate (Base64)" set={maskedOf(p.id, 'idpCert')} value={str(p.idpCert)} onChange={x => upP(i, 'idpCert', x)} width="100%" />
                  <SecretField label="SP private key (PEM)" set={maskedOf(p.id, 'privateKey')} value={str(p.privateKey)} onChange={x => upP(i, 'privateKey', x)} width="100%" />
                  <SecretField label="Decryption key (PEM)" set={maskedOf(p.id, 'decryptionPvk')} value={str(p.decryptionPvk)} onChange={x => upP(i, 'decryptionPvk', x)} width="100%" />
                  <Field label="Decryption certificate" value={str(p.decryptionCert)} onChange={x => upP(i, 'decryptionCert', x)} width="100%" />
                  <div>
                    <SwitchRow label="Want signed assertions" checked={bool(p.wantAssertionsSigned)} onChange={v => upP(i, 'wantAssertionsSigned', v)} />
                    <SwitchRow label="Want signed AuthnResponse" checked={bool(p.wantAuthnResponseSigned)} onChange={v => upP(i, 'wantAuthnResponseSigned', v)} />
                    <SwitchRow label="Update profile on login" checked={bool(p.updateUserDetailsOnLogin)} onChange={v => upP(i, 'updateUserDetailsOnLogin', v)} />
                  </div>
                </>
              )}
            </div>
          </div>
        ))}

        {/* LDAP — reference keeps a single LDAP section with search attributes */}
        <div style={{ border: '1px solid var(--mantine-color-gray-3)', borderRadius: 8, padding: 16, opacity: bool(ldap.enabled) ? 1 : 0.7 }}>
          <Group mb={12} justify="space-between" wrap="wrap">
            <Group gap={10}>
              <span style={{ fontSize: 13, fontWeight: 700, padding: '2px 8px', borderRadius: 6, background: 'var(--mantine-color-gray-1)', color: 'var(--mantine-color-gray-7)' }}>LDAP</span>
              <span style={{ fontSize: 13, fontWeight: 600 }}>{str(ldap.identityServiceName) || 'Corporate directory'}</span>
            </Group>
            <Button size="xs" variant="subtle" onClick={() => void testLdap()}>Test connection</Button>
          </Group>
          {ldapTest ? <Text size="xs" mb={10} c={ldapTest.ok ? 'teal' : 'orange'}>LDAP: {ldapTest.text}</Text> : null}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))', gap: '12px 16px' }}>
            <SwitchRow label="Enabled" checked={bool(ldap.enabled)} onChange={v => upL('enabled', v)} />
            <Field label="Label (login button)" value={str(ldap.identityServiceName)} onChange={x => upL('identityServiceName', x)} width="100%" />
            <Field label="URL" value={str(ldap.url)} onChange={x => upL('url', x)} placeholder="ldap://ldap.example.com:389" />
            <Field label="Search base" value={str(ldap.searchBase)} onChange={x => upL('searchBase', x)} placeholder="ou=people,dc=example,dc=com" />
            <Field label="Bind DN" value={str(ldap.bindDN)} onChange={x => upL('bindDN', x)} />
            <SecretField label="Bind credentials" set={ldapMasked('bindCredentials')} value={str(ldap.bindCredentials)} onChange={x => upL('bindCredentials', x)} />
            <Field label="Search filter" value={str(ldap.searchFilter)} onChange={x => upL('searchFilter', x)} placeholder="(&amp;(objectClass=person)(uid={0}))" />
            <Field label="Search scope" value={str(ldap.searchScope) || 'base'} onChange={x => upL('searchScope', x)} placeholder="base | one | sub" />
            <Field label="Username input placeholder" value={str(ldap.placeholder)} onChange={x => upL('placeholder', x)} placeholder="Username or email" />
            <Field label="Email attribute" value={str(ldap.emailAtt)} onChange={x => upL('emailAtt', x)} placeholder="mail" />
            <Field label="First name attribute" value={str(ldap.firstNameAtt)} onChange={x => upL('firstNameAtt', x)} placeholder="givenName" />
            <Field label="Last name attribute" value={str(ldap.lastNameAtt)} onChange={x => upL('lastNameAtt', x)} placeholder="sn" />
            <Field label="Display-name attribute" value={str(ldap.nameAtt)} onChange={x => upL('nameAtt', x)} />
            <Field label="Admin attribute" value={str(ldap.isAdminAtt)} onChange={x => upL('isAdminAtt', x)} placeholder="ou" />
            <Field label="Admin value" value={str(ldap.valAdmin)} onChange={x => upL('valAdmin', x)} placeholder="admins" />
            <Field label="Timeout (ms)" value={str(ldap.timeout)} onChange={x => upL('timeout', x)} placeholder="5000" />
            <div><SwitchRow label="Update profile on login" checked={bool(ldap.updateUserDetailsOnLogin)} onChange={v => upL('updateUserDetailsOnLogin', v)} /></div>
          </div>
        </div>
      </Stack>

      <Group mt={20} gap={12} justify="space-between">
        <Text size="xs" c={flash.saved ? 'teal' : 'red'}>{flash.message}</Text>
        <Button size="sm" loading={flash.busy} onClick={() => void save()}>Save SSO configuration</Button>
      </Group>

      {providers.length > 0 && (
        <Text size="xs" c="dimmed" mt={10}>
          Note: attribute-map role filters (blocked/guest) can be stored per provider via the
          API (POST /admin/sso/test/attr-filter for probing) but are not edited in this UI —
          local login stays the safety valve either way.
        </Text>
      )}
    </SectionShell>
  )
}
