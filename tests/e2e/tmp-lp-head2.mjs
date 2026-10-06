import { chromium } from 'playwright'
const BASE='http://127.0.0.1:7420'
const b=await chromium.launch()
async function login(page,acct){await page.goto(BASE+'/login',{waitUntil:'domcontentloaded'});await page.waitForTimeout(500);await page.fill('#email',acct.email);await page.fill('#password',acct.password);await page.click('button[type=submit]');await page.waitForURL(/\/project/,{timeout:30000})}
const ctx=await b.newContext();const page=await ctx.newPage()
await login(page,{email:'e2e-admin@e2e.test',password:'Ol-Fixture-9x7K'})
const res=await page.goto(BASE+'/launchpad',{waitUntil:'domcontentloaded'})
console.log('STATUS '+res.status())
for(const [k,v] of Object.entries(res.headers()))console.log(k+': '+v)
await ctx.close();await b.close()
