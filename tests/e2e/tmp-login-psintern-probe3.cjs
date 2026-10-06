const { chromium } = require('playwright');
const https = require('https');
(async () => {
  const browser = await chromium.launch({ args: ['--no-sandbox'] });
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  const statuses = [];
  const failed = [];
  page.on('response', (r) => {
    const u = r.url();
    if (/\.(css|js)(\?|$)/.test(u)) statuses.push(r.status() + ' ' + u.split('/.neuro.uni-bremen.de')[0].split('/psintern')[0].replace('https://psintern.neuro.uni-bremen.de', ''));
  });
  page.on('requestfailed', (r) => failed.push(r.url() + ' :: ' + (r.failure() || {}).errorText));
  const resp = await page.goto('https://psintern.neuro.uni-bremen.de/login', { waitUntil: 'networkidle', timeout: 60000 });
  await page.waitForTimeout(4000);
  const st = await page.evaluate(() => {
    const btn = [...document.querySelectorAll('button')].find((b) => /log ?in/i.test(b.textContent || ''));
    return {
      status: null,
      btnBg: btn ? getComputedStyle(btn).backgroundColor : null,
      btnPadded: btn ? getComputedStyle(btn).padding : null,
      btnRounded: btn ? getComputedStyle(btn).borderRadius : null,
      bodyFont: getComputedStyle(document.body).fontFamily.slice(0, 50),
      formVisible: !!document.querySelector('form'),
      spinner: !!document.querySelector('[class*="loader"], [class*="spinner"]'),
    };
  });
  console.log('--- button/computed ---');
  console.log(JSON.stringify(st, null, 2));
  console.log('--- asset waterfall (css/js) ---');
  const counts = {};
  statuses.forEach((s) => { const c = s.split(' ')[0]; counts[c] = (counts[c] || 0) + 1; });
  console.log('status counts:', JSON.stringify(counts));
  const not200 = statuses.filter((s) => !s.startsWith('200'));
  console.log('non-2xx assets:', not200.length ? not200.join('\n') : 'NONE');
  console.log('requestfailed:', failed.length ? failed.join('\n') : 'NONE');
  await browser.close();
})().catch((e) => { console.error('FAIL', e.message); process.exit(1); });
