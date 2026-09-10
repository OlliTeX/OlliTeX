// OLiT /user/password/reset on the shared Mantine surface (owner hub #2,
// 2026-09-16). Same contract as /login: THE FORM REMAINS A NATIVE HTML
// FORM (action /user/password/reset, hidden _csrf, data-ol-async-form) —
// the existing hydrate-form engine (CSRF fetch submit, rate-limiting,
// optional invisible captcha, message bag, sent/not-sent states) takes it
// over; the server flow is untouched by construction. The Mantine card
// below is the same design system as /login and /register.
import React from 'react'
import { createRoot } from 'react-dom/client'
import { Stack, Anchor, Button, Text } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { hydrateAsyncForm } from '../../features/form-helpers/hydrate-form'
import OlliTProvider from '../../shared/mantine/provider'
import { applyScheme as applyAuthScheme } from '../../shared/mantine/overall-theme'
import { AuthCard, AuthField, type FieldSpec } from '../../features/auth-pages/auth-page'
import '@/i18n'

// OlliT auth pages are standalone signed-out surfaces: pin the light
// scheme (Mantine provider + app theme variables agree), matching
// /login + /register (2026-09-10 a11y contrast fix).
document.documentElement.setAttribute('data-mantine-color-scheme', 'light')
applyAuthScheme('light')

function getMetaString(name: string): string {
  const el = document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)
  return el?.content || ''
}

function PasswordResetPage() {
  const { t } = useTranslation()
  const [csrf] = React.useState(() => getMetaString('ol-csrfToken'))
  const captchaEnabled = React.useMemo(
    () => getMetaString('ol-password-reset-captcha') === 'true',
    []
  )
  const error = React.useMemo(() => getMetaString('ol-password-reset-error'), [])
  const tokenExpired = error === 'password_reset_token_expired'

  React.useEffect(() => {
    document
      .querySelectorAll<HTMLFormElement>('[data-ol-async-form]')
      .forEach(hydrateAsyncForm)
  }, [])

  const emailField: FieldSpec = {
    id: 'email',
    name: 'email',
    type: 'email',
    label: t('email'),
    placeholder: 'email@example.com',
    required: true,
    autoComplete: 'username',
    autoFocus: true,
  }

  return (
    <AuthCard
      title={tokenExpired ? t('sorry_your_token_expired') : t('password_reset')}
      footer={
        <Anchor href="/login" size="sm">
          {t('back_to_log_in')}
        </Anchor>
      }
    >
      <form
        name="passwordResetForm"
        {...(captchaEnabled ? { captcha: 'true' } : {})}
        data-ol-async-form
        action="/user/password/reset"
        method="POST"
      >
        <input name="_csrf" type="hidden" value={csrf} />

        {tokenExpired ? (
          <Text size="md" c="dimmed" ta="center">
            {t('please_request_a_new_password_reset_email_and_follow_the_link')}
          </Text>
        ) : (
          <div data-ol-not-sent>
            <Text size="md" c="dimmed" ta="center" style={{ marginBottom: 4 }}>
              {t('enter_your_email_address_below_and_we_will_send_you_a_link_to_reset_your_password')}
            </Text>
            <Stack gap="md">
              {/* SSO/LDAP error (server 403, message key
                  no-password-allowed-due-to-sso) — the hydrate-form engine
                  unhides this exact node; markup mirrors the legacy
                  customFormMessageNewStyle('danger') contract. */}
              <div
                data-ol-custom-form-message="no-password-allowed-due-to-sso"
                hidden
                role="alert"
                aria-live="polite"
              >
                <div className="notification notification-type-error mb-3">
                  <div className="notification-icon">
                    <span aria-hidden="true" translate="no" className="material-symbols">
                      error
                    </span>
                  </div>
                  <div className="notification-content text-left">
                    {t('you_cant_reset_password_due_to_ldap_or_sso')}
                  </div>
                </div>
              </div>
              <AuthField field={emailField} />
              <Button type="submit" size="lg" w="100%" data-ol-disabled-inflight>
                <span data-ol-inflight="idle">{t('reset_password_sentence_case')}</span>
                <span hidden data-ol-inflight="pending">
                  {t('requesting_password_reset')}…
                </span>
              </Button>
            </Stack>
          </div>
        )}

        <div data-ol-sent hidden>
          <Text size="md" ta="center" style={{ marginBottom: 8 }}>
            {t('password_reset_email_sent')}
          </Text>
          <Button component="a" href="/login" size="lg" w="100%">
            {t('back_to_log_in')}
          </Button>
        </div>
      </form>
    </AuthCard>
  )
}

const el = document.getElementById('auth-root')
if (el) {
  const root = createRoot(el)
  root.render(
    <OlliTProvider>
      <PasswordResetPage />
    </OlliTProvider>
  )
}
