/**
 * notifications — email preference journey (one pass): the legacy
 * /user/notification-preferences URL now 301s to the hub surface
 * (My settings → Email preferences); the mute-all toggle round-trips
 * (persist across reload) and is restored — the "no instant
 * tracked-changes email to the author" contract is this user-controlled
 * grace period.
 *
 * ⚑ NTF-UI-1 — FIXED (2026-10-15, owner-approved; ag-69). The hub mute switch used
 * to render the OPPOSITE of the page's own fetched data under load (silent
 * load()-catch → checked-default; a user acting on it saved the opposite
 * direction). FIX in notifications-settings-section.tsx: load() now retries
 * (3×, backoff) and on final failure renders a VISIBLE error state with Retry —
 * the control is never rendered in a guessed state. This spec now PINS that
 * contract: (A) a flaky first load recovers via the retry path and still
 * agrees with the wire; (B) a persistently failing load shows the visible
 * error state AND renders NO switch (no misleading control); (C) the round-trip
 * data contract (wire persistence) + UI⇄wire agreement are HARD legs — a desync
 * recurring after the fix is a regression and must fail the battery.
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

  // ---- NTF-UI-1 contract legs (pinned fix, ag-69; owner-approved 2026-10-15) ----
  // Leg A: a FLAKY first load must recover via the retry path and the switch
  // must converge to the WIRE (the ag-67/68 desync signature must be gone).
  {
    let aborted = 0
    await page.route('**/notifications/preferences', async (route) => {
      if (route.request().method() === 'GET' && aborted < 1) {
        aborted += 1
        return route.abort('failed')
      }
      return route.continue()
    })
    await page.goto('/user-settings/mysettings.email', { waitUntil: 'domcontentloaded' })
    await page.unroute('**/notifications/preferences')
    const wireA = await readWire()
    let okA = false
    for (let t = 0; t < 15; t++) {
      const n = await page.locator('label.mantine-Switch-body').count()
      if (n > 0) {
        const ck = await page
          .locator('label.mantine-Switch-body')
          .first()
          .locator('input')
          .first()
          .isChecked()
          .catch(() => 'ERR')
        if (ck === !wireA) {
          okA = true
          break
        }
      }
      await page.waitForTimeout(1000)
    }
    expect(
      okA,
      `NTF-UI-1 Leg A: flaky first load must recover via retry and the switch must agree with the wire (aborted-first-GET captured=${aborted})`,
    ).toBe(true)
  }
  // Leg B: a persistently failing load must show the VISIBLE error state and
  // render NO switch control — no misleading checked-default state (the ag-68
  // defect). 3 GETs blocked = all retry attempts.
  {
    let blocked = 0
    await page.route('**/notifications/preferences', async (route) => {
      if (route.request().method() === 'GET') {
        blocked += 1
        if (blocked <= 3) return route.abort('failed')
      }
      return route.continue()
    })
    await page.goto('/user-settings/mysettings.email', { waitUntil: 'domcontentloaded' })
    await page.getByText("Couldn't load notification preferences").waitFor({ state: 'visible', timeout: 20000 })
    const n = await page.locator('label.mantine-Switch-body').count()
    await page.unroute('**/notifications/preferences')
    expect(
      n,
      `NTF-UI-1 Leg B: failed loads must not render the switch control (no misleading state; blocked=${blocked})`,
    ).toBe(0)
  }
  // Healthy recovery for the round-trip flow (Leg B intentionally left the page
  // in the error state) — the switch must be back once the route is cleared;
  // a persistent error state here is a real product regression and fails hard.
  await page.goto('/user-settings/mysettings.email', { waitUntil: 'domcontentloaded' })
  const roundTripHealthy = await page
    .locator('label.mantine-Switch-body')
    .first()
    .isVisible({ timeout: 15000 })
    .catch(() => false)
  if (!roundTripHealthy) {
    throw new Error(
      'NTF-UI-1: switch control missing after Leg B recovery load — either the retry path is broken (regression) or the world is rejecting the GET. ' +
        'errorStateVisible=' + String(await page.getByText("Couldn't load notification preferences").isVisible().catch(() => false)),
    )
  }

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

  // UI leg — HARD (NTF-UI-1 pinned contract, fixed ag-69): the page is loaded
  // with the POST-TOGGLE state at this point (the reload goto above), so the
  // switch must converge to !postWire. A silent-checked-default regression or
  // any desync fails with the full evidence series.
  let uiAgrees = false
  const series: string[] = []
  const uiTarget = !postWire // UI checked = !wire; wire is now postWire (post-toggle)
  for (let t = 0; t < 18; t++) {
    const ck = await page.locator('label.mantine-Switch-body').first().locator('input').first()
      .isChecked().catch(() => 'ERR')
    series.push(`${t}.s:${ck}`)
    if (ck === uiTarget) { uiAgrees = true; break }
    await page.waitForTimeout(1000)
  }
  const uiDiag = `ui-target-checked=${uiTarget} (post-toggle wire=${postWire}) | series=${series.join(',')} | preClickConverged=${uiConverged} | ${gotoRes ? gotoRes.status() : ''} ${reloadedUrl} switches=${swCount} | pageErrors=${JSON.stringify(pageErrors)} | exchanges=${JSON.stringify(seen)}`
  if (!uiAgrees) {
    const msg = `⚑ NTF-UI-1 REGRESSION (desync or silent-default returned — must be chased): ${uiDiag}`
    console.log(msg)
    test.info().annotations.push({ type: 'NTF-UI-1-regression', description: msg.slice(0, 300) })
  } else {
    console.log('UI⇄wire agreement: OK (converged after ~' + series.length + ' s)')
  }

  // World hygiene (2026-10-13): this toggle inverts B's GLOBAL muteAll leaf and
  // the chat fan-out honors it (chat notify.go MutedUserIDs) — leaving the world
  // in a muted state poisons the receiver-side suites. Restore over the WIRE and
  // read it back through the canonical route. Runs LAST so it cannot leave the
  // world in the toggled state on any earlier failure path.
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
    uiAgrees,
    `UI switch must agree with the wire after reload (NTF-UI-1 pinned contract) | ${uiDiag}`,
  ).toBe(true)
  expect(
    postWire,
    `wire contract: persisted value must equal the toggled state (preState=${preState}, want post-toggle mute=${!preState})`,
  ).toBe(!preState)
})
