// Deterministic upstream for the linked-url-proxy contract battery (loopback only).
// Serves: 200 ok / 404 / 302->200 / 302-loop / 413 (2MB CL) / 35s hang.
const http = require('node:http')
const srv = http.createServer((req, res) => {
  const p = req.url
  if (p.startsWith('/ok')) {
    res.writeHead(200, { 'Content-Type': 'text/plain', 'Content-Length': Buffer.byteLength('hello-linked-url') })
    return res.end('hello-linked-url')
  }
  if (p.startsWith('/missing')) {
    res.writeHead(404, { 'Content-Type': 'text/plain' })
    return res.end('nope')
  }
  if (p.startsWith('/redirect')) {
    res.writeHead(302, { Location: '/ok' })
    return res.end()
  }
  if (p.startsWith('/loop')) {
    res.writeHead(302, { Location: '/loop' })
    return res.end()
  }
  if (p.startsWith('/big')) {
    res.writeHead(200, { 'Content-Type': 'application/octet-stream', 'Content-Length': 2 * 1024 * 1024 })
    return res.end()
  }
  if (p.startsWith('/hang')) {
    setTimeout(() => { res.writeHead(200); res.end('late') }, 35000)
    return
  }
  res.writeHead(500, { 'Content-Type': 'text/plain' })
  res.end('unknown')
})
srv.listen(9991, '127.0.0.1', () => console.log('lup-upstream on 127.0.0.1:9991'))
