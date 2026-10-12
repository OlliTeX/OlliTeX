import { chromium } from 'playwright'
import { readFileSync, writeFileSync } from 'fs'
import { execSync } from 'child_process'
const B='http://127.0.0.1:7420'
const creds = readFileSync('fixtures/credentials.ts','utf8')
const grab=(n)=>creds.match(new RegExp(n+" = ({[^}]+})"))[1]
const em=(s,re)=>s.match(re)[1]
const ADMIN={email:em(grab('ADMIN'),/email:\s*'([^']+)'/),password:em(grab('ADMIN'),/password:\s*'([^']+)'/)}
const DISPO = 'e2e-admcy7-' + Date.now() + '@e2e.test'
const PW = 'AdmcyPw111!'
function docFull(){
  const q = 'const d=db.users.findOne({ email: "' + DISPO + '" }); print(JSON.stringify({ keys: Object.keys(d).join(","), hp: d.hashedPassword ? d.hashedPassword.slice(0,12)+"..len"+String(d.hashedPassword).length : null, susp: d.suspended, inactive: d.inactive, must_reconfirm: d.must_reconfirm }))'
  writeFileSync('/tmp/dq2.js', q)
  return execSync('docker exec -i ol-e2e-mongo-1 mongosh --quiet sharelatex < /tmp/dq2.js', {encoding:'utf8'}).trim().split('\n').pop()
}
const b = await chromium.launch()
const cA = await b.newContext()
const pA = await cA.newPage()
await pA.goto(B+'/login',{waitUntil:'domcontentloaded'})
await pA.fill('input[type="email"], #email', ADMIN.email)
await pA.fill('input[type="password"], #password', ADMIN.password)
await pA.locator('button:has-text("Sign in"), button[type="submit"]').first().click()
await pA.waitForTimeout(2000)
const tok = (await pA.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(()=>null)) || ''
const H = tok ? { 'X-Csrf-Token': tok, 'Content-Type':'application/json' } : { 'Content-Type':'application/json' }
const api = (m, path, data) => cA.request.fetch(B + path, { method: m, headers: H, data: data===undefined?undefined:JSON.stringify(data) })
let r = await api('POST','/admin/user/create',{ email: DISPO, firstName:'Adm7', lastName:'Cy', password: PW })
console.log('create:', r.status())
const uid = ((await r.text()).match(/[0-9a-f]{24}/)||[null])[0]
await api('POST','/admin/user/'+uid+'/update',{ inactive:false })
console.log('doc now:', docFull().slice(0,280))
// RAW login POST (same context = fresh csrf cookie)
const rL = await cA.request.post(B + '/login', { data: { email: DISPO, password: PW }, headers: H })
console.log('RAW login POST:', rL.status(), (await rL.text()).slice(0,220))
// bcrypt verify the stored hash offline
const qv = 'const d=db.users.findOne({ email: "' + DISPO + '" });'
writeFileSync('/tmp/dq3.js', qv)

await b.close()
