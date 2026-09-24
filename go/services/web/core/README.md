# `go/services/web/core` — the web request framework

The small framework every feature package builds on (mirrors the Node
express stack: app, middleware, session, csrf, mail, mongo lazy client).

| file | responsibility |
| --- | --- |
| `app.go` | `App` (feature/route registry, dispatch), `Route` (`Method`, `Path`, `NoLogin`, `NoSession`, `Handler`), global login gate + CSRF check (`x-csrf-token`/`_csrf`), request shaping (SiteURL, X-Forwarded handling) |
| `response.go` | `Res` — `Status`/`JSON`/`HTML`/`Redirect` (Node-exact 302 body wording per Accept), `EtagWeakBody` (`W/"<hexlen>-<sha1-base64>"`), header helpers |
| `session.go` | `Session` — cookie `overleaf.sid`, csrf secret + render token (`csrf@3.1`-compatible), passport-style user slots (`IsLoggedIn`, `CsrfToken`, `CsrfSecret`) |
| `authorize.go` | session-user authorization checks (admin/site-capability) backed by Mongo `users` reads |
| `mongo.go` | `MongoLazy` — shared client, `DB(ctx)` database accessor (wired by `cmd/web` via `App.SetMongo`) |
| `mail.go` | `Mail` / `NewMail` — the SMTP send path (`Send(to, subject, text, html)`), env-configured (`OVERLEAF_EMAIL_HOST/PORT/FROMUSER…`), the same message bytes Node's nodemailer emits |
| `tokens.go` | one-time tokens (confirm/password-reset) — Node `OneTimeToken` parity on the `OneTimeToken` collection |
| `redis.go` | csrf@3.1 `CsrfToken`/`VerifyCsrfToken` (HMAC render-token family) + the redis seam |
| `headers.go` | the shared response-header vocab (CSP presets incl. `cspReact`, COOP/CORP, permissions-policy, nosniff) — the values are oracle-pinned per page family |
| `pagedata.go` | `PageData` — the slot vocabulary the views render (Nonce/CSRF/Origin/Origin/`CANMGTPL`, `LPADM`, `LPUID`, …) |
| `limiter.go` | the rate-limit seam (Node `rate-limit` middleware parity) |
| `badjson.go` | malformed-JSON 400 shape parity (`{}` + Node's 400 page) |
| `config.go` | the small runtime config object shared by features |
| `edstate.go` | `EditorGate` — the Node process-level `Settings.editorIsOpen` gate parity (P3.1) |
| `usersessions.go` | `UserSessions` — the Node `UserSessionsManager` parity surface (track/clear on login) |

Non-Go-ism policy (repo-wide): each deviation gets the narrowest faithful
equivalent plus a test pin that names the Node behaviour being mirrored.
