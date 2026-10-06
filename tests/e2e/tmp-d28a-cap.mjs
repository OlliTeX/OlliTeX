// D28a contract capture: drive the live IDE against the Node real-time service
// (debug mode logging enabled) and dump the exact wire events.
import { chromium } from 'playwright';
import { execSync } from 'node:child_process';

const BASE = 'http://localhost:7420';
const USER = 'e2e-admin@e2e.test';
const PASS = 'Ol-Fixture-9x7K';
const PROJ = '6ab73507941509df73b96a34';

const b = await chromium.launch();
const p = await b.newPage();
const consoleErrs = [];
p.on('console', m => { if (m.type() === 'error') consoleErrs.push(m.text().slice(0, 150)); });

await p.goto(BASE + '/login', { waitUntil: 'load' });
await p.waitForSelector('input[name=email]', { timeout: 20000 });
await p.fill('input[name=email]', USER);
await p.fill('input[name=password]', PASS);
await Promise.all([p.waitForURL(/\/(projects|hub)/, { timeout: 30000 }), p.click('button[type=submit]')]);
await p.goto(BASE + '/project/' + PROJ, { waitUntil: 'domcontentloaded' });
// let the IDE boot + bus connect; type to generate cursor updates
await p.waitForTimeout(12000);
try { await p.click('.cm-editor', { timeout: 5000 }); } catch {}
try {
  await p.keyboard.type(' %D28aProbe', { delay: 60 });
} catch {}
await p.waitForTimeout(6000);
// second tab: same project → presence/cursor broadcast path
const p2 = await b.newPage();
await p2.goto(BASE + '/project/' + PROJ, { waitUntil: 'domcontentloaded' });
await p2.waitForTimeout(9000);
try { await p2.click('.cm-editor', { timeout: 5000 }); } catch {}
try { await p2.keyboard.type(' %D28aTwo', { delay: 60 }); } catch {}
await p2.waitForTimeout(6000);
await b.close();

const log = execSync('docker exec ol-e2e-overleaf-1 tail -300 /var/log/overleaf/real-time.log', { encoding: 'utf8' });
console.log('=== CONSOLE ERRORS ==='); console.log(consoleErrs.slice(0, 6).join('\n') || '(none)');
console.log('=== REAL-TIME LOG TAIL ===');
console.log(log);
