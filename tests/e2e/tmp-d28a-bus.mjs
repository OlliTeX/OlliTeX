// D28a flip — event-bus E2E against the GO bus (nginx :7420 → 127.0.0.1:3026):
//   B1 tab1 IDE loads (bus join OK)
//   B2 tab2 IDE loads (bus join OK)
//   B3 bus /clients shows BOTH publicIds (same project)
//   B4 bus /count-connected-clients == 2
//   B5 close tab1 → count drops to 1 (clientDisconnected path)
import { chromium } from 'playwright';
import * as cp from 'node:child_process';
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
if (!PID) { console.error('usage: tmp-d28a-bus.mjs <projectId>'); process.exit(2); }

const out = { B1: null, B2: null, B3: null, B4: null, B5: null, fails: [] };
const b = await chromium.launch();
const ctx = await b.newContext();

const busApi = (arg) => cp.execSync(`docker exec ol-e2e-overleaf-1 curl -s -m 3 "http://127.0.0.1:3026${arg}"`, { encoding: 'utf8' }).trim();

async function openIde(tab, label) {
  const p = tab === 1 ? await ctx.newPage() : await ctx.newPage();
  const key = 'B' + tab;
  p.on('pageerror', (e) => out.fails.push(key + ' PAGEERR ' + (e.message || '').slice(0, 160)));
  await p.goto(BASE + '/project/' + PID, { waitUntil: 'domcontentloaded' });
  try {
    await p.waitForSelector('.cm-content', { timeout: 45000 });
    out[key] = 'pass: editor with bus join (cm-content)';
  } catch {
    out[key] = 'FAIL: no editor within 45s';
  }
  return p;
}

await ctx.newPage().then(async (lp) => {
  await lp.goto(BASE + '/login', { waitUntil: 'load' });
  await lp.waitForSelector('input[name=email]', { timeout: 20000 });
  await lp.fill('input[name=email]', EMAIL);
  await lp.fill('input[name=password]', PASSWORD);
  await Promise.all([lp.waitForURL('**/hub**', { timeout: 30000 }), lp.click('button[type=submit]')]);
});

const tab1 = await openIde(1);
const tab2 = await openIde(2);
await new Promise((r) => setTimeout(r, 2500));

try {
  const list = JSON.parse(busApi('/clients'));
  const clients = Array.isArray(list) ? list : (list.clients || []);
  const inProject = clients.filter((c) => c.project_id === PID);
  out.B3 = inProject.length >= 2
    ? 'pass: ' + inProject.length + ' bus clients in project: ' + inProject.map((c) => c.client_id).join(',')
    : 'FAIL: only ' + inProject.length + ' clients: ' + JSON.stringify(inProject).slice(0, 200);
} catch (e) { out.B3 = 'FAIL: /clients: ' + String(e.message).slice(0, 120); }

try {
  const c = JSON.parse(busApi('/project/' + PID + '/count-connected-clients'));
  out.B4 = c.nConnectedClients === 2 ? 'pass: count=2' : 'FAIL: count=' + c.nConnectedClients;
} catch (e) { out.B4 = 'FAIL: count: ' + String(e.message).slice(0, 120); }

await tab1.close();
await new Promise((r) => setTimeout(r, 1500));
try {
  const c = JSON.parse(busApi('/project/' + PID + '/count-connected-clients'));
  out.B5 = c.nConnectedClients === 1 ? 'pass: count dropped to 1 after tab close' : 'FAIL: count=' + c.nConnectedClients;
} catch (e) { out.B5 = 'FAIL: ' + String(e.message).slice(0, 120); }

await b.close();
const fails = Object.entries(out).filter(([, v]) => typeof v === 'string' && String(v).startsWith('FAIL'));
console.log(JSON.stringify(out, null, 1));
process.exit(fails.length ? 1 : 0);
