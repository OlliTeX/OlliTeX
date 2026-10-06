import http from 'http';
import { chromium } from 'playwright';
const BASE='http://127.0.0.1:7420', PID='6ab73507941509df73b96a34';
const b=await chromium.launch();
const page=await b.newPage();
await page.goto(BASE+'/login',{waitUntil:'load'});
await page.waitForSelector('input[name=email]');
await page.fill('input[name=email]','e2e-admin@e2e.test');
await page.fill('input[name=password]','Ol-Fixture-9x7K');
await Promise.all([page.waitForURL('**/hub**',{timeout:30000}), page.click('button[type=submit]')]);
await page.waitForTimeout(1000);
const sid=await page.context().cookies().then(cs=>cs.find(c=>c.name==='overleaf.sid')?.value);
b.close();
console.log('sid len', sid?.length, 'head', String(sid).slice(0,14));
http.get({ host:'127.0.0.1', port:7420, path:'/collab/'+PID, headers:{
  Upgrade:'websocket', Connection:'Upgrade', 'Sec-WebSocket-Version':'13',
  'Sec-WebSocket-Key':'dGhlIHNhbXBsZSBub25jZQ==', 'Cookie':'overleaf.sid='+sid, Origin:BASE,
}}, res=>{
  console.log('STATUS:', res.statusCode);
  console.log('HDRS:', JSON.stringify(res.headers));
  let d=''; res.on('data',c=>d+=c); res.on('end',()=>console.log('BODY:', d.slice(0,120)));
}).on('error',e=>{ console.log('ERR', e.message); });
setTimeout(()=>process.exit(0), 6000);
