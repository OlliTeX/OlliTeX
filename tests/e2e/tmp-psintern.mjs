import { chromium } from 'playwright';
const b=await chromium.launch();
const page=await (await b.newContext()).newPage();
const errs=[];
page.on('pageerror', e=>errs.push(String(e).slice(0,150)));
page.on('console', m=>{ if(m.type()==='error') errs.push(m.text().slice(0,150)); });
await page.goto('https://psintern.neuro.uni-bremen.de/login',{ waitUntil:'load', timeout:60000, ignoreHTTPSErrors:true });
await page.waitForTimeout(4000);
const state={
  title: await page.title(),
  hasLoginForm: (await page.locator('input[name=email]').count())>0,
  bodyLen: (await page.locator('body').innerText()).length,
  bodyHead: (await page.locator('body').innerText()).slice(0,120),
  errs: errs.slice(0,6),
};
console.log(JSON.stringify(state,null,1));
await b.close();
process.exit(state.hasLoginForm ? 0 : 1);
