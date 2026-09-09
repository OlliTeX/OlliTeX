// OLiT /login on the shared Mantine surface (owner scope). Entry follows
// the hub pattern (frontend/js/pages/** auto-entries): it hydrates the
// existing async-form engine for the NATIVE form — the exact legacy server
// contract (POST /login, _csrf, redirect, rate limiting) — and renders the
// Mantine login card (same design system as the hubs).
import React from 'react'
import { createRoot } from 'react-dom/client'
import { Group, Stack, Anchor, Button } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { hydrateAsyncForm } from '../../features/form-helpers/hydrate-form'
import OlliTProvider from '../../shared/mantine/provider'
import { applyScheme as applyAuthScheme } from '../../shared/mantine/overall-theme'
import { AuthCard, AuthField, type FieldSpec } from '../../features/auth-pages/auth-page'
import '@/i18n'

// OlliT auth pages are standalone signed-out surfaces: pin the light
// scheme (Mantine provider + app theme variables agree), matching the
// legacy always-light login page — a mixed-scheme render (dark Mantine
// chrome over light theme variables) failed a11y contrast (2026-09-10).
document.documentElement.setAttribute('data-mantine-color-scheme', 'light')
applyAuthScheme('light')

interface SsoButton {
  label: string
  href: string
}

function getAuthConfig(): {
  sso?: SsoButton[]
  ldapEnabled?: boolean
  ldapPlaceholder?: string
  supportTitle?: string
  supportText?: string
} {
  const el = document.querySelector<HTMLMetaElement>('meta[name="ol-auth-config"]')
  try {
    return el ? JSON.parse(el.content || '{}') : {}
  } catch {
    return {}
  }
}

function LoginPage() {
  const { t } = useTranslation()
  const cfg = React.useMemo(getAuthConfig, [])
  const [csrf] = React.useState(() => {
    const m = document.querySelector<HTMLMetaElement>('meta[name="ol-csrfToken"]')
    return m?.content || ''
  })

  React.useEffect(() => {
    document
      .querySelectorAll<HTMLFormElement>('[data-ol-async-form]')
      .forEach(hydrateAsyncForm)
  }, [])

  const emailField: FieldSpec = {
    id: 'email',
    name: 'email',
    type: cfg.ldapEnabled ? 'text' : 'email',
    label: cfg.ldapEnabled ? cfg.ldapPlaceholder || 'Username' : t('email'),
    placeholder: cfg.ldapEnabled ? cfg.ldapPlaceholder || 'Username' : 'email@example.com',
    required: true,
    autoComplete: 'username',
    autoFocus: true,
  }
  const passwordField: FieldSpec = {
    id: 'password',
    name: 'password',
    type: 'password',
    label: t('password'),
    required: true,
    autoComplete: 'current-password',
  }

  return (
    <AuthCard
      title={cfg.supportTitle || t('log_in')}
      footer={
        <div>
          <Anchor href="/register" size="sm">
            {t('dont_have_an_account')}
          </Anchor>
          {cfg.supportText ? (
            <div style={{ marginTop: 8, fontSize: 'var(--font-size-03)', color: 'var(--content-secondary-themed)' }}>
              {cfg.supportText}
            </div>
          ) : null}
        </div>
      }
    >
      <form name="loginForm" data-ol-async-form action="/login" method="POST">
        <input name="_csrf" type="hidden" value={csrf} />
        <Stack gap="md">
          <AuthField field={emailField} />
          <AuthField field={passwordField} />
          <Group justify="space-between" wrap="nowrap" style={{ marginTop: 4 }}>
            <Anchor component="a" href="/user/password/reset" size="sm" style={{ whiteSpace: 'nowrap' }}>
              {t('forgot_your_password')}?
            </Anchor>
            <Button type="submit" size="lg" w="fit-content" data-ol-disabled-inflight>
              <span data-ol-inflight="idle">{t('login')}</span>
              <span hidden data-ol-inflight="pending">
                {t('logging_in')}…
              </span>
            </Button>
          </Group>
        </Stack>
      </form>
      {(cfg.sso || []).map(btn => (
        <Button
          key={btn.href}
          component="a"
          href={btn.href}
          variant="light"
          size="lg"
          fullWidth
          style={{ marginTop: 12 }}
          data-ol-disabled-inflight
        >
          <span data-ol-inflight="idle">{btn.label}</span>
          <span hidden data-ol-inflight="pending">
            {t('logging_in')}…
          </span>
        </Button>
      ))}
    </AuthCard>
  )
}

const el = document.getElementById('auth-root')
if (el) {
  const root = createRoot(el)
  root.render(
    <OlliTProvider>
      <LoginPage />
    </OlliTProvider>
  )
}
