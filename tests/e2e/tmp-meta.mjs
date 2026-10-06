import { chromium } from 'playwright';
const b=await chromium.launch();
const page=await b.newPage();
await page.goto('http://127.0.0.1:7420/login',{waitUntil:'load'});
await page.waitForSelector('input[name=email]');
await page.fill('input[name=email]','e2e-admin@e2e.test');
await page.fill('input[name=password]','Ol-Fixture-9x7K');
await Promise.all([page.waitForURL('**/hub**',{timeout:30000}), page.click('button[type=submit]')]);
const data=await page.evaluate(async ()=>{
  const r=await fetch('/project/6ab73507941509df73b96a34/metadata',{headers:{accept:'application/json'}});
  return { status:r.status, ct:r.headers.get('content-type'), body:(await r.text()).slice(0,800) };
});
console.log(JSON.stringify(data,null,1));
await b.close();
