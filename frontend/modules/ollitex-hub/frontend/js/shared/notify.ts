// overleaf-lab — #5 (2026-09-08): single safe toast entry point for /hub.
//
// Root cause it fixes (PG-TO-1): hub sections imported `@mantine/notifications`
// directly in 17 places, and one section called a non-existent
// `useMantineNotifications` hook → the whole leaf crashed.
//
// Rules this enforces:
//   * one toast API for every /hub section (import `notify` from here),
//   * a failed toast is cosmetic and must NEVER take down the caller
//     (show() is wrapped in try/catch — Mantine can throw without a provider),
//   * the repo eslint config (services/web/eslint.config.mjs) bans importing
//     '@mantine/notifications' anywhere in the hub module EXCEPT this file.

import { notifications } from '@mantine/notifications'

export type NotifyOptions = {
  message: string
  /** Mantine color (teal/green/red/yellow/gray/orange/blue …) */
  color?: string
  title?: string
  description?: string
  autoClose?: number | false
}

type ShowOptions = Partial<{
  message: string
  color: string
  title: string
  description: string
  autoClose: number | false
}>

export function notify(options: NotifyOptions): void {
  const payload: ShowOptions = { message: options.message }
  if (options.color) payload.color = options.color
  if (options.title) payload.title = options.title
  if (options.description) payload.description = options.description
  if (options.autoClose !== undefined) payload.autoClose = options.autoClose
  try {
    notifications.show(payload as never)
  } catch {
    // toasts are cosmetic; never let a failed toast break the caller
  }
}

/** Convenience: ok('Saved.', { description: 'The settings were stored.' }) */
export function ok(message: string, extra: Omit<NotifyOptions, 'message'> = {}): void {
  notify({ color: 'teal', message, ...extra })
}

/** Convenience: fail('Delete failed.', { description: '...' }) */
export function fail(message: string, extra: Omit<NotifyOptions, 'message'> = {}): void {
  notify({ color: 'red', message, ...extra })
}

/** Extract a human-readable message from an error/`{data:{message}}` shape. */
export function errorMessage(err: unknown, fallback: string): string {
  if (err && typeof err === 'object') {
    const e = err as any
    if (typeof e?.data?.message === 'string' && e.data.message) return e.data.message
    if (typeof e?.message === 'string' && e.message) return e.message
  }
  return fallback
}
