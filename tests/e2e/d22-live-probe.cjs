/* d22-live-probe.cjs — READ-ONLY inspection of /hub#/site.general.stats on
   the disposable e2e stack (owner fixture identities, throwaway stack).
   Not a spec; run: node d22-live-probe.cjs */
const { chromium } = require('playwright');

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420';
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' };

(async () => {
  const browser = await chromium.launch({ headless: true });
  const page = await (await browser.newContext()).newPage();
  const errors = [];
  page.on('console', m => { if (m.type() === 'error') errors.push(m.text().slice(0, 160)); });
  page.on('response', r => { if (r.status() >= 400 && r.url().includes('instance-stats')) errors.push(`${r.status()} ${r.url()}`); });

  await page.goto(BASE + '/login', { waitUntil: 'domcontentloaded', timeout: 30000 });
  await page.fill('#email', ADMIN.email);
  await page.fill('#password', ADMIN.password);
  await page.click('button[type=submit]');
  await page.waitForURL(/\/project|\/hub/, { timeout: 30000 });
  console.log('LOGIN OK →', page.url());

  // the hub stats leaf
  await page.goto(BASE + '/hub#/site.general.stats', { waitUntil: 'domcontentloaded', timeout: 30000 });
  await page.waitForTimeout(4000); // let the section fetch render

  const bodyText = await page.evaluate(() => document.body.innerText);
  const interesting = bodyText
    .split('\n')
    .map(s => s.trim())
    .filter(s => /statistic|instance|projects|users|storage|system|series|error|load|window|month|week/i.test(s))
    .slice(0, 24);
  console.log('HUB STATS SECTION (text lines):');
  console.log(interesting.join('\n') || '(no matching lines — full head below)');
  if (!interesting.length) console.log(bodyText.slice(0, 600));

  // the API the section calls
  const api = await page.evaluate(async (mets) => {
    const out = {};
    for (const [metric, window] of mets) {
      try {
        const r = await fetch(`/admin/instance-stats/api/series?metric=${metric}&window=${window}`, { headers: { Accept: 'application/json' } });
        out[metric] = { status: r.status, body: (await r.text()).slice(0, 200) };
      } catch (e) { out[metric] = { error: String(e) }; }
    }
    return out;
  }, [['active_projects', '30d'], ['new_projects', '30d'], ['user_count', '30d'], ['storage', '30d']]);
  console.log('\nSERIES API:');
  for (const [k, v] of Object.entries(api)) console.log(`  ${k}: ${v.status ?? ''} ${(v.body || v.error || '').slice(0, 140)}`);

  console.log('\nCONSOLE ERRORS:', errors.length ? errors.slice(0, 6) : 'none');
  await browser.close();
})().catch(e => { console.error('PROBE ERR:', e.message); process.exit(1); });
