// Diagnosis probe (NOT committed): is the psintern login page booting?
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch({ args: ['--no-sandbox'] });
  const page = await (await browser.newContext()).newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push('PAGEERROR: ' + e.message.split('\n')[0]));
  page.on('console', (m) => { if (m.type() === 'error') errors.push('CONSOLE: ' + m.text().slice(0, 200)); });
  page.on('requestfailed', (r) => errors.push('REQFAIL: ' + r.url() + '  ' + (r.failure() || {}).errorText));
  const t0 = Date.now();
  const resp = await page.goto('https://psintern.neuro.uni-bremen.de/login', { waitUntil: 'domcontentloaded', timeout: 60000 });
  console.log('goto status:', resp && resp.status());
  await page.waitForTimeout(9000);
  const state = await page.evaluate(() => {
    const auth = document.getElementById('auth-root');
    return {
      authChildren: auth ? auth.children.length : -1,
      bodyLen: document.body.innerHTML.length,
      computedBodyBg: getComputedStyle(document.body).backgroundColor,
      h1s: [...document.querySelectorAll('h1,h2')].map((h) => h.textContent.trim()).slice(0, 3),
      anyButton: !!document.querySelector('button'),
      fontsLoaded: (document.fonts ? document.fonts.status : 'n/a'),
    };
  });
  console.log('after 9s:', JSON.stringify(state, null, 2));
  console.log('errors:', errors.slice(0, 12));
  await page.screenshot({ path: '/tmp/login_shot.png', fullPage: true });
  console.log('shot -> /tmp/login_shot.png (total ms:', Date.now() - t0, ')');
  await browser.close();
})().catch((e) => { console.error('PROBE FAIL:', e.message); process.exit(1); });
