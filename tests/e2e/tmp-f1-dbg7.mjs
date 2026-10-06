import { chromium } from 'playwright';
const BASE='http://127.0.0.1:7420';
const pid='6ab73507941509df73b96a34';
const b = await chromium.launch();
const page = await b.newPage();
page.on('console', m => console.log('CON[',m.type(),']', m.text().slice(0,250)));
page.on('pageerror', e => console.log('PAGEERR', (e.message||'').slice(0,300)));
const pending = new Map();
page.on('request', r => pending.set(r.url(), r.resourceType()));
page.on('requestfinished', r => pending.delete(r.url()));
page.on('requestfailed', r => { console.log('REQFAIL', r.url().slice(0,120), (r.failure()||{}).errorText); pending.delete(r.url()); });
await page.goto(BASE+'/login', { waitUntil: 'domcontentloaded' });
await page.fill('input[name=email]', 'e2e-admin@e2e.test');
await page.fill('input[name=password]', 'Ol-Fixture-9x7K');
await page.click('button[type=submit]');
await page.waitForTimeout(2500);
await page.goto(BASE+'/editor/'+pid, { waitUntil: 'load' });
await page.waitForTimeout(9000);
console.log('STILL PENDING REQUESTS:');
for (const [u,t] of pending) if(!u.startsWith('data:')) console.log('  ', t, u.slice(0,110));
const out = await page.evaluate(() => {
  const win = {};
  try {
    const arr = globalThis.webpackChunk_overleaf_web;
    const fn = (require) => {
      try {
        const m = require(70238);
        win.ok = true;
        win.keys = Object.keys(m||{}).slice(0,12);
        win.defaultType = typeof (m && m.default);
      } catch (e) {
        win.err = ((e && e.message) || '').slice(0,200) + ' :: ' + ((e && e.stack) || '').slice(0,250);
      }
    };
    arr.push([[999901], {}, fn]);
  } catch (e) {
    win.pushErr = String(e).slice(0,200);
  }
  return win;
});
console.log('ENTRY RE-REQUIRE:', JSON.stringify(out));
await b.close();
