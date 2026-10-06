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
const cookies=await page.context().cookies();
const sid=cookies.find(c=>c.name==='overleaf.sid')?.value;
console.log('cookie sid?', sid? '(yes, len '+sid.length+')':'MISSING');
try {
  const ws=new WebSocket('ws://127.0.0.1:7420/collab/'+PID, { headers:{ Cookie:'overleaf.sid='+sid, Origin:BASE } });
  const t=setTimeout(()=>{ console.log('CONNECT TIMED OUT'); process.exit(1); }, 6000);
  ws.onopen=()=>{ console.log('OPEN OK — ws connected!'); clearTimeout(t); ws.close(); process.exit(0); };
  ws.onerror=(e)=>{ console.log('WS error event', e.message||''); clearTimeout(t); process.exit(1); };
  ws.onclose=(e)=>{ console.log('CLOSE code='+e.code+' reason='+JSON.stringify(e.reason)); clearTimeout(t); process.exit(1); };
} catch(e){ console.log('throw:', e.message); }
b.close();
