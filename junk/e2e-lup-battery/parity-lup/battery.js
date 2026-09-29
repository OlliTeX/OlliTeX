// linked-url-proxy contract battery (B2). Runs INSIDE the overleafserver-family
// container against http://127.0.0.1:3066 (the service). Asserts exact
// Node-contract (status, body) per case. Usage: node /tmp/lup-battery.js [up]
//   [up] = include upstream-dependent cases (needs upstream on 127.0.0.1:9991 +
//          OVERLEAF_LINKED_URL_ALLOWED_RESOURCES allowing it + MAX_UPLOAD_SIZE=1).
const BASE = 'http://127.0.0.1:3066'
const UP = process.argv.includes('up')
function html404(m){return '<!DOCTYPE html>\n<html lang="en">\n<head>\n<meta charset="utf-8">\n<title>Error</title>\n</head>\n<body>\n<pre>Cannot '+m+'</pre>\n</body>\n</html>\n'}
let pass = 0, fail = 0
const failures = []

async function probe(method, path, { headers = {} } = {}) {
  const res = await fetch(BASE + path, { method, headers, redirect: 'manual' })
  const body = await res.text()
  return { status: res.status, ct: (res.headers.get('content-type') || ''), body }
}

const cases = [
  ['P1 status json', 'GET', '/status', { status: 200, body: '{"status":"linked-url-proxy is up"}' }],
  ['P2 missing url 400', 'GET', '/', { status: 400, body: 'Missing ?url parameter' }],
  ['P3 unknown path 404', 'GET', '/nope', { status: 404, body: html404('GET /nope') }],
  ['P4 post 404', 'POST', '/', { status: 404, body: html404('POST /') }],
  ['P5 head mirrors get', 'HEAD', '/', { status: 400 }],
  ['P6 ftp 500', 'GET', '/?url=' + encodeURIComponent('ftp://example.com/x'), { status: 500, body: 'Error: Invalid url to pass to open(): ftp://example.com/x' }],
  ['P7 data 500', 'GET', '/?url=' + encodeURIComponent('data:text/html,x'), { status: 500, body: 'Error: Invalid url to pass to open(): data:text/html,x' }],
  ['P8 relative 500', 'GET', '/?url=' + encodeURIComponent('example.com'), { status: 500, body: 'Error: Invalid url to pass to open(): example.com' }],
  ['P9 garbage 500', 'GET', '/?url=' + encodeURIComponent('not a url'), { status: 500, body: 'Error: Invalid url to pass to open(): not a url' }],
  ['P10 loopback 403', 'GET', '/?url=' + encodeURIComponent('http://127.0.0.2:9/x'), { status: 403, body: 'Error: Blocked IP address: 127.0.0.2' }],
  ['P11 private 403', 'GET', '/?url=' + encodeURIComponent('http://10.0.0.5/x'), { status: 403, body: 'Error: Blocked IP address: 10.0.0.5' }],
  ['P12 linklocal 403', 'GET', '/?url=' + encodeURIComponent('http://169.254.16.20/x'), { status: 403, body: 'Error: Blocked IP address: 169.254.16.20' }],
  ['P13 dns 421', 'GET', '/?url=' + encodeURIComponent('http://no-such-z9.invalid/x'), { status: 421, body: 'Error: DNS lookup failed for no-such-z9.invalid' }],
]
if (UP) {
  cases.push(
    ['P14 upstream 200 bytes', 'GET', '/?url=' + encodeURIComponent('http://127.0.0.1:9991/ok'), { status: 200, body: 'hello-linked-url', ct: 'text/plain' }],
    ['P15 upstream 404', 'GET', '/?url=' + encodeURIComponent('http://127.0.0.1:9991/missing'), { status: 404, body: 'Error: request failed' }],
    ['P16 redirect resolves', 'GET', '/?url=' + encodeURIComponent('http://127.0.0.1:9991/redirect'), { status: 200, body: 'hello-linked-url' }],
    ['P17 redirect loop 421', 'GET', '/?url=' + encodeURIComponent('http://127.0.0.1:9991/loop'), { status: 421, body: 'Error: Too many redirects' }],
    ['P18 too large 413', 'GET', '/?url=' + encodeURIComponent('http://127.0.0.1:9991/big'), { status: 413, body: 'Error: file too large' }],
    ['P19 refused 422', 'GET', '/?url=' + encodeURIComponent('http://127.0.0.1:9992/x'), { status: 422, bodyPrefix: 'Error: ' }],
    ['P20 path>2000 400', 'GET', '/?url=' + encodeURIComponent('http://127.0.0.1:9991/' + 'a'.repeat(2100)), { status: 400, bodyPrefix: 'Error: Invalid or unsafe URL path: /' }],
  )
}

for (const [name, method, path, exp] of cases) {
  let got
  try {
    got = await probe(method, path)
  } catch (e) {
    fail++; failures.push(`${name}: NETWORK ERROR ${e}`)
    console.log(`FAIL ${name}: network error ${e}`)
    continue
  }
  const problems = []
  if (got.status !== exp.status) problems.push(`status ${got.status} != ${exp.status}`)
  if (exp.body && got.body !== exp.body) problems.push(`body ${JSON.stringify(got.body.slice(0, 90))} != ${JSON.stringify(exp.body.slice(0, 90))}`)
  if (exp.bodyPrefix && !got.body.startsWith(exp.bodyPrefix)) problems.push(`body ${JSON.stringify(got.body.slice(0, 90))} !prefix ${JSON.stringify(exp.bodyPrefix)}`)
  if (exp.ct && !got.ct.includes(exp.ct)) problems.push(`ct ${JSON.stringify(got.ct)} !~ ${exp.ct}`)
  if (problems.length === 0) {
    pass++; console.log(`PASS ${name} (${got.status})`)
  } else {
    fail++; failures.push(`${name}: ${problems.join(' | ')}\n   got status=${got.status} ct=${got.ct} body=${JSON.stringify(got.body.slice(0, 140))}`)
    console.log(`FAIL ${name}: ${problems.join(' | ')}`)
    console.log(`     got status=${got.status} ct=${got.ct} body=${JSON.stringify(got.body.slice(0, 140))}`)
  }
}

// P21 timeout 408 (35s hang vs 30s proxy timeout) — last, it is slow.
if (UP) {
  const t0 = Date.now()
  const got = await probe('GET', '/?url=' + encodeURIComponent('http://127.0.0.1:9991/hang'))
  const problems = []
  if (got.status !== 408) problems.push(`status ${got.status} != 408`)
  if (!got.body.startsWith('Error: ')) problems.push(`body ${JSON.stringify(got.body.slice(0, 60))} !prefix 'Error: '`)
  if (problems.length === 0) { pass++; console.log(`PASS P21 timeout 408 (${Math.round((Date.now() - t0) / 1000)}s)`) }
  else { fail++; failures.push(`P21: ${problems.join(' | ')}`); console.log(`FAIL P21 timeout 408: ${problems.join(' | ')} status=${got.status} body=${JSON.stringify(got.body.slice(0, 120))}`) }
}

console.log(`\nBATTERY: ${pass} passed, ${fail} failed`)
process.exit(fail === 0 ? 0 : 1)
