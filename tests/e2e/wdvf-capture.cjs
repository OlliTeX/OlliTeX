/* WDV-F definitive capture (owner-gated): load the owner's psintern project
   editor as the admin test user and collect (a) client console errors,
   (b) pageerrors, (c) failing network responses (>=400 or JSON-parse
   suspects). Read-only: no mutations after login. */
const { chromium } = require('playwright');

const BASE = 'https://psintern.neuro.uni-bremen.de';
const EMAIL = process.env.EW_EMAIL || '';
const PASS = process.env.EW_PASS || '';
const PROJECT = '6a900f391f82ca1771fbc873';

(async () => {
  const browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  const consoleErr = [];
  const pageErr = [];
  const badNet = [];
  page.on('console', m => { if (m.type() === 'error') consoleErr.push(m.text().slice(0, 400)); });
  page.on('pageerror', e => pageErr.push(String(e).slice(0, 400)));
  page.on('response', async r => {
    const s = r.status();
    const url = r.url();
    if (s >= 400) {
      let body = '';
      try { body = (await r.text()).slice(0, 200); } catch (e) { body = '<unreadable>'; }
      badNet.push(`${s} ${url} :: ${body.replace(/\n/g, ' ')}`);
    }
  });

  // 1. login via the real UI (form), exactly like a user
  await page.goto(BASE + '/login');
  let emailSel = null;
  for (const s of ['input[type=email]', 'input[name="email"]', 'input[name="login"]', 'input']) {
    if (await page.locator(s).count() > 0) { emailSel = s; break; }
  }
  if (emailSel) {
    await page.locator(emailSel).first().fill(EMAIL);
    await page.locator('input[type=password]').first().fill(PASS);
    await page.locator('button[type=submit]').first().click();
    await page.waitForLoadState('domcontentloaded');
  }
  const loggedIn = await page.evaluate(() => !!document.cookie.match(/overleaf\.sid|sid=/));
  console.log('after-login url:', page.url());
  console.log('loggedIn(cookie):', loggedIn);

  // 2. open the owner's project editor
  await page.goto(`${BASE}/project/${PROJECT}`);
  try { await page.waitForLoadState('domcontentloaded', { timeout: 30000 }); } catch (e) {}
  await page.waitForTimeout(15000);
  console.log('project url now:', page.url());
  console.log('title:', await page.title());

  // 3. also touch /hub (reported crash site)
  await page.goto(BASE + '/hub');
  await page.waitForTimeout(8000);
  console.log('hub url now:', page.url());

  console.log('\n=== CONSOLE ERRORS (' + consoleErr.length + ') ===');
  consoleErr.slice(0, 20).forEach(e => console.log('  -', e));
  console.log('\n=== PAGE ERRORS (' + pageErr.length + ') ===');
  pageErr.slice(0, 10).forEach(e => console.log('  -', e));
  console.log('\n=== FAILED NET (' + badNet.length + ') ===');
  badNet.slice(0, 15).forEach(e => console.log('  -', e));

  const verdict = (consoleErr.length === 0 && pageErr.length === 0) ? 'CLEAN' : 'ERRORS';
  console.log('\nVERDICT:', verdict);
  await browser.close();
  process.exit(0);
})().catch(e => { console.error('CAPTURE-FAIL:', String(e).slice(0, 300)); process.exit(1); });
