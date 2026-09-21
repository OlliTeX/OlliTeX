# `go/services/web/contract` — the Node route inventory

`routes.csv` — the machine-readable flip checklist extracted from the Node
router (`services/web/app/src/router.mjs` + the module routers):
160 routes as `method,path,router,feature,source`.

Use it to (a) see which routes a flip must cover, (b) audit parity after a
flip (every row for a surface has a Go owner in `features/` or an explicit
documented skip), and (c) find the exact Node source line behind any route
(`source` = file:line in the Node tree).

Regenerate when the Node router changes (the extraction is a grep/parse over
the router files — see the phase notes in `WEB_GO_PLAN.md` for the original
extraction command family).
