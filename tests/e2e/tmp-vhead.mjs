import { chromium } from 'playwright';
const b=await chromium.launch();
const page=await (await b.newContext()).newPage();
await page.goto('http://127.0.0.1:7420/login',{waitUntil:'load'});
await page.waitForSelector('input[name=email]');
await page.fill('input[name=email]','e2e-admin@e2e.test');
await page.fill('input[name=password]','Ol-Fixture-9x7K');
await Promise.all([page.waitForURL('**/hub**',{timeout:30000}), page.click('button[type=submit]')]);
const r=await page.evaluate(async()=>{
  const h=await fetch('/project/6ab73507941509df73b96a34/collab/history/14',{headers:{accept:'application/json'}});
  const v=await h.json().catch(()=>null);
  const h2=await fetch('/project/6ab73507941509df73b96a34/collab/history',{headers:{accept:'application/json'}});
  const top=await h2.json();
  return { v14: v && v.content ? v.content.slice(-120) : (v&&v.content!==undefined?v.content:JSON.stringify(v).slice(0,100)), top: top.versions && top.versions.slice(0,3) };
});
console.log(JSON.stringify(r,null,1).slice(0,700));
await b.close();
