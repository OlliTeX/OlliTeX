import { chromium } from 'playwright';
const b=await chromium.launch();
const page=await (await b.newContext()).newPage();
page.on('console', m=>{
  const t=m.text();
  if (/update|sync|yjs|Yjs|websock|collab|provider|Unable|Caught|denied|error/i.test(t)) console.log('[console]', m.type(), t.slice(0,200));
});
page.on('pageerror', e=>console.log('[pageerror]', String(e).slice(0,200)));
await page.goto('http://127.0.0.1:7420/login',{waitUntil:'load'});
await page.waitForSelector('input[name=email]');
await page.fill('input[name=email]','e2e-admin@e2e.test');
await page.fill('input[name=password]','Ol-Fixture-9x7K');
await Promise.all([page.waitForURL('**/hub**',{timeout:30000}), page.click('button[type=submit]')]);
await page.goto('http://127.0.0.1:7420/project/6ab73507941509df73b96a34',{waitUntil:'load'});
await page.waitForTimeout(8000);
console.log('--- state ---');
console.log((await page.locator('body').innerText()).slice(0,200));
await b.close();
