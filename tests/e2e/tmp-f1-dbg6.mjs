import { chromium } from 'playwright';
const BASE='http://127.0.0.1:7420';
const b = await chromium.launch(); const page = await b.newPage();
page.on('console', m => console.log('CON[',m.type(),']', m.text().slice(0,250)));
page.on('pageerror', e => console.log('PAGEERR', (e.message||'').slice(0,300)));
await page.goto(BASE+'/login', { waitUntil: 'domcontentloaded' });
await page.fill('input[name=email]', 'e2e-admin@e2e.test');
await page.fill('input[name=password]', 'Ol-Fixture-9x7K');
await page.click('button[type=submit]');
await page.waitForTimeout(4000);
console.log('URL after login:', page.url());
// hub is now loaded
await page.waitForTimeout(8000);
const probe = await page.evaluate(() => {
  const t = document.body.innerText.slice(0,200).replace(/\n/g,' | ');
  const nav = document.querySelector('nav, [class*=nav]');
  return { title: document.title, bodyText: t, hasNav: !!nav, wcLen: (globalThis.webpackChunk_overleaf_web||[]).length };
});
console.log('HUB PROBE:', JSON.stringify(probe));
await b.close();
