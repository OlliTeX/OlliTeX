import { chromium } from 'playwright'
import { readFileSync } from 'fs'
const B='http://127.0.0.1:7420'
const creds = readFileSync('fixtures/credentials.ts','utf8')
const grab=(n)=>creds.match(new RegExp(n+" = ({[^}]+})"))[1]
const em=(s,re)=>s.match(re)[1]
const USER={email:em(grab('USER'),/email:\s*'([^']+)'/),password:em(grab('USER'),/password:\s*'([^']+)'/)}
const b=await chromium.launch(); const c=await b.newContext({viewport:{width:1400,height:900}}); const p=await c.newPage()
await p.goto(B+'/login',{waitUntil:'domcontentloaded'})
await p.fill('input[type="email"], #email', USER.email); await p.fill('input[type="password"], #password', USER.password)
await p.locator('button:has-text("Sign in"), button[type="submit"]').first().click(); await p.waitForTimeout(2000)
const csrf=(await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(()=>null))||''
const H={'X-Csrf-Token':csrf,'Content-Type':'application/json'}
const api=(m,pa,d)=>c.request.fetch(B+pa,{method:m,headers:H,data:d===undefined?undefined:JSON.stringify(d)})
const r0=await api('POST','/project/new',{projectName:'inspect-live-' + Date.now()})
const pid=((await r0.text()).match(/"[0-9a-f]{24}"/)||[null])[0]?.replace(/"/g,'')
console.log('pid', pid)
// open editor
await p.goto(B + '/editor/' + pid, { waitUntil:'domcontentloaded' })
await p.waitForSelector('.cm-content', { timeout: 90_000 })
console.log('editor .cm-content visible')
// replace content with an issue-generating document
const ed = p.locator('.cm-content').first()
await ed.click()
await p.keyboard.press('ControlOrMeta+A')
await p.keyboard.press('Delete')
await p.waitForTimeout(800)
const DOC = '\\documentclass{article}\n\\begin{document}\n\\section*{Intro}\\label{sec-intro}\nSee \\ref{sec-missing} and \\cite{doe:2000:missing}.\n\\end{document}'
await p.keyboard.type(DOC, { delay: 4 })
await p.waitForTimeout(4000)
// wire: analyze; entry point = rootFolder doc entity id
const { execSync } = await import('child_process')
const script = "print(String(db.projects.findOne({ _id: ObjectId('" + pid + "') }).rootFolder[0].docs[0]._id))"
const docId = execSync('docker exec ol-e2e-mongo-1 mongosh --quiet sharelatex --eval "' + script + '"', { encoding:'utf8' }).trim().split('\n').pop()
console.log('entry docId', docId)
let ra = await api('POST','/project/'+pid+'/project-inspection/analyze',{ entryPointIds:[docId] })
let t = await ra.text()
console.log('ANALYZE:', ra.status())
try {
  const j = JSON.parse(t)
  console.log('issues:', JSON.stringify(j.issues || null).slice(0, 520))
  console.log('overview:', JSON.stringify(j.overview || null).slice(0, 260))
  console.log('coverage:', JSON.stringify(j.coverage || null).slice(0, 260))
  console.log('graph.nodes:', JSON.stringify(j.graph?.nodes || null).slice(0, 260))
} catch(e) { console.log('NO JSON:', t.slice(0, 200)) }
// rail: find the inspection entry
const rails = await p.locator('[class*="rail"] , aside, nav, [data-testid*="rail"]').count()
console.log('rail-ish elements:', rails)
const railTexts = await p.locator('[class*="rail" i]').allInnerTexts().catch(()=>[])
for (const tt of railTexts.slice(0, 6)) console.log('  rail element text:', JSON.stringify(tt.slice(0, 160)))

// THE rail tab is icon 'monitoring' (rail.tsx audit 019: key project-inspection, icon monitoring)
const tab = p.locator('span, i, svg, [class*="icon" i]', { hasText: /^monitoring$/ }).first()
// (exact count via getByText below)
const tabLoc = p.getByText('monitoring', { exact: true }).first()
const tc = await tabLoc.count()
console.log('exact-text monitoring nodes:', tc)
await tabLoc.click({ timeout: 8000 }).catch(e=>console.log('tab click failed:', String(e).slice(0,100)))
await p.waitForTimeout(5000)
const anBtn = p.getByText('Analyze', { exact: true }).first()
console.log('Analyze button count:', await anBtn.count())
await anBtn.click({ timeout: 10000 }).catch(e=>console.log('analyze click failed:', String(e).slice(0,100)))
await p.waitForTimeout(9000)
const body = await p.locator('body').innerText().catch(()=>'')
const idx = body.search(/Citation has no matching|has no matching bibliography|referenced but|undefined reference|issue/i)
console.log('POST-ANALYZE slice:', body.slice(Math.max(0, idx-150), idx+900).replace(/\n/g,' | '))
// cleanup
await api('POST','/project/'+pid+'/trash')
console.log('cleaned up')
await b.close()
