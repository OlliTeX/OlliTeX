package core

import (
	"net/http"
)

// badBody400 — Node express.json+HttpErrorHandler behavior for a scalar-root
// or unparseable JSON body (P3.3 pin "{}" for JSON accept; P6.4a pin: the
// 705B error page for html/negotiated accept). Byte-exact Node body
// (views/general/500 with adminEmail rendered as the string "undefined" —
// this deployment sets no OVERLEAF_ADMIN_EMAIL, pinned via
// POST /user/llm-providers body 'not-json' on the live Node baseline).
var badJSONPage = []byte(`<!DOCTYPE html><html lang="en"><head><title>Something went wrong</title><link rel="icon" href="/favicon.ico"></head><body class="full-height"><main class="content content-alt full-height" id="main-content"><div class="container full-height"><div class="error-container full-height"><div class="error-details"><p class="error-status">Something went wrong, sorry.</p><p class="error-description">There was a problem with your request.</p><p class="error-description">Please go back and try again.
If the problem persists, please contact us at
<a href="mailto:undefined" target="_blank"></a>.</p><p class="error-actions"><a class="btn btn-primary" href="/">Home</a></p></div></div></div></main></body></html>`)

func badBody400(a *App, r *http.Request, res *Res) {
	if a != nil && a.acceptsJSON(r) {
		res.BareWrite(400, []byte("{}"))
		return
	}
	res.W.Header().Set("Content-Type", "text/html; charset=utf-8")
	res.W.WriteHeader(400)
	_, _ = res.W.Write(badJSONPage)
}

// BadJSON400 — exported for features that parse strict JSON bodies after the
// core pass (the core pre-handler already answers for POST/PUT/PATCH/DELETE
// JSON bodies on web-profile routes; this is belt-and-braces parity).
func BadJSON400(a *App, r *http.Request, res *Res) { badBody400(a, r, res) }
