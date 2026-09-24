# `tags` — web feature package (P7 U1)

The entire project-tags family (Node `services/web/app/src/Features/Tags/` +
`modules/Tags/`):

- `GET  /tag`                     — list the user's tags
- `POST /tag`                     — create (name + optional color; 400 VA / 500 long-name)
- `POST /tag/:tagId/rename`      — rename (400 VA / 500 duplicate-key / 404 bad-oid)
- `POST /tag/:tagId/edit`        — rename+color in one (Node wires it to `rename-tag`'s limiter)
- `DELETE /tag/:tagId`           — idempotent 204
- `POST   /tag/:tagId/project/:projectId`          — add one project (404/405 VA / 204)
- `DELETE /tag/:tagId/project/:projectId`          — remove one project (204)
- `POST   /tag/:tagId/projects`                     — batch add (`{projectIds:[…]}`)
- `POST   /tag/:tagId/projects/remove`              — batch remove
- `GET  /user/:userId/tag`     — private-API-only route → 404 HTML page for everyone

Pinned wire (byte-exact against the Node oracle, `WEB_GO_PLAN.md` U1):

- Tag doc order on the wire: stored `{_id,user_id,name,color?,project_ids,…}`;
  a fresh create is returned in mongoose schema order
  `{user_id,name,color?,project_ids:[_empty_],_id,__v}` (and the duplicate
  create returns the existing doc, stored order).
- `project_ids` are 24-hex strings (mongoose casts ObjectIds on read).
- Rate limits: Node `router.mjs` — create/rename/edit/delete/
  add-projects/rename… are all **30 points / 60 s** keyed by uid
  (`remove-projects-from-tag` is the batch-remove name — `FROM`, not `TO`).
- The 500 pages and the 404 page render through `views/` with the full
  `canManageTemplatesMenu` ladder (`templates.MenuGrant`) + admin nav,
  exactly like every Node page render (`ExpressLocals`).

Read the package doc comments in `tags.go` for the Node source mapping and
the per-route pins. Wired into the binary from `cmd/web/main.go` (repo
root); parity-locked by `tests/e2e/specs/parity/web-go-u1-parity.test.e2e.ts`
(Node == Go == Node, in-container dual-port battery
`parity/u1-battery.cjs`). The nginx flip conf `server-ce/nginx/flips/web-u1.conf`
is kept for the hard-cutover routing (P7 step 2).
