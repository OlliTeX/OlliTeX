const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch({ args: ['--no-sandbox'] });
  const page = await (await browser.newContext({ viewport: { width: 1280, height: 900 } })).newPage();
  await page.goto('https://psintern.neuro.uni-bremen.de/login', { waitUntil: 'networkidle', timeout: 60000 });
  await page.waitForTimeout(5000);
  const info = await page.evaluate(() => {
    const btns = [...document.querySelectorAll('button')];
    const log = btns.find((b) => /log ?in/i.test(b.textContent || ''));
    const all = btns.map((b) => ({ text: (b.textContent || '').trim().slice(0, 20), bg: getComputedStyle(b).backgroundColor, radius: getComputedStyle(b).borderRadius }));
    const loginCss = log ? [...document.styleSheets].flatMap((ss) => {
      let rules = []; try { rules = ss.cssRules || []; } catch (e) { return []; }
      const out = [];
      for (const r of rules) { if (r.selectorText && (r.selectorText.includes('Button') || r.selectorText.includes('button') || r.selectorText.includes('login'))) out.push(r.cssText.slice(0, 140)); }
      return out;
    }).filter((t) => /background|radius|padding/i.test(t)).slice(0, 14) : [];
    return {
      nButtons: btns.length,
      all,
      loginTag: log ? log.outerHTML.slice(0, 220) : null,
      loginClasses: log ? log.className : null,
      styleSheets: [...document.styleSheets].map((s) => (s.href || 'inline').split('/').pop()),
      bodyBg: getComputedStyle(document.body).backgroundColor,
      rootBg: (document.getElementById('auth-root') || {}).backgroundColor && getComputedStyle(document.getElementById('auth-root')).backgroundColor,
    };
  });
  console.log(JSON.stringify(info, null, 2));
  await page.screenshot({ path: '/tmp/login_shot2.png', fullPage: false });
  await browser.close();
})().catch((e) => { console.error('FAIL', e.message); process.exit(1); });
