# go-i18n evaluation (P7-post item 4)

**Date:** 2026-09-25 · **Status:** evaluation complete; recommendation = **defer adoption,
one seam is in place (email templates), two candidates shortlisted** if we ever build it.

Owner scope (remember.md P7-post item 4): *"evaluate go-i18n … The recommendation
should focus on Go-rendered strings, e-mail template text, and future error strings.
A full product-wide i18n migration is out of scope."*

---

## 1. The surface we'd actually translate (measured, this tree)

| Surface | Where | Size | i18n-fit |
|---|---|---|---|
| **E-mail templates** | `go/services/web/features/emailtemplates` (centralized 2026-09-25, item 3) | 13 slots, all text in one registry | **Best-fit**: static text, one call path (`Render`), admin-editable today |
| **Go-rendered HTML views** | `go/services/web/views/*` (baked) + page data in `core/`, `features/*` | ~540KB of baked HTML; ~440 hardcoded English strings in Go views/features | Moderate: the *live* product UI is TypeScript/frontend-rendered (already has i18n); what Go serves is page shells, the editor shell, admin page, /hub page |
| **API error strings** | all `features/*` JSON contracts | hundreds of distinct strings | **Worst-fit**: see §3.2 |
| **Frontend UI (dominant)** | `frontend/js/**` + baked bundles | 4672 keys in `locales/en.json`, 18 locale files | **Already solved** — Node legacy shipped `locales/*.json` consumed by the frontend i18n layer; not a Go problem |

Key structural fact: post-cutover **Go is the server; the browser still runs the
TypeScript app** (which carries its own i18n over the same `locales/*.json`). The
user-visible product surface is therefore *mostly already i18n-ready*, and the Go
surface that remains is (a) e-mail, (b) server-rendered shells/pages, (c) API errors.

## 2. Candidate libraries

Measured against: zero-runtime-cost default-locale fast path; extraction DX for a
Go codebase that pins bytes; ICU vs simple placeholders; catalog compatibility with
the existing `locales/*.json`; maintenance health.

| Library | Model | Catalog format | Verdict |
|---|---|---|---|
| **nicksnyder/go-i18n/v2** (github.com/nicksnyder/go-i18n/v2) | `Bundle` + `Translator`; per-locale `.json`/`.po`/`.resjson` catalogs; ICU MessageFormat (pluralization, gender, select) | JSON or PO — **can load the existing `locales/*.json`** with a key-adapter | **Primary candidate**. Largest install base in the Go ecosystem; active; the ICU path gives us plural rules for free when we ever need them (e.g. "3 days ago") |
| **hashicorp/go-i18n v2** | fork-line of the v1 API | JSON (`{"key": "value"}`) | Solid & boring; ICU support arrived late; smaller footprint. Fine for flat placeholder strings; weaker than nicksnyder for growth |
| **hand-rolled `map[string]string` per locale** (what `emailtemplates` effectively is today) | one `map[locale]string` table + `Lookup` | our own JSON | Honest option for the e-mail-only scope: we already have exactly that shape (`{{var}}` interpolation + per-slot fields) and the admin UI to edit it; zero new dependency |
| **x/text** | collation/formatting, no catalog | — | Not a candidate (different job) |

**Extraction DX note:** nicksnyder expects annotated source (`// i18n: key {v}`
comments) processed by `goi18n extract` into `.po` catalogs. Our strings are
*pinned literals* (byte-parity discipline), so extraction would be an **opt-in
marker pass** over a *subset* of files (the list in §3.1), not a repo-wide scan.

## 3. Recommendation

### 3.1 Adopt a small library, but only at two seams (if/when we start)

If we do i18n, the order of work should be:

1. **E-mail first** — `emailtemplates.Render` already takes a `vars map[string]string`
   and the admin UI already edits per-slot text. i18n here is a **catalog dimension
   on the existing store** (document: `{locale} → {subject,text,html}`), resolved
   from the recipient's `user.language` (we already persist per-user language in the
   `user` docs) or `Accept-Language` for anonymous senders (reset links). This reuses
   item 3's machinery 1:1; a hand-rolled locale table is defensible *here*.
2. **Go-rendered shell strings second** — introduce **nicksnyder/go-i18n/v2** in
   `go/libraries/i18n` (new package), load `locales/*.json` (the same catalogs the
   frontend already ships — one source of truth), and replace the ~440 hardcoded
   strings in `views/` + page-data builders through one `T(locale, key, vars)` seam.
   ICU format keeps the door open for plurals later.
3. **API error strings last, and mostly NO** (below).

### 3.2 API error strings: recommend NOT translating (and say why, in the open)

- They are **contract surface**: the Go API contracts are byte-pinned to the Node
  oracles (the `parpins_test.go`-style discipline that makes this port drop-in).
  Translating `error` strings changes behavior for every consumer (frontend,
  integrations, tests) and breaks the oracle unless the oracle is translated too.
- The **frontend translates error *codes/keys* it already understands** via its
  own i18n layer — the right place. The API should keep emitting stable
  machine-oriented strings (as Node does), and the UI localizes display.
- Where a Go-rendered *page* shows an error (e.g. login "Invalid email or
  password"), that string is §3.1 item 2's territory (view layer), not the API
  contract.

### 3.3 What NOT to do

- A full product-wide migration (out of scope per owner; the frontend already has
  its own pipeline).
- Adding a second catalog format (PO) on top of the existing `locales/*.json` —
  keep one source of truth; adapter-in, don't fork.
- Translating log lines or admin-internal diagnostics.

## 4. Concrete adoption sketch (for the "when", not the "now")

```
go/libraries/i18n/
  i18n.go    — Bundle singleton; T(locale, key, vars...) (string, ok bool)
               default locale = "en"; unknown locale → "en" fallback (documented)
  bundle.go  — NewBundleFromJSONDir(path) over locales/*.json
               (en.json keys are the raw English strings → identity default;
                missing key → key itself = safe fallback, zero data loss)
  extract.md — the marker annotation + goi18n extraction pass (only §3.1 lists)
  i18n_test.go — T("de","…") vs locales/de.json; fallback chain; ICU plural pin
```

Wiring points (all optional seams, default locale keeps today's bytes):

- `core.App` carries `I18n *i18n.Bundle` (nil = English-only mode = current
  behavior; e2e stack stays byte-identical by default).
- `emailtemplates.Render(...)` gains `T func(locale,key string, vars map[string]string) (string,bool)`
  — call sites pass it in only after the catalog exists.
- View builders take the bundle from `core.App` where present.

Test strategy: **locale matrix test** (en/de/… × every key referenced in the two
seams) asserting key presence in `locales/<locale>.json` (catches missing
translations at build time, not in production).

## 5. Decision requested from owner

1. **Adopt now?** Recommendation: **no** — the only high-value surface (e-mail)
   is already admin-editable per-instance (item 3), which is what most
   deployments actually need; catalog i18n adds value mainly once a *specific
   language target* is in scope.
2. **If yes**, do §4 first (library + bundle + one e-mail slot in German as the
   canary), gate on the locale-matrix test, then broaden.

## 6. Evidence anchors (this tree)

- `locales/en.json` — 4672 keys; 18 locale files (cs da de en es fi fr it ja ko
  nl no pl pt ru sv tr zh-CN) — the frontend's existing catalogs.
- `go/services/web/features/emailtemplates/templates.go` — the centralized
  registry (item 3) with the `vars` interpolation seam where a catalog
  translation function would hang.
- `go/services/web/views/` — baked Go-rendered HTML (~540KB); the ~440-string
  surface for §3.1 item 2.
- `go/libraries/HANDOFF.md` — the library-convention home a new `i18n` package
  would join.
