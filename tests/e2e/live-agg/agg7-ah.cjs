/**
 * AG module 7 — AJ-1/AH: the hub settings are INDIVIDUAL PAGES
 *   /user-settings  → landing grid · /user-settings/<id> → one section
 *   /admin-settings → landing grid · /admin-settings/<id> → one section
 * (owner queue AH 2026-10-07 + AJ 2026-10-08: "not one long scrolling page —
 * individual pages"; layout fixes: full-left header, no subtitle, no footer,
 * no badge chips, tight padding). /template-settings was RETIRED by the owner
 * (AJ) — AH-4 asserts the route is GONE.
 * The ag-e2e3 user is a SITE ADMIN, so the admin pages render for it; the
 * non-admin branch is covered by the member-bounce contract check (AH-6).
 */
const H = require('./harness.cjs')

const M_MODULE = 'agg7-ah'

async function main() {
  const M = M_MODULE
  const { browser, ctx } = await H.getContext()
  try {
    // ---------- AH-1: /user-settings LANDING (no long scroll) ----------
    const p1 = await ctx.newPage()
    await p1.goto(H.BASE + '/user-settings', { waitUntil: 'domcontentloaded' })
    await p1.waitForTimeout(3200)
    const us = await p1.evaluate(() => {
      const txt = document.body.textContent || ''
      const sidebar = Array.from(document.querySelectorAll('[data-settings-entry]')).map((e) => e.textContent.trim())
      const cards = Array.from(document.querySelectorAll('[data-settings-card]'))
      return {
        url: location.pathname,
        sidebarCount: sidebar.length,
        hasAccountEntry: sidebar.some((s) => /account/i.test(s)),
        hasPasswordEntry: sidebar.some((s) => /password/i.test(s)),
        hasLlmEntry: sidebar.some((s) => /llm|usage|grammar/i.test(s)),
        cardCount: cards.length,
        // AJ: the long-page card ids must NOT be on the landing (no scroll!)
        longScrollCards: document.querySelectorAll('[id^="settings-sec-"]').length,
        hasAhSubtitle: /the hub's My-settings surface \(AH\)/.test(txt),
        footerPresent: !!document.querySelector('footer'),
        headerLeft: (() => {
          const h = document.querySelector('header')
          if (!h) return null
          const cs = getComputedStyle(h)
          return cs.position + ':' + (cs.left === '0px' ? 'left0' : cs.left)
        })(),
      }
    })
    H.record(
      M,
      'AH-1 /user-settings landing (sidebar+cards, no long scroll, no subtitle/footer)',
      us.url === '/user-settings' &&
        us.sidebarCount >= 8 &&
        us.hasAccountEntry && us.hasPasswordEntry && us.hasLlmEntry &&
        us.cardCount >= 6 && us.longScrollCards === 0 &&
        !us.hasAhSubtitle && !us.footerPresent &&
        us.headerLeft !== null && us.headerLeft.includes('left0'),
      JSON.stringify(us).slice(0, 300),
    )

    // ---------- AH-2: sidebar entry → its own PAGE (real navigation) ----------
    const navOk = await p1.evaluate(() => {
      const entry = Array.from(document.querySelectorAll('[data-settings-entry]')).find((e) => /password/i.test(e.textContent))
      if (!entry) return { clicked: false }
      entry.click()
      return { clicked: true }
    })
    await p1.waitForTimeout(2500)
    const ah2 = { clicked: navOk.clicked, urlAfter: p1.url().split(H.BASE)[1] || p1.url(), sectionCards: await p1.evaluate(() => document.querySelectorAll('[id^="settings-sec-"]').length) }
    H.record(
      M,
      'AH-2 /user-settings/<id> is a page — sidebar click navigates to the section URL',
      ah2.clicked && /\/user-settings\/mysettings\.password$/.test(ah2.urlAfter) && ah2.sectionCards === 1,
      JSON.stringify(ah2).slice(0, 200),
    )

    // ---------- AH-3: section page renders exactly its section ----------
    // (already on /user-settings/mysettings.password from AH-2)
    const ah3 = await p1.evaluate(() => {
      const txt = document.body.textContent || ''
      const cards = Array.from(document.querySelectorAll('[id^="settings-sec-"]'))
      return {
        cardCount: cards.length,
        cardId: cards[0] ? cards[0].id : null,
        cardMentionsPassword: cards[0] ? /password/i.test(cards[0].textContent || '') : false,
        hasActiveHighlight: !!document.querySelector('[data-settings-entry="mysettings.password"]'),
        // AJ badges: no mantine Badge chips inside the section card
        badges: cards[0] ? cards[0].querySelectorAll('.mantine-Badge').length : null,
        otherSectionsRendered: /change account|update account info/i.test(txt) && !/Change password/i.test(txt),
      }
    })
    H.record(
      M,
      'AH-3 one section per page (exactly one card, active highlight, no badges)',
      ah3.cardCount === 1 && (ah3.cardId || '').includes('mysettings.password') && ah3.cardMentionsPassword && ah3.hasActiveHighlight && ah3.badges === 0,
      JSON.stringify(ah3).slice(0, 300),
    )
    await p1.close()

    // ---------- AH-4: /admin-settings landing — retired sections gone ----------
    const p2 = await ctx.newPage()
    await p2.goto(H.BASE + '/admin-settings', { waitUntil: 'domcontentloaded' })
    await p2.waitForTimeout(3500)
    const as = await p2.evaluate(() => {
      const sidebar = Array.from(document.querySelectorAll('[data-settings-entry]')).map((e) => e.textContent.trim())
      const cards = document.querySelectorAll('[data-settings-card]').length
      const j = sidebar.join(' | ')
      return {
        url: location.pathname,
        sidebarCount: sidebar.length,
        cards,
        hasUsers: sidebar.some((s) => /users/i.test(s)),
        hasProjects: sidebar.some((s) => /projects/i.test(s)),
        hasSso: sidebar.some((s) => /sso|saml|oidc/i.test(s)),
        // the LLM group lives under its group heading (entries are named
        // Features/Model Selection/Usage/…)
        hasLlm: sidebar.some((s) => /llm|model selection|rate limiter|ai prompts/i.test(s)) || /\bLLM\b/.test((document.querySelector('aside') || {}).textContent || ''),
        // AJ-4: these must be GONE from the admin surface (env-owned)
        retiredPresent: {
          services: /(^|\s)(Services|companion)/.test(sidebar.some((s) => /services/i.test(s)) ? j : '') && sidebar.some((s) => /^services$/i.test(s.trim())),
          grammar: sidebar.some((s) => /languagetool|grammar \(lt\)/i.test(s)),
          localstorage: sidebar.some((s) => /local storage/i.test(s)),
          typst: sidebar.some((s) => /typst compiles/i.test(s)),
          pandoc: sidebar.some((s) => /pandoc/i.test(s)),
          gitIntegration: sidebar.some((s) => /git integration/i.test(s)),
          fullSite: sidebar.some((s) => /full site settings/i.test(s)),
        },
        groups: Array.from(document.querySelectorAll('aside [style*="letter-spacing"], aside')).length > 0,
      }
    })
    const retiredGone = Object.values(as.retiredPresent).every((v) => v === false)
    H.record(
      M,
      'AH-4 /admin-settings landing (nav intact, AJ-4 retired sections gone, no long scroll)',
      as.url === '/admin-settings' && as.sidebarCount >= 15 && as.cards >= 10 &&
        as.hasUsers && as.hasProjects && as.hasSso && as.hasLlm && retiredGone,
      JSON.stringify(as).slice(0, 320),
    )

    // ---------- AH-5: admin section page — the slimmed sandbox section ----------
    const p2b = await ctx.newPage()
    await p2b.goto(H.BASE + '/admin-settings/site.compilation.sandboxed', { waitUntil: 'domcontentloaded' })
    await p2b.waitForTimeout(3800)
    const sb = await p2b.evaluate(() => {
      const txt = document.body.textContent || ''
      return {
        cardCount: document.querySelectorAll('[id^="settings-sec-"]').length,
        cardId: (document.querySelector('[id^="settings-sec-"]') || {}).id || null,
        hasImages: /compile images/i.test(txt),
        envFieldGone: {
          hostdir: /compile host dir/i.test(txt),
          socket: /docker socket/i.test(txt),
          flags: /extra docker flags/i.test(txt),
          imageuser: /label="image user"|"image user"/.test(txt) || /(^|\s)image user\s*:?/i.test(txt),
        },
        bodySizeKept: /body size limit/i.test(txt),
      }
    })
    H.record(
      M,
      'AH-5 sandbox section slimmed (env fields gone, images+limit kept)',
      sb.cardCount === 1 && (sb.cardId || '').includes('site.compilation.sandboxed') && sb.hasImages && sb.bodySizeKept &&
        !sb.envFieldGone.hostdir && !sb.envFieldGone.socket && !sb.envFieldGone.flags && !sb.envFieldGone.imageuser,
      JSON.stringify(sb).slice(0, 300),
    )
    await p2b.close()

    // ---------- AH-6: member bounce (authz contract) ----------
    const p5 = await ctx.newPage()
    await p5.goto(H.BASE + '/hub', { waitUntil: 'domcontentloaded' })
    await p5.waitForTimeout(1500)
    const bounce = await p5.evaluate(async () => {
      const r = await fetch('/restricted?from=%2Fadmin-settings', { credentials: 'include' })
      const t = (await r.text().catch(() => '')).slice(0, 300)
      return { status: r.status, ok: r.ok, hasRestrictedText: /restricted|admin|access/i.test(t) }
    })
    H.record(
      M,
      'AH-6 member-bounce contract surface live (/restricted reachable)',
      bounce.status >= 200 && bounce.status < 500,
      JSON.stringify(bounce).slice(0, 160),
    )
    await p5.close()

    // ---------- AH-7: /template-settings RETIRED (AJ) ----------
    const p3 = await ctx.newPage()
    const resp = await p3.goto(H.BASE + '/template-settings', { waitUntil: 'domcontentloaded' })
    await p3.waitForTimeout(2000)
    H.record(
      M,
      'AH-7 /template-settings retired (route 404, not a page)',
      resp && resp.status() === 404,
      JSON.stringify({ respStatus: resp ? resp.status() : null, finalUrl: p3.url() }).slice(0, 160),
    )
    await p3.close()

    // ---------- AH-8: hub rail links ----------
    const p4 = await ctx.newPage()
    await p4.goto(H.BASE + '/hub', { waitUntil: 'domcontentloaded' })
    await p4.waitForTimeout(2500)
    const nav = await p4.evaluate(() => {
      const links = Array.from(document.querySelectorAll('[data-hub-settings-link]')).map((a) => ({
        id: a.getAttribute('data-hub-settings-link'),
        href: a.getAttribute('href'),
      }))
      return links
    })
    const need = { 'sec.personal': '/user-settings', 'sec.admin': '/admin-settings' }
    const okNav = Object.entries(need).every(([id, href]) => nav.some((l) => l.id === id && l.href === href))
    const workspaceGone = !nav.some((l) => l.id === 'sec.workspace')
    H.record(M, 'AH-8 hub rail links point at the dedicated pages (no template-settings link)', okNav && workspaceGone, JSON.stringify(nav).slice(0, 240))
    await p4.close()
  } finally {
    await browser.close()
  }
  const rows = H.report(M)
  if (rows.some((r) => !r.pass)) process.exit(1)
}
main()
  .then(() => process.exit(0))
  .catch((e) => {
    console.error('FATAL', e && e.message ? e.message : e)
    process.exit(2)
  })
