import { chromium } from 'playwright';
const b=await chromium.launch();
const ctx=await b.newContext();
const page=await ctx.newPage();
await page.goto('http://127.0.0.1:7420/login',{waitUntil:'load'});
await page.waitForSelector('input[name=email]');
await page.fill('input[name=email]','e2e-admin@e2e.test');
await page.fill('input[name=password]','Ol-Fixture-9x7K');
await Promise.all([page.waitForURL('**/hub**',{timeout:30000}), page.click('button[type=submit]')]);
await page.goto('http://127.0.0.1:7420/project/6ab73507941509df73b96a34',{waitUntil:'load'});
await page.waitForTimeout(1500);
const out=await page.evaluate(async ()=>{
  const log=[];
  try {
    const mod=await import('/js/'+ (document.querySelector('script[src*="ide"]')?.src.split('/js/')[1]?.split('-')[0]||'') +'-x.js').catch(()=>null);
  } catch(e){}
  // use the app's own provider via a fresh import from the BUNDLE is not possible;
  // instead: fetch the same y-websocket through a dynamic chunk URL we find from performance entries
  const entries=performance.getEntriesByType('resource').map(e=>e.name).filter(n=>/\/js\/\d+-/.test(n));
  const wssrc=entries.find(n=>/3269-/.test(n));
  return { entries: entries.slice(0,6), wssrc };
});
console.log(JSON.stringify(out,null,1).slice(0,400));
// Now: monkey approach — create provider via the page's own y-websocket module is hard;
// use a raw lib0-style check instead: verify provider events via the actual collab ws object
// found inside the app: reach into the editor state? Skip — instead verify via a fresh connection:
const res2=await page.evaluate(async (BASE)=>{
  // y-websocket is bundled; but we can still create a provider using the SAME ws URL and inspect
  // whether 'status' events fire by watching the app's own provider instance if exposed.
  return 'n/a';
},'x');
// Simpler: hook WebSocket BEFORE the page's provider connects (re-navigate with hook), then inspect
// the provider instance's event set after connect — find the ws object via our wrapper log.
await b.close();
