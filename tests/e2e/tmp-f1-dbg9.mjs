import { chromium } from 'playwright';
const BASE='http://127.0.0.1:7420';
const pid='6ab73507941509df73b96a34';
const b = await chromium.launch();
const page = await b.newPage();
page.on('pageerror', e => console.log('PAGEERR', (e.message||'').slice(0,250)));
await page.addInitScript(() => {
  const log = (t, d) => console.log('[HOOK] ' + t + ' ' + d);
  const origPush = Array.prototype.push;
  const wrap = (arr) => {
    const wp = arr.push.bind(arr);
    arr.push = function(...items) {
      const it = items[0];
      if (it && it.length && Array.isArray(it[0])) {
        log('CHUNK PUSH', 'ids=[' + it[0].join(',') + '] modules=[' + Object.keys(it[1]||{}).slice(0,6).join(',') + '] rt=' + (typeof it[2]));
      }
      return wp(...items);
    };
    return arr;
  };
  wrap(globalThis.webpackChunk_overleaf_web || (globalThis.webpackChunk_overleaf_web = []));
  const origCreate = document.createElement.bind(document);
  document.createElement = function(tag, ...rest) {
    const el = origCreate(tag, ...rest);
    if (String(tag).toLowerCase() === 'script') {
      const obs = new MutationObserver(() => {});
      const origSet = el.setAttribute.bind(el);
      el.setAttribute = function(name, val) {
        if (name === 'src') log('SCRIPT SRC', val + ' (len=' + (val||'').length + ')');
        return origSet(name, val);
      };
      const origAdd = el.addEventListener.bind(el);
      el.addEventListener = function(type, fn, opts) {
        if (type === 'load' || type === 'error' || type === 'script' + 'load') {
          origAdd(type, function(...a) { log('SCRIPT EVT ' + type + ' for ' + (el.src||'inline'), el.currentSrc || ''); return fn && fn.apply(el, a); }, opts);
        } else {
          return origAdd(type, fn, opts);
        }
      };
    }
    return el;
  };
  // also hook the global array in case a different one appears
  Object.defineProperty(globalThis, 'webpackChunk_overleaf_web', {
    configurable: true,
    get() { return this.__wc || (this.__wc = []); },
    set(v) { this.__wc = wrap(v); }
  });
});
await page.goto(BASE+'/login', { waitUntil: 'domcontentloaded' });
await page.fill('input[name=email]', 'e2e-admin@e2e.test');
await page.fill('input[name=password]', 'Ol-Fixture-9x7K');
await page.click('button[type=submit]');
await page.waitForTimeout(2500);
console.log('===== EDITOR PAGE =====');
await page.goto(BASE+'/editor/'+pid, { waitUntil: 'load' });
await page.waitForTimeout(8000);
console.log('cm-content?', await page.locator('.cm-content').count());
await b.close();
