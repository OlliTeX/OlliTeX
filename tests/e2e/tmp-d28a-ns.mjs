import { chromium } from 'playwright';
const BASE = 'http://localhost:7420';
const b = await chromium.launch();
const p = await b.newPage();
await p.goto(BASE + '/login', { waitUntil: 'load' });
await p.waitForSelector('input[name=email]', { timeout: 20000 });
await p.fill('input[name=email]', 'e2e-admin@e2e.test');
await p.fill('input[name=password]', 'Ol-Fixture-9x7K');
await Promise.all([p.waitForURL(/\/(projects|hub)/, { timeout: 30000 }), p.click('button[type=submit]')]);
await p.goto(BASE + '/project/6ab73507941509df73b96a34', { waitUntil: 'load' });
await p.waitForTimeout(8000);
const info = await p.evaluate(() => {
  try {
    const io = window.io;
    const sock = io && io.sockets && Object.values(io.sockets)[0];
    return {
      ioVersion: io && io.version,
      sockets: io && Object.keys(io.sockets || {}),
      namespaces: sock ? Object.keys(sock.namespaces || {}) : null,
      nsMeta: sock ? Object.entries(sock.namespaces || {}).map(([k, v]) => ({
        name: k, open: v.open, connected: v.connected, endpoint: v.name,
      })) : null,
      opts: sock ? { resource: sock.options.resource, transports: sock.options.transports, query: sock.options.query, path: sock.options.path } : null,
      wsUrlMeta: (document.querySelector('meta[name=ol-wsUrl]') || {}).content,
    };
  } catch (e) { return 'ERR ' + e.message; }
});
console.log(JSON.stringify(info, null, 1));
await b.close();
