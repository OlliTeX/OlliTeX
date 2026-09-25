import React from 'react'
import {
  Card,
  Stack,
  Text,
  TextInput,
  PasswordInput,
  Alert,
} from '@mantine/core'
import SystemMessages from '@/shared/components/system-messages'

/**
 * OLiT auth pages (owner scope: /login + /register on the shared Mantine
 * surface). Design + behaviour contract:
 *
 * - THE FORM REMAINS A NATIVE HTML FORM (action /login or /register,
 *   hidden _csrf, data-ol-async-form) — the existing hydrate-form engine
 *   (CSRF fetch submit, rate-limit/captcha handling, redirect, message
 *   bag) takes it over; server flow is untouched by construction.
 * - Input ids/names KEEP the legacy contract (#email, #password,
 *   firstNameField/lastNameField/emailField) so the e2e suite and any
 *   password manager keep working.
 * - Mantine renders the components; the page chrome (card, spacing) is
 *   the hub/ollitex theme via OlliTProvider in the entry bundle.
 */

function authConfig(): any {
  const el = document.querySelector<HTMLMetaElement>(
    'meta[name="ol-auth-config"]'
  )
  try {
    return el ? JSON.parse(el.content || '{}') : {}
  } catch {
    return {}
  }
}

export function ErrorBag() {
  const cfg = authConfig()
  // The hydrate-form engine fills [data-ol-form-messages-new-style] with
  // <p>/<div> error nodes server-translation-wise; render the container
  // (Mantine-styled) and any static server-form messages (login error
  // hints like 'invalid-password-retry-or-reset' rendered by PUG into the
  // config).
  const staticMessages: string[] = cfg.errorMessages || []
  return (
    <Stack gap={6}>
      {staticMessages.map((m, i) => (
        <Alert key={i} variant="danger" withCloseButton={false} icon={undefined} my={0} fw={500} size="md">
          {m}
        </Alert>
      ))}
      <div data-ol-form-messages-new-style role="alert" className="ol-auth-form-messages" style={{ alignItems: 'stretch' }} />
    </Stack>
  )
}

export function AuthCard({
  title,
  actions,
  children,
  footer,
}: {
  title: string
  actions?: React.ReactNode
  children: React.ReactNode
  footer?: React.ReactNode
}) {
  return (
    <div
      className="ol-auth-root"
      style={{
        minHeight: 'calc(100vh - 90px)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '48px 16px',
        backgroundColor: 'var(--bg-primary-themed)',
      }}
    >
      <Card withBorder radius="md" p="xl" style={{ width: '100%', maxWidth: 440 }}>
        <Stack gap="md">
          {/* Owner #17a (2026-09-13): system messages on the auth pages too. */}
          <SystemMessages />
          <Text ta="center" style={{ fontSize: 'var(--font-size-06)', fontWeight: 700 }} fw={700}>
            {title}
          </Text>
          <ErrorBag />
          {children}
        </Stack>
        {actions ? (
          <Stack gap="sm" mt="lg" align="center">
            {actions}
          </Stack>
        ) : null}
        {footer ? (
          <div style={{ marginTop: 16, textAlign: 'center' }}>{footer}</div>
        ) : null}
      </Card>
    </div>
  )
}

export interface FieldSpec {
  id: string
  name: string
  type: 'text' | 'email' | 'password'
  label: string
  placeholder?: string
  required?: boolean
  autoComplete?: string
  maxLength?: number
  autoFocus?: boolean
}

export function AuthField({ field }: { field: FieldSpec }) {
  const common = {
    id: field.id,
    name: field.name,
    label: field.label,
    placeholder: field.placeholder,
    required: field.required,
    autoComplete: field.autoComplete,
    maxLength: field.maxLength,
    autoFocus: field.autoFocus,
    radius: 'sm' as const,
    size: 'lg' as const,
  }
  if (field.type === 'password') {
    return <PasswordInput {...(common as any)} />
  }
  return <TextInput {...(common as any)} />
}
