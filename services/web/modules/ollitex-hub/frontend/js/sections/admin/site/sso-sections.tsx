// /hub → Site settings → Integrations: SAML / OIDC / LDAP (owner #32).
// Native Mantine remake of the legacy admin-tools SSO tabs — same sections
// (sso-saml / sso-oidc / sso-ldap), same fields, same PUT contract; secrets
// kept on the server when sent empty.

import React from 'react'
import { Anchor, Group, Text } from '@mantine/core'
import {
  Field,
  NativeSelectField,
  PageLoading,
  SectionShell,
  SectionTitle,
  SecretField,
  SwitchRow,
  bool0,
  str0,
  useSyncValues,
  useSiteSettings,
} from './site-core'

function Loading({ label }: { label: string }) {
  return <PageLoading label={label} />
}

export function SsoSamlSection() {
  const { data, error, flash, save, load } = useSiteSettings('sso-saml')
  const { v, up } = useSyncValues(data, d => ({
    enabled: bool0((d as any).enabled),
    identityServiceName: str0((d as any).identityServiceName),
    issuer: str0((d as any).issuer),
    entryPoint: str0((d as any).entryPoint),
    audience: str0((d as any).audience),
    wantAssertionsSigned: bool0((d as any).wantAssertionsSigned, true),
    cert: '',
    key: '',
  }))
  const certSet = Boolean((data as any)?.idpCertSet)
  const keySet = Boolean((data as any)?.privateKeySet)
  if (!data && !error) return <Loading label="Loading SAML settings…" />
  if (error && !data) {
    return (
      <Group justify="space-between" gap="xs" wrap="wrap">
        <Text size="sm" c="red">Couldn’t load SAML settings — {error}</Text>
        <Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor>
      </Group>
    )
  }
  return (
    <SectionShell
      title="SSO · SAML"
      badge="SAML"
      enabled={Boolean(v.enabled)}
      onEnabled={v2 => up({ enabled: v2 })}
      description="Single sign-on with a SAML 2.0 identity provider."
      footerNote="Applies at next login (no container restart required)."
      flash={flash}
      onSave={() => void save({
        enabled: Boolean(v.enabled),
        identityServiceName: String(v.identityServiceName || ''),
        issuer: String(v.issuer || ''),
        entryPoint: String(v.entryPoint || ''),
        audience: String(v.audience || ''),
        wantAssertionsSigned: Boolean(v.wantAssertionsSigned),
        idpCert: String(v.cert || ''),
        privateKey: String(v.key || ''),
      })}
    >
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Service name" value={String(v.identityServiceName || '')} onChange={x => up({ identityServiceName: x })} placeholder="Corporate SSO" />
        <Field label="Issuer" value={String(v.issuer || '')} onChange={x => up({ issuer: x })} placeholder="https://app.example.com" />
      </Group>
      <SectionTitle top>Identity provider</SectionTitle>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Entry point" required value={String(v.entryPoint || '')} onChange={x => up({ entryPoint: x })} placeholder="https://idp.example.com/sso/saml" />
        <Field label="Audience" value={String(v.audience || '')} onChange={x => up({ audience: x })} placeholder="Entity ID receiving the assertion" />
      </Group>
      <SwitchRow label="Want signed assertions" checked={Boolean(v.wantAssertionsSigned)} onChange={x => up({ wantAssertionsSigned: x })} />
      <SectionTitle top>Credentials</SectionTitle>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start', width: '100%' }}>
        <SecretField label="IdP certificate (Base64)" set={Boolean(certSet)} value={String(v.cert || '')} onChange={x => up({ cert: x })} width="100%" />
        <SecretField label="SP private key" set={Boolean(keySet)} value={String(v.key || '')} onChange={x => up({ key: x })} hint="Leave empty to keep the stored key." width="100%" />
      </Group>
      <Group gap="xs" wrap="wrap">
        <Text size="xs" c="dimmed">Your SP metadata for the IdP admin:</Text>
        <Anchor href="/saml/meta" target="_blank" rel="noreferrer" size="sm">/saml/meta</Anchor>
      </Group>
    </SectionShell>
  )
}

export function SsoOidcSection() {
  const { data, error, flash, save, load } = useSiteSettings('sso-oidc')
  const { v, up } = useSyncValues(data, d => ({
    enabled: bool0((d as any).enabled),
    identityServiceName: str0((d as any).identityServiceName),
    issuer: str0((d as any).issuer),
    authorizationURL: str0((d as any).authorizationURL),
    tokenURL: str0((d as any).tokenURL),
    userInfoURL: str0((d as any).userInfoURL),
    logoutURL: str0((d as any).logoutURL),
    clientID: str0((d as any).clientID),
    clientSecret: '',
    scope: str0((d as any).scope, 'openid profile email'),
    // 2026-09 (owner OIDC item): the claim-mapping + admin-promotion fields
    // that were previously env/DB-only are now editable here (they feed
    // Settings.oidc.* via ssoConfigLoader → OIDCModuleManager):
    attUserId: str0((d as any).attUserId, 'id'),
    attAdmin: str0((d as any).attAdmin),
    valAdmin: str0((d as any).valAdmin),
    updateUserDetailsOnLogin: bool0((d as any).updateUserDetailsOnLogin),
    allowedEmailDomains: str0(
      Array.isArray((d as any)?.allowedOIDCEmailDomains)
        ? (d as any).allowedOIDCEmailDomains.join(',')
        : (d as any)?.allowedOIDCEmailDomains
    ),
  }))
  const secretSet = Boolean((data as any)?.clientSecretSet)
  if (!data && !error) return <Loading label="Loading OIDC settings…" />
  if (error && !data) {
    return (
      <Group justify="space-between" gap="xs" wrap="wrap">
        <Text size="sm" c="red">Couldn’t load OIDC settings — {error}</Text>
        <Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor>
      </Group>
    )
  }
  return (
    <SectionShell
      title="SSO · OIDC"
      badge="OIDC"
      enabled={Boolean(v.enabled)}
      onEnabled={x => up({ enabled: x })}
      description="OpenID Connect sign-in. URL fields may stay empty for issuer auto-discovery."
      footerNote="Applies at next login (no container restart required)."
      flash={flash}
      onSave={() => void save({
        enabled: Boolean(v.enabled),
        identityServiceName: String(v.identityServiceName || ''),
        issuer: String(v.issuer || ''),
        authorizationURL: String(v.authorizationURL || ''),
        tokenURL: String(v.tokenURL || ''),
        userInfoURL: String(v.userInfoURL || ''),
        logoutURL: String(v.logoutURL || ''),
        clientID: String(v.clientID || ''),
        clientSecret: String(v.clientSecret || ''),
        scope: String(v.scope || ''),
        // claim mapping + admin promotion (owner OIDC item):
        attUserId: String(v.attUserId || ''),
        attAdmin: String(v.attAdmin || ''),
        valAdmin: String(v.valAdmin || ''),
        updateUserDetailsOnLogin: Boolean(v.updateUserDetailsOnLogin),
        allowedOIDCEmailDomains: String(v.allowedEmailDomains || ''),
      })}
    >
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Service name" value={String(v.identityServiceName || '')} onChange={x => up({ identityServiceName: x })} placeholder="Corporate SSO" />
        <Field label="Issuer" required value={String(v.issuer || '')} onChange={x => up({ issuer: x })} placeholder="https://accounts.example.com" />
      </Group>
      <SectionTitle top>OAuth 2.0 / OIDC endpoint set</SectionTitle>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Authorization URL" value={String(v.authorizationURL || '')} onChange={x => up({ authorizationURL: x })} placeholder="auto-discovered" />
        <Field label="Token URL" value={String(v.tokenURL || '')} onChange={x => up({ tokenURL: x })} placeholder="auto-discovered" />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="User info URL" value={String(v.userInfoURL || '')} onChange={x => up({ userInfoURL: x })} placeholder="auto-discovered" />
        <Field label="Scope" value={String(v.scope || '')} onChange={x => up({ scope: x })} placeholder="openid profile email" />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Logout URL" value={String(v.logoutURL || '')} onChange={x => up({ logoutURL: x })} />
      </Group>
      <SectionTitle top>OAuth 2.0 client</SectionTitle>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Client ID" required value={String(v.clientID || '')} onChange={x => up({ clientID: x })} />
        <SecretField label="Client secret" set={Boolean(secretSet)} value={String(v.clientSecret || '')} onChange={x => up({ clientSecret: x })} hint="Leave empty to keep the stored secret." />
      </Group>
      {/* 2026-09 (owner OIDC item): claim mapping + admin promotion — the
          fields that back the (now non-standard-claim-aware) admin check. */}
      <SectionTitle top>Claim mapping</SectionTitle>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="User ID claim" value={String(v.attUserId || '')} onChange={x => up({ attUserId: x })} placeholder="id" hint={'Claim used as the stable OIDC user id ("email" = the email). Non-standard claims are read from the userinfo payload.'} />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <SwitchRow label="Update profile on login" checked={Boolean(v.updateUserDetailsOnLogin)} onChange={x => up({ updateUserDetailsOnLogin: x })} />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Admin claim" value={String(v.attAdmin || '')} onChange={x => up({ attAdmin: x })} placeholder="groups" hint={'Claim (standard or non-standard) whose value marks an admin ("email" = use the email).'} />
        <Field label="Admin claim value" value={String(v.valAdmin || '')} onChange={x => up({ valAdmin: x })} placeholder="admins" hint="Expected value of the admin claim." />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Allowed email domains" value={String(v.allowedEmailDomains || '')} onChange={x => up({ allowedEmailDomains: x })} placeholder="corp.example.com, other.example.com" hint="Comma-separated; empty = any domain." />
      </Group>
      <div className="muted-hint" style={{ fontSize: '0.82em', opacity: 0.72 }}>
        Non-standard claims (e.g. <code>groups</code>, <code>role</code>) are matched against the raw
        userinfo payload — <b>userinfo takes priority</b> over ID-token claims
        (see docs/wiki/admins/08-sso-saml-oidc.md + CREDITS.md for the fix provenance).
      </div>
    </SectionShell>
  )
}

export function SsoLdapSection() {
  const { data, error, flash, save, load } = useSiteSettings('sso-ldap')
  const { v, up } = useSyncValues(data, d => ({
    enabled: bool0((d as any).enabled),
    identityServiceName: str0((d as any).identityServiceName),
    url: str0((d as any).url),
    searchBase: str0((d as any).searchBase),
    bindDN: str0((d as any).bindDN),
    bindCredentials: '',
    searchFilter: str0((d as any).searchFilter, '(uid={{username}})'),
    searchScope: str0((d as any).searchScope, 'sub'),
    placeholder: str0((d as any).placeholder, 'Username'),
    emailAtt: str0((d as any).emailAtt, 'mail'),
    firstNameAtt: str0((d as any).firstNameAtt, 'givenName'),
    lastNameAtt: str0((d as any).lastNameAtt, 'sn'),
    isAdminAtt: str0((d as any).isAdminAtt),
    updateUserDetailsOnLogin: bool0((d as any).updateUserDetailsOnLogin),
  }))
  const credsSet = Boolean((data as any)?.bindCredentialsSet)
  if (!data && !error) return <Loading label="Loading LDAP settings…" />
  if (error && !data) {
    return (
      <Group justify="space-between" gap="xs" wrap="wrap">
        <Text size="sm" c="red">Couldn’t load LDAP settings — {error}</Text>
        <Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor>
      </Group>
    )
  }
  return (
    <SectionShell
      title="SSO · LDAP"
      badge="LDAP"
      enabled={Boolean(v.enabled)}
      onEnabled={x => up({ enabled: x })}
      description="Authenticate against an LDAP directory (search + bind)."
      footerNote="Applies at next login (no container restart required)."
      flash={flash}
      onSave={() => void save({
        enabled: Boolean(v.enabled),
        identityServiceName: String(v.identityServiceName || ''),
        url: String(v.url || ''),
        searchBase: String(v.searchBase || ''),
        bindDN: String(v.bindDN || ''),
        bindCredentials: String(v.bindCredentials || ''),
        searchFilter: String(v.searchFilter || ''),
        searchScope: String(v.searchScope || ''),
        placeholder: String(v.placeholder || ''),
        emailAtt: String(v.emailAtt || ''),
        firstNameAtt: String(v.firstNameAtt || ''),
        lastNameAtt: String(v.lastNameAtt || ''),
        isAdminAtt: String(v.isAdminAtt || ''),
        updateUserDetailsOnLogin: Boolean(v.updateUserDetailsOnLogin),
        // 2026-09-11 (owner batch 2 item 4): editable operation timeout (ms)
        timeout: v.timeout === '' || v.timeout === undefined ? undefined : Number(v.timeout),
      })}
    >
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Service name" value={String(v.identityServiceName || '')} onChange={x => up({ identityServiceName: x })} placeholder="Corporate LDAP" />
        <Field label="URL" required value={String(v.url || '')} onChange={x => up({ url: x })} placeholder="ldap://ldap.example.com:389" />
      </Group>
      <SectionTitle top>Connection</SectionTitle>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start', width: '100%' }}>
        <Field label="Bind DN" value={String(v.bindDN || '')} onChange={x => up({ bindDN: x })} placeholder="cn=admin,dc=example,dc=com" width="100%" />
        <SecretField label="Bind credentials" set={Boolean(credsSet)} value={String(v.bindCredentials || '')} onChange={x => up({ bindCredentials: x })} hint="Leave empty to keep the stored password." width="100%" />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field
          label="Timeout (ms)"
          type="number"
          value={v.timeout === undefined || v.timeout === null ? '10000' : String(v.timeout)}
          onChange={x => up({ timeout: x })}
          placeholder="10000"
          hint="Operation timeout in milliseconds (blank = instance default 10 000)."
          width="100%"
        />
      </Group>
      <SectionTitle top>Search</SectionTitle>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Search base" required value={String(v.searchBase || '')} onChange={x => up({ searchBase: x })} placeholder="ou=people,dc=example,dc=com" />
        <NativeSelectField label="Scope" value={String(v.searchScope || 'sub')} onChange={x => up({ searchScope: x })} options={[
          { value: 'sub', label: 'sub (subtree)' },
          { value: 'one', label: 'one (single level)' },
          { value: 'base', label: 'base' },
        ]} />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Filter" required value={String(v.searchFilter || '')} onChange={x => up({ searchFilter: x })} placeholder="(uid={{username}})" hint="{{username}} is replaced with the entered name." />
        <Field label="Login placeholder" value={String(v.placeholder || '')} onChange={x => up({ placeholder: x })} placeholder="Username" />
      </Group>
      <SectionTitle top>Attribute mapping</SectionTitle>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start', width: '100%' }}>
        <Field label="Email attribute" required value={String(v.emailAtt || '')} onChange={x => up({ emailAtt: x })} placeholder="mail" width="100%" />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="First name attribute" value={String(v.firstNameAtt || '')} onChange={x => up({ firstNameAtt: x })} placeholder="givenName" />
        <Field label="Last name attribute" value={String(v.lastNameAtt || '')} onChange={x => up({ lastNameAtt: x })} placeholder="sn" />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Admin attribute" value={String(v.isAdminAtt || '')} onChange={x => up({ isAdminAtt: x })} placeholder="memberOf" hint="Optional; grants admin when set." />
        <SwitchRow label="Update user details on login" checked={Boolean(v.updateUserDetailsOnLogin)} onChange={x => up({ updateUserDetailsOnLogin: x })} />
      </Group>
    </SectionShell>
  )
}
