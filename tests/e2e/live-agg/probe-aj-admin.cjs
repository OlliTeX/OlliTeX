const H = require('./harness.cjs')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  // /admin-settings landing — collect sidebar section list
  await page.goto(H.BASE + '/admin-settings', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(3000)
  const landing = await page.evaluate(() => {
    const links = Array.from(document.querySelectorAll('a[href^="/admin-settings/"]')).map(a => a.getAttribute('href') + ' :: ' + (a.textContent || '').trim())
    const txt = (document.body.innerText || '')
    return {
      links: links.slice(0, 60),
      hasActiveProjects: /active projects/i.test(txt),
      hasInstanceStats: /instance statistics|grafana/i.test(txt),
      hasOverview: /overview|activity/i.test(txt),
      hasPythonRunner: /python runner/i.test(txt),
      hasAiPrompts: /ai prompts|prompts/i.test(txt)
    }
  })
  console.log(JSON.stringify(landing, null, 1))
  // Open the specific sections and check for data/errors (AJ-3).
  for (const [name, re] of [['active-projects', /active|projects/i], ['instance-stats', /instance|grafana/i]]) {
    const link = page.locator(`a[href^="/admin-settings/"]`, { hasText: re }).first()
    if ((await link.count()) === 0) { console.log(name, ': NO LINK'); continue }
    const href = await link.getAttribute('href')
    await page.goto(H.BASE + href, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(4000)
    const st = await page.evaluate(() => {
      const txt = (document.body.innerText || '')
      return {
        href: location.pathname + location.hash,
        statusText: /couldn't load|something went wrong|error/i.test(txt),
        preview: txt.slice(0, 160)
      }
    })
    console.log(name, JSON.stringify(st))
  }
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
