// OLiT /register on the shared Mantine surface (owner scope). Same contract
// as /login: native form (POST /register, _csrf) taken over by the existing
// async-form engine; Mantine renders the card. CE registration fields per
// the legacy form (first/last/email — the instance decides what happens),
// plus the legacy context states (invitation, template) and domain
// restrictions.
import React from 'react'
import { createRoot } from 'react-dom/client'
import { Stack, Anchor, Button, Alert } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { hydrateAsyncForm } from '../../features/form-helpers/hydrate-form'
import OlliTProvider from '../../shared/mantine/provider'
import { applyScheme as applyAuthScheme } from '../../shared/mantine/overall-theme'
import { AuthCard, AuthField, type FieldSpec } from '../../features/auth-pages/auth-page'
import '@/i18n'

// OlliT auth pages are standalone signed-out surfaces: pin the light
// scheme (Mantine provider + app theme variables agree), matching the
// legacy always-light login page.
document.documentElement.setAttribute('data-mantine-color-scheme', 'light')
applyAuthScheme('light')

interface DomainItem {
  domain: string
  subdomains?: boolean
  exact?: boolean
}

interface AuthConfig {
  domains?: DomainItem[]
  context?:
    | { kind: 'invite'; userFirstName: string; projectName: string }
    | { kind: 'template'; templateName: string }
    | null
}

function getAuthConfig(): AuthConfig {
  const el = document.querySelector<HTMLMetaElement>('meta[name="ol-auth-config"]')
  try {
    return el ? JSON.parse(el.content || '{}') : {}
  } catch {
    return {}
  }
}

function RegisterContext({ cfg }: { cfg: AuthConfig }) {
  const { t } = useTranslation()
  if (cfg.context?.kind === 'invite') {
    return (
      <Alert variant="info" style={{ textAlign: 'left' }}>
        {t('user_wants_you_to_see_project', { username: cfg.context.userFirstName, projectname: '' })}{' '}
        <em>{cfg.context.projectName}</em>
        <div style={{ marginTop: 4 }}>
          {t('join_sl_to_view_project')}{' '}
          <Anchor href="/login" size="sm">
            {t('login_here')}
          </Anchor>
        </div>
      </Alert>
    )
  }
  if (cfg.context?.kind === 'template') {
    return (
      <Alert variant="info" style={{ textAlign: 'left' }}>
        {t('register_to_edit_template', { templateName: cfg.context.templateName })}{' '}
        <Anchor href="/login" size="sm">
          {t('login_here')}
        </Anchor>
      </Alert>
    )
  }
  return null
}

function RegisterPage() {
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

  const fields: FieldSpec[] = [
    {
      id: 'firstNameField',
      name: 'first_name',
      type: 'text',
      label: t('first_name'),
      placeholder: t('first_name'),
      required: true,
      autoComplete: 'given-name',
      maxLength: 100,
      autoFocus: true,
    },
    {
      id: 'lastNameField',
      name: 'last_name',
      type: 'text',
      label: t('last_name'),
      placeholder: t('last_name'),
      required: true,
      autoComplete: 'family-name',
      maxLength: 100,
    },
    {
      id: 'emailField',
      name: 'email',
      type: 'email',
      label: t('email'),
      placeholder: t('email'),
      required: true,
      autoComplete: 'username',
      maxLength: 100,
    },
  ]

  return (
    <AuthCard
      title={t('register')}
      footer={
        <Anchor href="/login" size="sm">
          {t('login_here')}
        </Anchor>
      }
    >
      <Stack gap="md">
        {cfg.domains && cfg.domains.length > 0 ? (
          <Alert variant="light" icon={undefined} fw={500} style={{ textAlign: 'left' }}>
            <div style={{ fontSize: 'var(--font-size-03)' }}>
              {t('registration_is_restricted_to_the_following_email_domains')}
            </div>
            <ul style={{ margin: '4px 0 0', paddingLeft: 16, fontSize: 'var(--font-size-03)' }}>
              {cfg.domains.map(d => (
                <li key={d.domain}>
                  <code>{d.domain}</code>
                  {d.subdomains && d.exact
                    ? ` (${t('including_all_subdomains')})`
                    : d.subdomains
                      ? ` (${t('subdomains_of_this_domain_only')})`
                      : ''}
                </li>
              ))}
            </ul>
          </Alert>
        ) : null}
        <form name="registrationForm" data-ol-async-form action="/register" method="POST" aria-label={t('create_account')}>
          <input name="_csrf" type="hidden" value={csrf} />
          <Stack gap="md">
            {fields.map(f => (
              <AuthField key={f.id} field={f} />
            ))}
            <Button type="submit" size="lg" fullWidth data-ol-disabled-inflight>
              <span data-ol-inflight="idle">{t('create_account')}</span>
              <span hidden data-ol-inflight="pending">
                {t('registering')}…
              </span>
            </Button>
          </Stack>
        </form>
      </Stack>
      <div style={{ marginTop: 12 }}>
        <RegisterContext cfg={cfg} />
      </div>
    </AuthCard>
  )
}

const el = document.getElementById('auth-root')
if (el) {
  const root = createRoot(el)
  root.render(
    <OlliTProvider>
      <RegisterPage />
    </OlliTProvider>
  )
}
