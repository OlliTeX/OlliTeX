const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch({ args: ['--no-sandbox'] });
  const page = await (await browser.newContext({ viewport: { width: 1280, height: 800 } })).newPage();
  await page.goto('https://psintern.neuro.uni-bremen.de/login', { waitUntil: 'domcontentloaded', timeout: 60000 });
  await page.waitForTimeout(7000);
  const st = await page.evaluate(() => {
    const btn = [...document.querySelectorAll('button')].find((b) => /log ?in/i.test(b.textContent || '')) || document.querySelector('button');
    const card = document.querySelector('[class*="mantine"], .application-page > *');
    const inp = document.querySelector('input[type="text"], input[type="email"]');
    return {
      btnBg: btn ? getComputedStyle(btn).backgroundColor : null,
      btnFont: btn ? getComputedStyle(btn).fontFamily.slice(0, 40) : null,
      btnColor: btn ? getComputedStyle(btn).color : null,
      bodyFont: getComputedStyle(document.body).fontFamily.slice(0, 60),
      hasMantineCard: !!card,
      inputPresent: !!inp,
      inputBorder: inp ? getComputedStyle(inp).borderStyle : null,
      title: document.title,
    };
  });
  console.log(JSON.stringify(st, null, 2));
  await browser.close();
})().catch((e) => { console.error('FAIL', e.message); process.exit(1); });
