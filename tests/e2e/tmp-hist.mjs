import { chromium } from 'playwright';
const b=await chromium.launch();
const ctx=await b.newContext();
const page=await ctx.newPage();
await page.goto('http://127.0.0.1:7420/login',{waitUntil:'load'});
await page.waitForSelector('input[name=email]');
await page.fill('input[name=email]','e2e-admin@e2e.test');
await page.fill('input[name=password]','Ol-Fixture-9x7K');
await Promise.all([page.waitForURL('**/hub**',{timeout:30000}), page.click('button[type=submit]')]);
const r=await page.evaluate(async()=>{
  const h=await fetch('/project/6ab73507941509df73b96a34/history',{headers:{accept:'application/json'}});
  return { status:h.status, body:(await h.text()).slice(0,400) };
});
console.log(JSON.stringify(r,null,1));
// also save the passport cookie for push.cjs
const sid=await page.context().cookies().then(cs=>cs.find(c=>c.name==='overleaf.sid')?.value);
if (sid) {
  const fs=await import('node:fs');
  fs.writeFileSync('/tmp/ol_probe1.json', JSON.stringify({ base:'http://127.0.0.1:7420', cookie:'overleaf.sid='+sid }));
  console.log('PASSPORT cookie saved for push.cjs');
}
await b.close();
