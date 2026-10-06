import { chromium } from 'playwright';
const b=await chromium.launch();
const page=await (await b.newContext()).newPage();
await page.goto('http://127.0.0.1:7420/login',{waitUntil:'load'});
await page.waitForSelector('input[name=email]');
await page.fill('input[name=email]','e2e-admin@e2e.test');
await page.fill('input[name=password]','Ol-Fixture-9x7K');
await Promise.all([page.waitForURL('**/hub**',{timeout:30000}), page.click('button[type=submit]')]);
const r=await page.evaluate(async()=>{
  const out={};
  for (const v of [15,16,17]) {
    const h=await fetch('/project/6ab73507941509df73b96a34/collab/history/'+v,{headers:{accept:'application/json'}});
    const j=await h.json().catch(()=>({}));
    const c=(j.content||'');
    out[v]={ tail: c.slice(-100), markers: ['f1-e2e-remote-push-123','A4MARK-','A4M3-'].filter(m=>c.includes(m)) };
  }
  return out;
});
console.log(JSON.stringify(r,null,1));
await b.close();
