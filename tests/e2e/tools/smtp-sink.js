// OlliTeX e2e/dev e-mail sink (2026-09-16, owner task T5).
//
// Why this exists instead of a pulled image: this box's registry access does
// not allow the Mailpit image. So the stack runs its own zero-dependency
// Node SMTP sink (image = the locally available git-bridge node image):
//
//   · SMTP  :1025  minimal ESMTP (EHLO / MAIL FROM / RCPT TO / DATA / QUIT),
//                  no auth, no STARTTLS — exactly what a test relay needs
//   · API   :8025  GET  /api/messages  → captured mail (JSON)
//                 DELETE /api/messages → wipe
//                 GET  /api/health
//
// Captured mail is in-memory (disposable stack). The smtp-capture spec reads
// the API over 127.0.0.1:18025 (compose port mapping).

'use strict'
const net = require('net')
const http = require('http')

const SMTP_PORT = Number(process.env.SINK_SMTP_PORT || 1025)
const API_PORT = Number(process.env.SINK_API_PORT || 8025)

const messages = [] // latest last: {from, to[], subject, raw, at}

function subjectOf(raw) {
  const m = raw.match(/^Subject:\s*(.*)$/im)
  return m ? m[1].trim() : ''
}

function log(...a) {
  console.log('[smtp-sink]', ...a)
}

const smtp = net.createServer(socket => {
  socket.write('220 smtp-sink\r\n')
  let state = 'cmd' // cmd | data
  let buf = ''
  let from = null
  const to = []
  let rawParts = []

  const reset = () => {
    state = 'cmd'
    buf = ''
    from = null
    to.length = 0
    rawParts = []
  }

  socket.on('data', chunk => {
    if (state === 'data') {
      buf += chunk.toString('utf8')
      // terminal dot line
      const end = buf.indexOf('\r\n.\r\n')
      if (end !== -1) {
        const raw = buf.slice(0, end)
        messages.push({ from, to: to.slice(), subject: subjectOf(raw), raw, at: Date.now() })
        log('captured mail from=' + from + ' to=' + JSON.stringify(to) + ' subject=' + subjectOf(raw))
        buf = buf.slice(end + 5)
        state = 'cmd'
        socket.write('250 2.0.0 OK accepted\r\n')
      }
      return
    }

    buf += chunk.toString('utf8')
    let idx
    while ((idx = buf.indexOf('\r\n')) !== -1) {
      const line = buf.slice(0, idx)
      buf = buf.slice(idx + 2)
      const up = line.trim().toUpperCase()
      if (up.startsWith('EHLO') || up.startsWith('HELO')) {
        socket.write('250-smtp-sink\r\n250 8BITMIME\r\n')
      } else if (up.startsWith('MAIL FROM')) {
        from = line.match(/<([^>]*)>/)?.[1] || line.replace(/^MAIL FROM:\s*/i, '').trim()
        socket.write('250 2.1.0 OK\r\n')
      } else if (up.startsWith('RCPT TO')) {
        to.push(line.match(/<([^>]*)>/)?.[1] || line.replace(/^RCPT TO:\s*/i, '').trim())
        socket.write('250 2.1.5 OK\r\n')
      } else if (up.startsWith('DATA')) {
        state = 'data'
        buf = ''
        rawParts = []
        socket.write('354 End data with <CR><LF>.<CR><LF>\r\n')
      } else if (up.startsWith('QUIT')) {
        socket.write('221 2.0.0 Bye\r\n')
        socket.end()
        return
      } else if (up.startsWith('RSET')) {
        reset()
        socket.write('250 2.0.0 OK\r\n')
      } else if (up.startsWith('NOOP') || up === '') {
        socket.write('250 2.0.0 OK\r\n')
      } else if (up.startsWith('VRFY')) {
        socket.write('252 2.0.0 Cannot VRFY user\r\n')
      } else {
        socket.write('250 2.0.0 OK\r\n')
      }
    }
  })
  socket.on('error', err => log('socket error:', err.message))
})

const api = http.createServer((req, res) => {
  const send = (code, obj, ctype = 'application/json') => {
    res.writeHead(code, { 'Content-Type': ctype, 'Access-Control-Allow-Origin': '*' })
    res.end(ctype === 'application/json' ? JSON.stringify(obj, null, 2) : obj)
  }
  if (req.method === 'OPTIONS') {
    res.writeHead(204, {
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'GET, DELETE, OPTIONS',
      'Access-Control-Allow-Headers': 'Content-Type',
    })
    return res.end()
  }
  const u = (req.url || '').split('?')[0]
  if (u === '/api/messages' && req.method === 'GET') return send(200, { count: messages.length, messages })
  if (u === '/api/messages' && req.method === 'DELETE') {
    messages.length = 0
    return send(200, { cleared: true })
  }
  if (u === '/api/health') return send(200, { ok: true })
  send(404, { error: 'not found' })
})

smtp.listen(SMTP_PORT, '0.0.0.0', () => log('SMTP listening on :' + SMTP_PORT))
api.listen(API_PORT, '0.0.0.0', () => log('API listening on :' + API_PORT))
