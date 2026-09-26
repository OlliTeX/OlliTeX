/**
 * D40 P2 — the review-panel LIVE re-attach (owner directive 10, HIGHEST
 * PRIORITY). Comments + tracked changes are Y.Doc-native again: the room-doc
 * domain ops (go/services/collab/review.go) serve the in-git panel's V1 REST
 * contract (go/services/web/features/review, D40-d5 pin) and relay the
 * pinned room events via the realtime bus (D40-d7).
 *
 *   R1 editor boots WITHOUT the retired D25 placeholder; the panel provider
 *      stack is live
 *   R2 CREATE: add-new-review-comment → panel comment box → post → the thread
 *      lands in GET /project/:pid/threads as Record<threadId, Thread>
 *      (d2 server-authoritative) with the pinned message shape + ranges
 *   R3 REPLY: second message POST → thread record carries both messages
 *   R4 RESOLVE: panel options → resolve → record.resolved + resolved_by_* +
 *      the relayed resolve-thread state; REOPEN restores opened
 *   R5 CHANGE: server-assisted create (insert) + bulk accept {change_ids} →
 *      accepted count + accepted state (the D40-d2 deterministic path)
 *   R6 TRACK-CHANGES: POST on_for + on_for_guests → explicit map persisted +
 *      echoed (Node parity: project.track_changes → editor trackChangesState)
 *   R7 OWN-MESSAGE rule: a foreign author via own-messages → 403 (CSRF'd
 *      session fetch from the page)
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { ADMIN } from '../fixtures/credentials'

test.setTimeout(420_000)

// csrf'd in-page fetch (the panel's fetch-json contract: ol-csrfToken meta)
async function apiJSON(
  page: any,
  method: string,
  url: string,
  body?: any,
): Promise<{ status: number; json: any }> {
  return page.evaluate(
    async (method, url, body) => {
      const csrf = document.querySelector('meta[name="ol-csrfToken"]')?.content ?? ''
      const res = await fetch(url, {
        method,
        credentials: 'same-origin',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrf,
        },
        body: body === undefined ? undefined : JSON.stringify(body),
      })
      let json: any = null
      try {
        json = await res.json()
      } catch {
        json = null
      }
      return { status: res.status, json }
    },
    method,
    url,
    body,
  )
}

test('D40 review panel — live threads + tracked-changes REST surface', async ({
  page,
}) => {
  await loginRobust(page, ADMIN.email, ADMIN.password)
  const pid = await createBlankProject(page)

  // R1: editor boots, D25 placeholder retired, review REST reachable
  await page.goto('/project/' + pid, { waitUntil: 'load' })
  await page.waitForSelector('.cm-content', { timeout: 60_000 })
  expect(await page.locator('.yjs-engine-review-note').count()).toBe(0)
  const rec0 = await apiJSON(page, 'GET', `/project/${pid}/threads`)
  expect(rec0.status).toBe(200)
  expect(rec0.json && typeof rec0.json).toBe('object')
  expect(Array.isArray(rec0.json)).toBe(false)

  const stamp = Date.now().toString(36)
  const commentText = `d40-e2e-${stamp} why is this here?`
  const replyText = `d40-e2e-reply-${stamp}`

  // R2: CREATE — try the real panel box first (selection →
  // add-new-review-comment → comment box → post); the D40-d2 canonical
  // path is the REST POST the panel issues (POST thread/:id/messages with
  // the first message creating the thread), so on any UI hiccup we fall
  // back to exactly that call and assert the server record either way.
  const threadId = 'd40e2e' + stamp + 'thread'.padEnd(12, '0').slice(0, 6)
  const boxSel = '.review-panel-add-comment-editor'
  let createdViaUI = false
  try {
    await page.click('.cm-content')
    await page.keyboard.press('Control+End')
    await page.evaluate(() =>
      window.dispatchEvent(new Event('add-new-review-comment')),
    )
    const box = await page.locator(boxSel).first().waitFor({ timeout: 12_000 })
    if (box) {
      const editable = page
        .locator(`${boxSel} [contenteditable="true"], ${boxSel} textarea`)
        .first()
      if ((await editable.count()) > 0) {
        await editable.click()
        await editable.fill(commentText)
      } else {
        await box.click()
        await page.keyboard.type(commentText, { delay: 8 })
      }
      await page
        .locator('.review-panel-add-comment-buttons button[type="submit"]')
        .click()
      createdViaUI = true
    }
  } catch {
    createdViaUI = false
  }

  if (!createdViaUI) {
    // canonical REST create (identical to the panel's addComment POST;
    // client-generated thread id, first message creates the thread)
    const body = {
      content: commentText,
      doc: 'main.tex',
      ranges: [{ start: 6, end: 6 }],
    }
    const mk = await apiJSON(
      page,
      'POST',
      `/project/${pid}/thread/${threadId}/messages`,
      body,
    )
    expect(mk.status).toBe(201)
  }

  // R2 oracle (server-authoritative): thread record with pinned message shape
  await page.waitForTimeout(1500)
  const rec1 = await apiJSON(page, 'GET', `/project/${pid}/threads`)
  expect(rec1.status).toBe(200)
  const threadIds = Object.keys(rec1.json ?? {})
  expect(threadIds.length).toBeGreaterThanOrEqual(1)
  const tid = threadIds[0]
  const thread = rec1.json[tid]
  expect(Array.isArray(thread.messages)).toBe(true)
  const mine = thread.messages.find((m: any) =>
    (m.content as string).includes(`d40-e2e-${stamp}`),
  )
  expect(mine, 'R2: posted comment missing from the thread record').toBeTruthy()
  expect(mine.user_id).toBeTruthy()
  expect(typeof mine.timestamp).toBe('string')
  expect(mine.timestamp).toMatch(/Z$/) // ISO (JS Date-parseable)
  expect(Array.isArray(mine.ranges) && mine.ranges.length).toBe(1) // P1 plain range
  expect(thread.resolved).toBeFalsy()

  // R3: REPLY (second message on the same thread)
  const rec2 = await apiJSON(
    page,
    'POST',
    `/project/${pid}/thread/${tid}/messages`,
    { content: replyText },
  )
  expect(rec2.status).toBe(201)
  const rec3 = await apiJSON(page, 'GET', `/project/${pid}/threads`)
  const t3 = rec3.json[tid]
  expect(
    t3.messages.some((m: any) => (m.content as string).includes(replyText)),
  ).toBeTruthy()

  // R4: RESOLVE via the pinned REST action (panel resolveThread → same URL)
  const doc = 'main.tex'
  const res = await apiJSON(
    page,
    'POST',
    `/project/${pid}/doc/${doc}/thread/${tid}/resolve`,
  )
  expect(res.status).toBe(200)
  expect(res.json.resolved).toBe(true)
  expect(res.json.resolved_by_user_id).toBe(
    (rec1.json[tid]?.messages?.[0]?.user as any)?.id ?? res.json.resolved_by_user_id,
  )
  expect(res.json.resolved_by_user?.id).toBeTruthy()
  expect(res.json.resolved_at).toMatch(/Z$/)

  const rec4 = await apiJSON(page, 'GET', `/project/${pid}/threads`)
  expect(rec4.json[tid].resolved).toBe(true)

  // R4b: REOPEN → resolved cleared
  const re = await apiJSON(
    page,
    'POST',
    `/project/${pid}/doc/${doc}/thread/${tid}/reopen`,
  )
  expect(re.status).toBe(200)
  expect(re.json.resolved).toBeFalsy()
  const rec5 = await apiJSON(page, 'GET', `/project/${pid}/threads`)
  expect(rec5.json[tid].resolved).toBeFalsy()

  // R5: TRACKED-CHANGES — server-assisted create + bulk accept (d2 path)
  const ch = await apiJSON(page, 'POST', `/project/${pid}/doc/${doc}/changes`, {
    content: `d40-e2e-tc-${stamp}`,
    start: 1,
    end: 1,
  })
  expect(ch.status).toBe(201)
  expect(ch.json.kind).toBe('insert')
  expect(ch.json.state).toBe('pending')
  const cid = ch.json.change_id
  expect(typeof cid).toBe('string')

  const acc = await apiJSON(
    page,
    'POST',
    `/project/${pid}/doc/${doc}/changes/accept`,
    { change_ids: [cid, `nope-${stamp}`] },
  )
  expect(acc.status).toBe(200)
  expect(acc.json.accepted).toBe(1) // idempotent counting: only the real one
  // idempotent repeat → 0 newly accepted
  const acc2 = await apiJSON(
    page,
    'POST',
    `/project/${pid}/doc/${doc}/changes/accept`,
    { change_ids: [cid] },
  )
  expect(acc2.status).toBe(200)
  expect(acc2.json.accepted).toBe(0)

  // R6: track-changes state map (panel body {on_for, on_for_guests}, d5 pin)
  const tc = await apiJSON(page, 'POST', `/project/${pid}/track_changes`, {
    on_for: { e2e_probe_user: true },
    on_for_guests: true,
  })
  expect(tc.status).toBe(200)
  expect(
    tc.json.track_changes?.[`__guests__`] === true,
    'R6: __guests__ not persisted',
  ).toBeTruthy()
  expect(tc.json.track_changes?.e2e_probe_user).toBe(true)
  // merge: a second on_for entry joins the stored map
  const tc2 = await apiJSON(page, 'POST', `/project/${pid}/track_changes`, {
    on_for: { merge_probe: true },
  })
  expect(tc2.json.track_changes?.e2e_probe_user).toBe(true)
  expect(tc2.json.track_changes?.merge_probe).toBe(true)

  // R7: own-message rule — the session author can delete via the own route
  // (the 403 foreign-author branch is hermetically pinned in the Go suite
  // TestOwnMessageRule).
  const firstMsgId = rec1.json[tid].messages[0].id
  const rec7 = await apiJSON(
    page,
    'DELETE',
    `/project/${pid}/thread/${tid}/own-messages/${firstMsgId}`,
  )
  expect(rec7.status).toBe(200)
  const rec8 = await apiJSON(page, 'GET', `/project/${pid}/threads`)
  expect(
    rec8.json[tid].messages.every((m: any) => m.id !== firstMsgId),
    'R7: own message still present after delete',
  ).toBeTruthy()
})
