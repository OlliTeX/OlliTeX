import { chromium } from 'playwright';
const BASE = 'http://localhost:7420';
const b = await chromium.launch();
const p = await b.newPage();
const frames = [];
p.on('websocket', ws => {
  const label = 'WS ' + ws.url();
  frames.push(':: ' + label);
  ws.on('framesent', f => frames.push('>> ' + f.payload));
  ws.on('framereceived', f => frames.push('<< ' + f.payload));
});
const http = [];
p.on('response', r => {
  const u = r.url();
  if (u.includes('/socket.io/1') && !u.includes('.js')) {
    http.push('HTTP ' + r.request().method() + ' ' + u + ' -> ' + r.status());
  }
});
await p.goto(BASE + '/login', { waitUntil: 'load' });
await p.waitForSelector('input[name=email]', { timeout: 20000 });
await p.fill('input[name=email]', 'e2e-admin@e2e.test');
await p.fill('input[name=password]', 'Ol-Fixture-9x7K');
await Promise.all([p.waitForURL(/\/(projects|hub)/, { timeout: 30000 }), p.click('button[type=submit]')]);
await p.goto(BASE + '/project/6ab73507941509df73b96a34', { waitUntil: 'load' });
await p.waitForTimeout(10000);
try { await p.click('.cm-editor', { timeout: 5000 }); await p.keyboard.type(' %D28aWire', { delay: 50 }); } catch {}
await p.waitForTimeout(8000);
await b.close();
console.log(http.join('\n'));
console.log('--- WS FRAMES ---');
const out = frames.map(f => {
  let s = typeof f === 'string' ? f : String(f.payload ?? f);
  return s.length > 400 ? s.slice(0, 400) + '…' : s;
});
console.log(out.join('\n'));
