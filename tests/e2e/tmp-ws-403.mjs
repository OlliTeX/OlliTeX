import { chromium } from 'playwright';
const BASE='http://127.0.0.1:7420';
const b=await chromium.launch();
const page=await b.newPage();
const api=page.request;
// login
await page.goto(BASE+'/login',{waitUntil:'load'});
await page.waitForSelector('input[name=email]');
await page.fill('input[name=email]','e2e-admin@e2e.test');
await page.fill('input[name=password]','Ol-Fixture-9x7K');
await Promise.all([page.waitForURL('**/hub**',{timeout:30000}), page.click('button[type=submit]')]);
await page.waitForTimeout(1500);
// attempt the WS handshake as the page would (fetch upgrade is not possible via page; use raw ws)
const res = await api.get(`${BASE}/collab/6ab73507941509df73b96a34`, { headers: { Upgrade:'websocket','Connection':'Upgrade','Sec-WebSocket-Version':'13','Sec-WebSocket-Key':'dGhlIHNhbXBsZSBub25jZQ==' } }).catch(e=>({status:()=>('fetch-err:'+String(e).slice(0,80)), headers:()=>({})}));
console.log('status:', res.status());
console.log('resp headers:', JSON.stringify(Object.fromEntries(res.headers()), null, 1).slice(0, 600));
console.log('body:', (await res.text().catch(()=>'?')).slice(0,200));
await b.close();
