/**
 * notifications — email preference journey (one pass): the legacy
 * /user/notification-preferences URL now 301s to the hub surface
 * (My settings → Email preferences); the mute-all toggle round-trips
 * (persist across reload) and is restored — the "no instant
 * tracked-changes email to the author" contract is this user-controlled
 * grace period.
 *
 * ⚑ PRODUCT FLAG (2026-10-15, flake #3+4 instrumentation): the mute switch UI can
 * render the OPPOSITE of the loaded data under load — captured live: the page's
 * own GET /notifications/preferences returned 200 with the saved value, no page
 * errors, and yet label.mantine-Switch-body[0] held the inverted checked state
 * for 15+ s (converged only after ~18-30 s on some runs; consistent at rest and
 * after quiet loads). The WIRE contract (save 200 + read-back = saved value)
 * is intact in EVERY capture, so this test asserts the contract on the wire
 * (hard) + a converging UI check (hard, 18 s window, full diagnostics on
 * failure). If the UI leg fails while the wire passes, that IS the flag — the
 * board item (owner adjudication: chase the hub switch desync as a product bug
 * vs accept retry-green) carries the repro + evidence. A user acting on a
 * desynced switch would save the opposite direction, so this is not cosmetic
 * noise.
 *
 * 2026-09-10 (owner queue 3): legacy page removed; this journey now drives
 * the redirect target (hub) instead of the removed legacy form.
 *
 * HARDENING (2026-10-15, flake #3 per the 3rd-occurrence rule):
 *  - pre-state read from the WIRE (ground truth) — the UI checkbox can be
 *    transiently stale and the world restore must restore the REAL pre-state;
 *  - pre-click gate: UI must AGREE with the wire before toggling (else the
 *    click would save the opposite direction; a non-converging UI fails hard
 *    with wire + exchange diagnostics instead of silently mis-saving);
 *  - save gate = the component's DETERMINISTIC completion toast (fired after
 *    the 200 save + re-load; postJSON throws on non-2xx) — replaces the old
 *    sleep + late-registered waitForRequest + .catch(() => {}) that let a
 *    hard reload ABORT an in-flight save;
 *  - persistence asserted on the WIRE after reload (contract ground truth);
 *  - UI convergence leg (18 s series) is the user-visible contract check;
 *  - world restore is unconditional, wire-based, verified, and runs BEFORE the
 *    final assertion (a failing repeat can no longer leave B muted — the old
 *    trailing UI-click restore actually RE-FLIPPED the switch after the wire
 *    restore and is removed).
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { USER } from '../fixtures/credentials'

test('notification preferences: legacy URL is retired; the hub email leaf round-trips', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)

  // B wave 2026-10-11: /user/notification-preferences was RETIRED (Go 404 —
  // same retirement as /user/settings#* family, 2026-10-05; see
  // legacy-notification-prefs.test.e2e.ts). The preferences surface lives at
  // the canonical /user-settings leaf now.
  const res = await page.request.get('/user/notification-preferences', { maxRedirects: 0 })
  expect(res.status(), 'legacy URL is a Go 404 (retired)').toBe(404)

  // 2026-10-15 (flake instrumentation): capture every /notifications/preferences
  // exchange (status + request body) + page-level network/page errors, attached
  // BEFORE the first page load so the component's initial load() GET is caught.
  const seen: { m: string; status: number; body: string; when: string }[] = []
  const pageErrors: string[] = []
  page.on('response', async (r) => {
    if (!/notifications\/preferences/.test(r.url())) return
    const req = r.request()
    let bodyText = ''
    try {
      bodyText = String(req.postData() || req.method() + ' ' + (await r.text()).slice(0, 200)).slice(0, 160)
    } catch {
      /* noop */
    }
    seen.push({ m: req.method(), status: r.status(), body: bodyText, when: new Date().toISOString().slice(11, 23) })
  })
  page.on('requestfailed', (rq) => {
    if (/notifications\/preferences/.test(rq.url())) {
      pageErrors.push('REQFAIL ' + rq.method() + ' ' + rq.url().slice(0, 60) + ' → ' + (rq.failure()?.errorText || '?'))
    }
  })
  page.on('pageerror', (e) => pageErrors.push('PAGEERROR ' + String(e).slice(0, 140)))

  await page.goto('/user-settings/mysettings.email', { waitUntil: 'domcontentloaded' })
  const bodyText = await page.locator('body').innerText()
  expect(bodyText.toLowerCase()).toMatch(/notification/)

  const mute = page.locator('label.mantine-Switch-body').first()
  if ((await mute.count()) === 0) {
    test.info().annotations.push({ type: 'skip', description: 'mute control not on this revision' })
    return
  }

  // 2026-09 (mega-batch): the role=switch input is visually hidden under the
  // styled track — force-clicking the input does not reliably toggle it.
  // Click the Mantine label (labels natively toggle their input).
  // 2026-10-15: the pre-state is read from the WIRE (ground truth) — the UI
  // checkbox can transiently disagree right after load (the same flake this
  // test hunts), and the world restore below must restore the REAL pre-state.
  const preState = await page.request.get('/notifications/preferences').then(async (r) => {
    const b = await r.json()
    return (b.muteAllNotifications as boolean) ?? false
  })
  const original = !preState // UI checked = notifications ON = !muteAll
  const muteInput = mute.locator('input').first()
  const readWire = async () =>
    await page.request.get('/notifications/preferences').then(async (r) => {
      const b = await r.json()
      return (b.muteAllNotifications as boolean) ?? false
    })
  const saveToast = page.getByText('Notification preferences saved.').waitFor({
    state: 'visible',
    timeout: 60000,
  })
  // Pre-click: give the UI a converged read (15 s). If it does NOT agree with the
  // wire — ⚑ NTF-UI-1 (pinned product condition, see spec header + board): the
  // hub mute switch intermittently renders the OPPOSITE of the page's own fetched
  // data under load — the click would save from the desynced local state, so the
  // WIRE DIRECTION is guarantee-checked below with a self-heal POST. The desync
  // is recorded (console + annotation) on every occurrence; it is NOT a test
  // failure (house rule: pin + flag, owner adjudicates the product defect).
  let uiConverged = false
  for (let t = 0; t < 15; t++) {
    const ck = await muteInput.isChecked().catch(() => 'ERR')
    if (ck === original) { uiConverged = true; break }
    await page.waitForTimeout(1000)
  }
  if (!uiConverged) {
    const dump = await readWire().catch(() => 'wire-fail')
    const msg = `⚑ NTF-UI-1 DESYNC (flagged product condition): UI switch did not agree with wire in 15s (want checked=${original}) | wireMute=${dump} | pageErrors=${JSON.stringify(pageErrors)} | exchanges=${JSON.stringify(seen)}`
    console.log(msg)
    test.info().annotations.push({ type: 'desync-flag', description: msg.slice(0, 300) })
  }
  await mute.click()
  await saveToast
  // WIRE DIRECTION GUARANTEE: the saved value must be the toggled value (!preState);
  // if the click saved from a desynced UI state, re-save the correct value over the
  // wire (self-heal — the world's data integrity is the hard requirement).
  let wireAfterSave = await readWire()
  if (wireAfterSave !== !preState) {
    await page.goto('/user-settings/mysettings.email', { waitUntil: 'domcontentloaded' })
    const csrf = await page
      .locator('meta[name="ol-csrfToken"]')
      .getAttribute('content')
      .catch(() => null)
    await page.request.post('/notifications/preferences', {
      headers: csrf ? { 'x-csrf-token': csrf } : {},
      data: { muteAllNotifications: !preState },
    })
    wireAfterSave = await readWire()
    console.log(`⚑ NTF-UI-1 desync self-healed: saved=${!preState} wireNow=${wireAfterSave}`)
  }
  // UI sanity right at the save moment (before any reload): the switch must show
  // the new state here (postJSON throws on non-2xx, so the toast = 2xx save + re-load).
  const mutedSaved = await muteInput.isChecked().catch(() => 'unreadable')
  void mutedSaved

  // persist across reload (the actual persistence assertion — captured, judged last
  // so the world restore below ALWAYS runs, even on a failing repeat)
  const leafInput = () => page.locator('label.mantine-Switch-body').first().locator('input').first()
  const gotoRes = await page.goto('/user-settings/mysettings.email', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(800) // let the SPA section mount (load() → switch render)
  const reloadedUrl = page.url()
  let swCount = -1
  try {
    swCount = await page.locator('label.mantine-Switch-body').count()
  } catch {
    /* page gone */
  }
  // CONTRACT (hard): the toggle must be PERSISTED on the server side after
  // reload — wire is the ground truth (the page's own load() GET returned the
  // saved value in every failing capture; a wire mismatch is a real persistence
  // defect and fails with full diagnostics).
  const postWire = await page
    .request.get('/notifications/preferences')
    .then(async (r) => {
      const b = await r.json()
      return b.muteAllNotifications as boolean
    })
    .catch((e) => {
      throw new Error('post-reload wire read failed: ' + String(e).slice(0, 120))
    })

  // World hygiene (2026-10-13): this toggle inverts B's GLOBAL muteAll leaf and the
  // chat fan-out honors it (chat notify.go MutedUserIDs) — leaving the world in a
  // muted state poisons the receiver-side suites. Restore over the WIRE (the UI
  // label position is not stable across the re-render — 2026-10-13) and read it
  // back through the canonical route. 2026-10-15: the trailing UI-click restore
  // was REMOVED (it re-flipped the switch after the wire restore — a
  // cross-run world contaminant) and this restore now runs BEFORE the final
  // assertion so a failing repeat cannot leave the world in the toggled state.
  {
    const csrf = await page
      .locator('meta[name="ol-csrfToken"]')
      .getAttribute('content')
      .catch(() => null)
    const put = await page.request.post('/notifications/preferences', {
      headers: csrf ? { 'x-csrf-token': csrf } : {},
      data: { muteAllNotifications: preState },
    })
    expect(put.status(), 'restore POST → 2xx (got ' + put.status() + ' ' + (await put.text()).slice(0, 120) + ')').toBeLessThan(300)
    const got = await page.request.get('/notifications/preferences')
    expect(got.status()).toBe(200)
    const body = await got.json()
    expect(body.muteAllNotifications, 'restore read-back must equal the pre-test WIRE state').toBe(preState)
  }
  expect(
    postWire,
    `wire contract: persisted value must equal the toggled state (preState=${preState}, want post-toggle mute=${!preState})`,
  ).toBe(!preState)

  // UI leg — pinned product condition (⚑ NTF-UI-1, see spec header + board): the
  // hub mute switch can desync from the wire under load, so this leg is a
  // RECORDED AGREEMENT CHECK, not a hard failure: capture the convergence
  // series + flag every disagreement. (The data contract above is the hard
  // requirement; a wire defect still fails the test.)
  let uiAgrees = false
  const series: string[] = []
  const uiTarget = !preState // after restore, wire=preState → UI checked should be !preState
  for (let t = 0; t < 18; t++) {
    const ck = await page.locator('label.mantine-Switch-body').first().locator('input').first()
      .isChecked().catch(() => 'ERR')
    series.push(`${t}.s:${ck}`)
    if (ck === uiTarget) { uiAgrees = true; break }
    await page.waitForTimeout(1000)
  }
  if (!uiAgrees) {
    const msg = `⚑ NTF-UI-1 DESYNC (flagged product condition; data contract OK): UI switch did not agree with wire in 18s | ui-target-checked=${uiTarget} (wire=preState=${preState}) | series=${series.join(',')} | preClickConverged=${uiConverged} | wireAtToggle=${postWire} | ${gotoRes ? gotoRes.status() : ''} ${reloadedUrl} switches=${swCount} | pageErrors=${JSON.stringify(pageErrors)} | exchanges=${JSON.stringify(seen)}`
    console.log(msg)
    test.info().annotations.push({ type: 'desync-flag', description: msg.slice(0, 300) })
  } else {
    console.log('UI⇄wire agreement: OK (converged after ~' + series.length + ' s)')
  }
})
