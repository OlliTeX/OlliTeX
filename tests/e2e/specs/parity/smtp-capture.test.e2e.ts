/**
 * 2026-09-16 (owner task T5, IMPROVEMENTS P0.2): SMTP capture proof.
 *
 * The e2e stack now ships a local Mailpit relay (no paid service):
 *   - OVERLEAF_EMAIL_* env wires the app's EmailSender (password-reset,
 *     invitations, registration mails) at http://mailpit:1025
 *   - the hub "test e-mail" button (POST /admin/site-settings/email/test)
 *     resolves the same section (site-settings email falls back to the
 *     env values) and must deliver.
 *
 * This spec sends a test e-mail through the admin endpoint and asserts it
 * lands in the stack's local SMTP sink capture (API on 127.0.0.1:18025).
 * That is a full-stack SMTP proof: app → SMTP → captured envelope.
 *
 * Sink API (tests/e2e/tools/smtp-sink.js): GET /api/messages →
 *   { count, messages: [{ from, to: string[], subject, raw, at }] }
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { ADMIN } from '../../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'
const SINK = 'http://127.0.0.1:18025'
const TO = 'e2e-admin@e2e.test'

function countFor(to: string): Promise<number> {
  return (async () => {
    try {
      const r = await fetch(`${SINK}/api/messages`)
      const j = await r.json()
      return (j.messages || []).filter(
        (m: any) => (m.to || []).map(String).includes(to)
      ).length
    } catch {
      return 0
    }
  })()
}

let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
})
test.afterAll(async () => {
  if (p) await p.context().close().catch(() => {})
})

test('smtp: admin test e-mail lands in the local relay (Mailpit capture)', async () => {
  // count messages addressed to TO before the send (the test mailbox is
  // disposable; delta check tolerates any warmup mail on the stack)
  const before: number = await countFor(TO)

  const csrf: string | null =
    (await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || undefined

  const send: any = await p.request.post(BASE + '/admin/site-settings/email/test', {
    headers: {
      'content-type': 'application/json',
      ...(csrf ? { 'X-CSRF-TOKEN': csrf } : {}),
    },
    data: JSON.stringify({ to: TO }),
  })
  const body = (await send.text().catch(() => '')).slice(0, 200)
  expect(send.status(), `test e-mail accepted (got ${send.status()}: ${body})`).toBe(200)

  // poll the sink: delivery is async-ish through nodemailer
  let delivered = false
  for (let i = 0; i < 20 && !delivered; i++) {
    await p.waitForTimeout(1000)
    delivered = (await countFor(TO)) > before
    if (delivered) break
  }

  expect(
    delivered,
    `local sink should have captured the test e-mail for ${TO} (sink ${SINK})`
  ).toBeTruthy()
})
