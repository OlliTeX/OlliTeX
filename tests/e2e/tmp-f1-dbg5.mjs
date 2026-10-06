import { chromium } from 'playwright';
const BASE='http://127.0.0.1:7420';
const pid='6ab73507941509df73b96a34';
const b = await chromium.launch(); const page = await b.newPage();
page.on('console', m => console.log('CON[',m.type(),']', m.text().slice(0,300)));
page.on('pageerror', e => console.log('PAGEERR', (e.message||'').slice(0,400)));
page.on('crash', () => console.log('!! PAGE CRASH'));
await page.addInitScript(() => {
  window.addEventListener('unhandledrejection', e => console.error('UNREJ', (e.reason&&e.reason.stack)||String(e.reason)));
  window.addEventListener('error', e => console.error('WERROR', e.message, (e.error&&e.error.stack||'').slice(0,200)));
});
await page.goto(BASE+'/login', { waitUntil: 'domcontentloaded' });
await page.fill('input[name=email]', 'e2e-admin@e2e.test');
await page.fill('input[name=password]', 'Ol-Fixture-9x7K');
await page.click('button[type=submit]');
await page.waitForTimeout(2500);
await page.goto(BASE+'/editor/'+pid, { waitUntil: 'load' });
await page.waitForTimeout(10000);
const probe = await page.evaluate(() => {
  const root = document.getElementById('ide-root');
  const fiber = root ? Object.keys(root).filter(k=>k.startsWith('__react')) : null;
  const wc = globalThis.webpackChunk_overleaf_web;
  return {
    rootChildTags: root ? [...root.children].map(c=>c.className.slice(0,50)) : null,
    fiberKeys: fiber,
    webpackChunkType: wc ? Array.isArray(wc) ? 'array len '+wc.length : typeof wc : 'MISSING',
    wcEntries: wc && Array.isArray(wc) ? wc.map(w=>Array.isArray(w[0])?w[0].join(','):(w[0]||'rt')).slice(0,12) : null,
    winKeys: Object.keys(window).filter(k=>/ol|OL|webpack/i.test(k)).slice(0,15),
  };
});
console.log('PROBE:', JSON.stringify(probe, null, 1));
await b.close();
