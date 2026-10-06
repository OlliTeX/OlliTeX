import { chromium } from 'playwright';
const b=await chromium.launch();
const ctx=await b.newContext();
await ctx.addInitScript(()=>{
  const OrigWS=window.WebSocket;
  window.__wslog=window.__wslog||[];
  window.WebSocket=function(...a){
    const ws=new OrigWS(...a);
    window.__wslog.push({url:ws.url, t:'open:'+Date.now()});
    const origOn=ws.onmessage;
    ws.addEventListener('message', ev=>{
      const d=ev.data;
      const buf=d instanceof ArrayBuffer ? new Uint8Array(d) : null;
      window.__wslog.push({t:'msg', size:d.byteLength??d.length, head: buf? Array.from(buf.slice(0,8)).map(x=>x.toString(16)).join(' ') : String(d).slice(0,60)});
    });
    ws.addEventListener('close', ev=>window.__wslog.push({t:'close', code:ev.code, reason:ev.reason}));
    ws.addEventListener('error', ev=>window.__wslog.push({t:'error'}));
    return ws;
  };
  window.WebSocket.prototype=OrigWS.prototype;
  Object.setPrototypeOf(window.WebSocket, OrigWS);
});
const page=await ctx.newPage();
await page.goto('http://127.0.0.1:7420/login',{waitUntil:'load'});
await page.waitForSelector('input[name=email]');
await page.fill('input[name=email]','e2e-admin@e2e.test');
await page.fill('input[name=password]','Ol-Fixture-9x7K');
await Promise.all([page.waitForURL('**/hub**',{timeout:30000}), page.click('button[type=submit]')]);
await page.goto('http://127.0.0.1:7420/project/6ab73507941509df73b96a34',{waitUntil:'load'});
await page.waitForTimeout(9000);
const log=await page.evaluate(()=>window.__wslog||[]);
console.log(JSON.stringify(log,null,1).slice(0,1500));
await b.close();
