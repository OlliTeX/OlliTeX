/* Oracle: pin exact EmailBuilder output (subject/html/text) for the two
 * cron emailTypes, from the real Node implementation (temp script, not
 * committed). Run: cd services/web && node tmp-cronmail-oracle.mjs > /tmp/cronmail-oracle.json
 */
process.env.APP_NAME = 'OlliTeX'
process.env.PUBLIC_URL = 'http://127.0.0.1:4000'
process.env.ADMIN_EMAIL = 'admin@site.test'

const EmailBuilderMod = await import('./app/src/Features/Email/EmailBuilder.mjs')
const { buildEmail } = EmailBuilderMod.default

const fixtures = [
  {
    id: 'pn-comment-safe',
    type: 'projectNotification',
    opts: {
      to: 'anna@example.org',
      projectId: 'abc123def456abc123def456',
      projectName: 'My Great Project',
      userName: 'Ann A',
      threadId: 'thr123',
      isComment: true,
    },
  },
  {
    id: 'pn-reply-safe',
    type: 'projectNotification',
    opts: {
      to: 'bob@example.org',
      projectId: 'aaaabbbbccccddddeeeeffff',
      projectName: 'reply-proj',
      userName: 'Bob B',
      threadId: 'thr9',
      isComment: false,
    },
  },
  {
    id: 'pn-comment-escapey',
    type: 'projectNotification',
    opts: {
      to: 'c@example.org',
      projectId: 'p2',
      projectName: `A & B <tag> "q" 'a'`,
      isComment: true,
    },
  },
  {
    id: 'pn-comment-fallback',
    type: 'projectNotification',
    opts: { to: 'd@example.org', projectId: 'p3', projectName: 'project', isComment: true },
  },
  {
    id: 'tc-safe',
    type: 'trackedChangesNotification',
    opts: { to: 'e@example.org', projectId: 'p4', projectName: 'Tracking Project' },
  },
  {
    id: 'tc-escapey',
    type: 'trackedChangesNotification',
    opts: { to: 'f@example.org', projectId: 'p5', projectName: `X & Y "z"` },
  },
]

const out = {
  env: { APP_NAME: process.env.APP_NAME, PUBLIC_URL: process.env.PUBLIC_URL, ADMIN_EMAIL: process.env.ADMIN_EMAIL },
  fixtures: [],
}
for (const f of fixtures) {
  const e = buildEmail(f.type, { ...f.opts })
  out.fixtures.push({ id: f.id, type: f.type, opts: f.opts, subject: e.subject, html: e.html, text: e.text })
}
process.stdout.write(JSON.stringify(out, null, 1) + '\n')
