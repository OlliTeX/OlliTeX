import { chromium } from 'playwright';
const BASE='http://127.0.0.1:7420';
const pid='6ab73507941509df73b96a34';
const b = await chromium.launch();
const page = await b.newPage();
page.on('console', m => console.log('CON[',m.type(),']', m.text().slice(0,250)));
page.on('pageerror', e => console.log('PAGEERR', (e.message||'').slice(0,300)));
await page.addInitScript(() => {
  const origCreate = document.createElement.bind(document);
  document.createElement = function(tag, ...rest) {
    const el = origCreate(tag, ...rest);
    if (tag === 'script') {
      const origSet = el.setAttribute;
      // log when src gets set and when appended
      new MutationObserver((muts) => {
        for (const m of muts) if (m.type === 'attributes' && m.attributeName === 'src') console.log('SCRIPT SRC SET: ' + el.src);
      }).observe(el, { attributes: true });
      const origAppend = el.addEventListener;
      el.addEventListener = function(type, fn, opts) {
        console.log('SCRIPT EVT: ' + type + ' src=' + (el.src || 'inline'));
        return origAppend(type, fn, opts);
      };
    }
    return el;
  };
});
await page.goto(BASE+'/login', { waitUntil: 'domcontentloaded' });
await page.fill('input[name=email]', 'e2e-admin@e2e.test');
await page.fill('input[name=password]', 'Ol-Fixture-9x7K');
await page.click('button[type=submit]');
await page.waitForTimeout(2500);
await page.goto(BASE+'/editor/'+pid, { waitUntil: 'load' });
await page.waitForTimeout(4000);
const meta = await page.evaluate(() => {
  const q = (n) => document.querySelector('meta[name="'+n+'"]');
  return {
    ol_i18n: q('ol-i18n') ? q('ol-i18n').content.slice(0,200) : 'MISSING',
    ol_exposed: q('ol-ExposedSettings') ? q('ol-ExposedSettings').content.slice(0,120) : 'MISSING',
    ol_wsUrl: q('ol-wsUrl') ? q('ol-wsUrl').content : 'MISSING',
  };
});
console.log('META:', JSON.stringify(meta, null, 1));
console.log('cm-content?', await page.locator('.cm-content').count());
await b.close();
