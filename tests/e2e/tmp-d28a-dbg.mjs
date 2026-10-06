import { chromium } from 'playwright';
import fs from 'node:fs';
import path from 'node:path';
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
const PID = process.argv[2];
const b = await chromium.launch();
const p = await (await b.newContext()).newPage();
const errs = [], reqs = [];
p.on('pageerror', (e) => errs.push('PE ' + (e.message || '').slice(0, 220)));
p.on('requestfailed', (r) => reqs.push('RF ' + r.url().slice(0, 120) + ' ' + (r.failure() || {}).errorText));
p.on('response', (r) => { if (r.status() >= 400) reqs.push('R' + r.status() + ' ' + r.url().slice(0, 120)); });
await p.goto(BASE + '/login', { waitUntil: 'load', timeout: 30000 });
await p.waitForSelector('input[name=email]', { timeout: 20000 });
await p.fill('input[name=email]', EMAIL);
await p.fill('input[name=password]', PASSWORD);
await Promise.all([p.waitForURL('**/hub**', { timeout: 30000 }), p.click('button[type=submit]')]);
console.log('after-login:', p.url());
await p.goto(BASE + '/project/' + PID, { waitUntil: 'domcontentloaded', timeout: 40000 });
await new Promise((r) => setTimeout(r, 12000));
console.log('ide url:', p.url());
console.log('has cm-content:', (await p.$('.cm-content')) ? 'yes' : 'no');
console.log('title:', await p.title());
console.log(JSON.stringify({ errs: errs.slice(0, 6), reqs: reqs.slice(0, 10) }, null, 1));
await b.close();
