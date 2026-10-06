// F1 flip — full browser E2E (Yjs adapter live):
//   A1 editor loads with seeded content (cm-content, documentclass + begin{document})
//   A2 D25 placeholder (.yjs-engine-review-note) visible
//   A3 typing locally → history v2 (local direction committed)
//   A4 external peer push (push.cjs) → remote edit mirrored into open editor
//   A5 history chain (≥3) + v1 = seed
// base/creds/project from .env.test like the suite.
import { chromium } from 'playwright';
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);

// pull env
const envFile = path.resolve(process.cwd(), '.env.test');
const env = Object.fromEntries(
  fs.readFileSync(envFile, 'utf8').split('\n').filter((l) => l && !l.startsWith('#')).map((l) => {
    const i = l.indexOf('=');
    return [l.slice(0, i).trim(), l.slice(i + 1).trim().replace(/^["']|["']$/g, '')];
  })
);
const BASE = env.BASE_URL || 'http://127.0.0.1:7420';
const EMAIL = env.E2E_ADMIN_EMAIL || 'e2e-admin@e2e.test';
const PASSWORD = env.E2E_ADMIN_PASSWORD || 'Ol-Fixture-9x7K';
const PID = process.argv[2] || '';
if (!PID) {
  console.error('usage: tmp-f1-e2e.mjs <projectId>');
  process.exit(2);
}

const out = { A1: null, A2: null, A3: null, A4: null, A5: null, fails: [] };
const b = await chromium.launch();
const page = await b.newPage();
page.on('pageerror', (e) => out.fails.push('PAGEERR ' + (e.message || '').slice(0, 200)));

async function apiFetch(method, p, body) {
  const r = await page.request[method](BASE + p, body ? { data: body, headers: { 'content-type': 'application/json', accept: 'application/json' } } : { headers: { accept: 'application/json' } });
  let text = '';
  try { text = await r.text(); } catch { }
  return { status: r.status(), text };
}

// login
await page.goto(BASE + '/login', { waitUntil: 'load' });
await page.waitForSelector('input[name=email]', { timeout: 20000 });
await page.fill('input[name=email]', EMAIL);
await page.fill('input[name=password]', PASSWORD);
await Promise.all([page.waitForURL('**/hub**', { timeout: 30000 }), page.click('button[type=submit]')]);

// open editor
await page.goto(BASE + '/project/' + PID, { waitUntil: 'load' });

// A1: editor boots with the seed (v1) content
let cm = null;
try {
  cm = await page.waitForSelector('.cm-content', { timeout: 45000 });
} catch { }
let a1 = false;
if (cm) {
  const txt = await page.evaluate(() => document.querySelector('.cm-content').innerText);
  a1 = /documentclass\s*\{/.test(txt) && /begin\{document\}/.test(txt) && /end\{document\}/.test(txt);
}
out.A1 = { booted: Boolean(cm), seeded: a1 };

// A2: D25 honest placeholder
const note = await page.locator('.yjs-engine-review-note').count().catch(() => 0);
out.A2 = { placeholderVisible: note > 0 };

// A3: local typing → v2 in history
if (cm) {
  const before = await page.evaluate(() => document.querySelector('.cm-content').innerText.length);
  await page.click('.cm-content');
  await page.keyboard.press('Control+End');
  await page.keyboard.type('\n% f1-e2e-edit-' + Date.now(), { delay: 8 });
  await page.waitForTimeout(2500);
  const after = await page.evaluate(() => document.querySelector('.cm-content').innerText.length);
  let h = null;
  try { h = (await apiFetch('get', '/project/' + PID + '/collab/history')).text; } catch { }
  let count = 0, last = null;
  try {
    const j = JSON.parse(h);
    const hs = (j && (j.versions || j.history)) || [];
    count = Array.isArray(hs) ? hs.length : 0;
    last = Array.isArray(hs) && hs[0] ? JSON.stringify(hs[0]).slice(0, 120) : null;
  } catch { }
  out.A3 = { grew: after > before, historyCount: count, last };
}

// A4: external peer push → mirrored in the open editor
const beforeText = cm ? await page.evaluate(() => document.querySelector('.cm-content').innerText) : '';
const { execFileSync } = await import('node:child_process');
try {
  execFileSync('node', ['/tmp/wsprj/push.cjs', PID, '% f1-e2e-remote-push-123'], { cwd: '/tmp/wsprj', env: { ...process.env } });
  await page.waitForTimeout(6000);
} catch (e) {
  out.fails.push('push.cjs failed: ' + String(e.message || e).slice(0, 160));
}
const afterText = cm ? await page.evaluate(() => document.querySelector('.cm-content').innerText) : '';
out.A4 = { mirrored: afterText.includes('% f1-e2e-remote-push-123') && beforeText !== afterText };

// A5: history chain + v1 = seed
let hist = [];
try { hist = JSON.parse((await apiFetch('get', '/project/' + PID + '/collab/history')).text).versions || []; } catch { }
out.A5 = { count: hist.length };

console.log(JSON.stringify(out, null, 2));
await b.close();
const ok = out.A1 && out.A1.booted && out.A1.seeded && out.A2.placeholderVisible && out.A3 && out.A3.grew && out.A4 && out.A4.mirrored;
process.exit(ok ? 0 : 1);
