1. SQLlite config database: 
  The idea was to move all the env parameter that are not directly required for starting the docker container into a SQLite config database.
  These former env parameter should be manged by the admin part of /hub. However, a cli tool as a backup would be appreciated.  

2. We need to implement a GDPR-compliant cookie consent system. 
I asked Gemini LLM about suggestions:
[->]
- Frontend HTML/JS snippet for the cookie consent banner that captures user preferences (Necessary, Analytics, Marketing).
- Go HTTP handlers and middleware to handle setting, parsing, and validating secure, HTTP-only consent cookies (SameSite=Lax/Strict, Secure).
- Server-side logic in Go to conditionally render/inject tracking scripts or execute backend logic based on the user's cookie consent state."
[<-]

[2026-09-25 status: IMPLEMENTED — GET/POST /cookie-consent (Go core, CSRF on POST, allowlisted values, secure canonical cookie — deliberately not HttpOnly because the first-party JS consent gate must read it), GET /legal cookie/privacy policy (the banner's link target), frontend setConsent now records the choice server-side (with the legacy http/Secure cookie bug fixed), e2e contract spec 8/8 green. Commits 6e8ca411f5 / 76c481361c / 52f9f25ce8.]

3. email templates: Manage the email template under /hub admin settings (rebrand them templates to OlliTeX too). Text areas with save and reset to default

4. We need to work on our i18n in the project. We should check if this will help us:
https://github.com/nicksnyder/go-i18n 
go-i18n is a Go package and a command that helps you translate Go programs into multiple languages.

[2026-09-29 status: AUDITED AND ADOPTED. Integration verified against the library README + v2.6.1 source (your clone at ../go-i18n): correct — every call site matches the documented contract; two deliberate documented deviations (no DefaultMessage — the English-default seam owns byte-pinning; CE locale policy ignores q-values). Landed: phase 1 email seam (German canary) + wave A navbar/launchpad + wave B shell pages (login/register/logout/404/500/restricted/set-password/sessions) — en byte-identical for the e2e battery, de renders real German. Wave C scoped: admin pages + /user/settings page strings + one-time-login. See docs/go-i18n-evaluation.md (status section) + TODO-89b7ceba.]

5. mongo:8.3: Make sure that server-ce dev & co uses mongo:8.3 and not mongo:6.0



6. [2026-09-29 nightly arc, for the owner's morning review]
   - d5dd23dd Yjs history hybrid: COMPLETE. Final slices: S4 (Go DU call-surface retired, cb61485259) + S5 (owner directive executed: Node document-updater / history-v1 / project-history trees -> junk/, their runit units, crons, bin helpers, preshutdown hooks, env + workspace + develop-compose wiring removed — 134afebcc0).
   - D41-DU: COMPLETE (DU fully retired from the stack).
   - otc / Go OT plane (35ed23bd stage 2): NEEDS YOUR DECISION — (a) scope a wave porting the OT-era version features (labels / version-zip / blob download / change-list / restore-revert) to the Yjs plane, THEN retire the Go OT services + battery re-pin; (b) retire now and accept loss of those features on pre-cutover rooms (battery rows retire); (c) keep the Go OT plane as the read-only legacy version store (stable: Yjs = live, OT plane = legacy). Evidence + route map in TODO-35ed23bd.
   - D27 dependency refresh: mongo-driver v1.17.10 -> v2.9.1 DONE + GATED + PUSHED (db9b9913d2; 159 files, full race suite green). Next for that arc: the live e2e battery + image re-bake run against v2 (owner-gated), then we're done with dependencies (go-redis/x-crypto/aws-sdk already current; dsnet/compress stays — klauspost has no bzip2 through v1.20.1, verified).
   - Next in the approved pipeline: D22 observability (Prometheus/Grafana, 8cbc1526), D39 re-confirm after the bake (47cfa663), terminal audit (9fbda0eb).
