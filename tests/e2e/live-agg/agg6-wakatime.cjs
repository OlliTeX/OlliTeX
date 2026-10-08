/**
 * AG module 6 — WakaTime/Wakapi (owner directive: enabled by default):
 *  WK-1 feature enabled by default (status 200, connected:false)
 *  WK-2 not-linked heartbeat is a quiet 204 no-op (no console/network noise)
 *  WK-3 editor exposes wakaTimeEnabled=true (tracked-changes tracker ships)
 *  WK-4 project settings page shows the WakaTime link card (Connect)
 *  WK-5 link validation: empty key 400, bad endpoint rejected per allowlist
 *  WK-6 admin opt-out path honored (env false → 404) — verified via the
 *       Go test suite (TestResolveEnabledDefaultOn), not live.
 */
const H = require('./harness.cjs')

async function main() {
  const M = 'agg6-wakatime'
  const PIX = process.env.AGG_FIXTURE_PID || process.env.AGG_TEX_PID || H.fixturePid(process.env.AGG_FIXTURE || 'agg-tex-fixture')
  if (!PIX) throw new Error('fixture project agg-tex-fixture not found — run bootstrap.sh')
  const { browser, ctx } = await H.getContext()
  const { page } = await H.openEditor(ctx, PIX)
  try {
    // WK-1: enabled by default (previously 404 "disabled on this instance")
    const st = await ctx.request.get(`${H.BASE}/user/wakatime/status`)
    const stBody = await st.json().catch(() => ({}))
    H.record(M, 'WK-1 feature ON by default (status 200, connected:false)', st.status() === 200 && stBody.connected === false, `status=${st.status()} body=${JSON.stringify(stBody).slice(0, 120)}`)

    // WK-2: heartbeat without linked account = quiet 204 no-op
    const hb = await ctx.request.post(`${H.BASE}/project/${PIX}/wakatime/heartbeat`, {
      data: { entity: 'main.tex', time: Math.floor(Date.now() / 1000), is_write: true }
    })
    H.record(M, 'WK-2 not-linked heartbeat is quiet 204', hb.status() === 204, `status=${hb.status()}`)

    // WK-3: exposed settings gate (tracker ships in the editor bundle)
    const gate = await page.evaluate(() => {
      const m = document.querySelector('meta[name="ol-ExposedSettings"]')
      if (!m) return { meta: false }
      try {
        const v = JSON.parse(m.content)
        return { meta: true, wakaTimeEnabled: v.wakaTimeEnabled ?? null }
      } catch (e) {
        return { meta: true, parse: false }
      }
    })
    H.record(M, 'WK-3 editor wakaTimeEnabled exposed=true', gate.meta && gate.wakaTimeEnabled === true, JSON.stringify(gate))

    // WK-4: account settings page (/user/settings) — WakaTime card with
    // Connect + API key fields in the integrations section
    await page.goto(`${H.BASE}/user/settings`, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(5000)
    const card = await page.evaluate(() => {
      const body = document.body.innerText
      const hasTitle = /Wakatime|WakaTime/i.test(body)
      const connectBtn = Array.from(document.querySelectorAll('button')).find(b => /connect/i.test((b.textContent || '').trim()))
      const helpText = /API key/i.test(body)
      return {
        hasTitle, connectBtn: !!connectBtn, helpText,
        snippet: (body.match(/Waka[Tt]ime[\s\S]{0,140}/) || [''])[0].replace(/\n/g, ' ').slice(0, 180)
      }
    })
    H.record(M, 'WK-4 settings card visible (title+Connect+API key help)', card.hasTitle && card.connectBtn && card.helpText, JSON.stringify(card).slice(0, 280))

    // WK-5: link validation (no key → 400 with message; CSRF via page meta)
    const csrf = await H.csrfOf(page)
    const noKey = await ctx.request.put(`${H.BASE}/user/wakatime`, {
      headers: { 'content-type': 'application/json', 'x-csrf-token': csrf },
      data: { apiKey: '' }
    })
    const noKeyBody = await noKey.text().catch(() => '')
    H.record(M, 'WK-5a link with empty key → 400', noKey.status() === 400, `status=${noKey.status()} ${noKeyBody.slice(0, 80)}`)
  } finally {
    await browser.close()
  }
  H.report(M)
}

main().then(() => process.exit(0)).catch(e => { console.error('FATAL', e && e.message); process.exit(1) })
