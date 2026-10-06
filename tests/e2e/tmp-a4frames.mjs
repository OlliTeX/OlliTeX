import { chromium } from 'playwright';
const b=await chromium.launch();
const ctx=await b.newContext();
await ctx.addInitScript(()=>{
  const O=window.WebSocket;
  window.__f=[];
  window.WebSocket=function(...a){
    const w=new O(...a);
    w.addEventListener('message', ev=>{
      const d=ev.data; const buf=d instanceof ArrayBuffer? new Uint8Array(d):null;
      window.__f.push({t:'in', len:d.byteLength??d.length, head: buf? Array.from(buf.slice(0,6)).map(x=>x.toString(16)).join(' '):String(d).slice(0,40)});
    });
    return w;
  };
  window.WebSocket.prototype=O.prototype;
});
const page=await ctx.newPage();
await page.goto('http://127.0.0.1:7420/login',{waitUntil:'load'});
await page.waitForSelector('input[name=email]');
await page.fill('input[name=email]','e2e-admin@e2e.test');
await page.fill('input[name=password]','Ol-Fixture-9x7K');
await Promise.all([page.waitForURL('**/hub**',{timeout:30000}), page.click('button[type=submit]')]);
await page.goto('http://127.0.0.1:7420/project/6ab73507941509df73b96a34',{waitUntil:'load'});
await page.waitForSelector('.cm-content',{timeout:45000});
await page.waitForTimeout(2000);
window.framesBefore=await page.evaluate(()=>{ (window.__f=window.__f||[]); return 0; });
const { execFileSync } = await import('node:child_process');
execFileSync('node', ['/tmp/wsprj/push.cjs', '6ab73507941509df73b96a34', '% A4MARK2-' + Date.now()], { cwd: '/tmp/wsprj' });
await page.waitForTimeout(6000);
const frames=await page.evaluate(()=>window.__f||[]);
console.log(JSON.stringify(frames,null,0).slice(0,700));
await b.close();
