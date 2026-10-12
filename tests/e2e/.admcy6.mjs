import { chromium } from 'playwright'
import { execSync } from 'child_process'
import { readFileSync, writeFileSync } from 'fs'
const B='http://127.0.0.1:7420'
const creds = readFileSync('fixtures/credentials.ts','utf8')
const grab=(n)=>creds.match(new RegExp(n+" = ({[^}]+})"))[1]
const em=(s,re)=>s.match(re)[1]
const ADMIN={email:em(grab('ADMIN'),/email:\s*'([^']+)'/),password:em(grab('ADMIN'),/password:\s*'([^']+)'/)}
const DISPO = 'e2e-admcy6-' + Date.now() + '@e2e.test'
const PW = 'AdmcyPw000!'

const docState = () => {
  // mongo script is written to a file and piped in — zero shell quoting layers
  const q = 'const d=db.users.findOne({ email: "' + DISPO + '" }); print(JSON.stringify({ hp: String(Boolean(d.hashedPassword)), susp: String(d.suspended), keys: Object.keys(d).slice(0, 20).join(",").slice(0,160) }));'
  writeFileSync('/tmp/dq.js', q)
  return execSync(`docker exec -i ol-e2e-mongo-1 mongosh --quiet sharelatex < /tmp/dq.js`, {encoding:'utf8'}).trim().split('\n').pop()
}

const b = await chromium.launch()
const cA = await b.newContext({ viewport:{width:1500,height:900} })
const pA = await cA.newPage()
await pA.goto(B+'/login',{waitUntil:'domcontentloaded'})
await pA.fill('input[type="email"], #email', ADMIN.email)
await pA.fill('input[type="password"], #password', ADMIN.password)
await pA.locator('button:has-text("Sign in"), button[type="submit"]').first().click()
await pA.waitForTimeout(2000)
const tok = (await pA.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(()=>null)) || ''
const H = { 'X-Csrf-Token': tok, 'Content-Type':'application/json' }
const api = (m, path, data) => cA.request.fetch(B + path, { method: m, headers: H, data: data===undefined?undefined:JSON.stringify(data) })

let r = await api('POST','/admin/user/create',{ email: DISPO, firstName:'Adm6', lastName:'Cy', password: PW })
const uid = ((await r.text()).match(/[0-9a-f]{24}/)||[null])[0]
console.log('create:', r.status())
console.log('doc before:', docState().slice(0,230))

const clogin = async (pw) => {
  const c = await b.newContext(); const p = await c.newPage()
  await p.goto(B+'/login',{waitUntil:'domcontentloaded'})
  await p.fill('input[type="email"], #email', DISPO)
  await p.fill('input[type="password"], #password', pw)
  await p.locator('button:has-text("Sign in"), button[type="submit"]').first().click()
  await p.waitForTimeout(1600)
  const ok = !p.url().includes('/login')
  await c.close()
  return ok
}

r = await api('POST','/admin/user/'+uid+'/update',{ inactive:false })
console.log('update inactive:false ->', r.status(), (await r.text()).slice(0,110))
console.log('login after inactive=false:', await clogin(PW))

r = await api('POST','/admin/user/'+uid+'/update',{ admin:true, roleName:'admin' })
console.log('update admin:true ->', r.status(), (await r.text()).slice(0,110))
const c = await b.newContext(); const p = await c.newPage()
await p.goto(B+'/login',{waitUntil:'domcontentloaded'})
await p.fill('input[type="email"], #email', DISPO)
await p.fill('input[type="password"], #password', PW)
await p.locator('button:has-text("Sign in"), button[type="submit"]').first().click()
await p.waitForTimeout(1600)
console.log('C logged in?', !p.url().includes('/login'))
const rr = await c.request.get(B + '/admin-settings/site.general.users.all', { maxRedirects: 0 })
console.log('C admin-settings page:', rr.status(), (rr.headers()['location']||'').slice(0,60))
await c.close()

r = await api('POST','/admin/user/'+uid+'/update',{ suspended:true })
console.log('suspend ->', r.status(), (await r.text()).slice(0,90))
console.log('suspended login (expect false):', await clogin(PW))
r = await api('POST','/admin/user/'+uid+'/update',{ suspended:false })
console.log('unsuspend ->', r.status(), (await r.text()).slice(0,90))
console.log('unsuspended login (expect true):', await clogin(PW))

r = await api('POST','/admin/user/'+uid+'/delete')
console.log('soft delete ->', r.status(), (await r.text()).slice(0,90))
console.log('deleted login (expect false):', await clogin(PW))
console.log('doc after delete:', docState().slice(0,230))
r = await api('POST','/admin/user/'+uid+'/restore')
console.log('restore ->', r.status(), (await r.text()).slice(0,90))
console.log('restored login:', await clogin(PW))

// cleanup: purge the disposable so admin lists stay tidy
r = await api('DELETE','/admin/user/'+uid)
console.log('purge at end ->', r.status())
await b.close()
