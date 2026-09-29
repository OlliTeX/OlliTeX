package apps

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	cc "ollitex/go/services/clsitex/compilecontroller"
	"ollitex/go/services/clsitex/compilemanager"
	clserrors "ollitex/go/services/clsitex/errors"
	"ollitex/go/services/clsitex/logger"
)

// --- error middleware (app.js 225-233, verbatim chain) ----------------------

// handleValidationError ports the Node express error middleware:
//
//	app.use(function (error, req, res, next) {
//	  if (error instanceof Errors.NotFoundError)        res.sendStatus(404)
//	  else if (error instanceof Errors.InvalidParameter) res.status(400).send(error.message)
//	  else if (error.code === 'EPIPE')                    res.sendStatus(503)
//	  else { logger.error(...); res.sendStatus(error.statusCode || 500) }
//	})
//
// Go divergences:
//   - There is no Go error type carrying a statusCode, so the final branch
//     always renders 500 (the Node `error.statusCode || 500` fallback).
//   - The express chain runs for ANY error returned by a handler, even
//     after the handler already wrote a body (Node fires next(error) after
//     a failed mid-body write). In Go a handler returning a non-zero status
//     plus error means the body is on its way anyway, so the middleware
//     only renders when the handler wrote nothing (code == 0) and logs the
//     write-error case instead. The four mappings and their priority
//     (NotFound -> InvalidParameter -> EPIPE -> 500) are unchanged.
func (a *App) handleValidationError(w http.ResponseWriter, code int, url string, err error) {
	if err == nil {
		return
	}
	if code != 0 {
		// The handler already wrote a body; the body is on its way (Node
		// fired next(error) after a failed mid-body write). Log only.
		logger.Error(map[string]any{"err": err.Error(), "code": code, "url": url},
			"error after partial write")
		return
	}
	switch {
	case clserrors.IsNotFoundError(err):
		logger.Debug(map[string]any{"err": err.Error(), "url": url},
			"not found error")
		a.plainStatus(w, 404)
	case clserrors.IsInvalidParameter(err):
		// Node: res.status(400).send(error.message)
		logger.Debug(map[string]any{"err": err.Error(), "url": url},
			"invalid parameter error")
		a.plainStatus(w, 400)
	case isEPipeError(err):
		// inspect container returns EPIPE when shutting down
		a.plainStatus(w, 503) // 503 Unavailable
	default:
		logger.Error(map[string]any{"err": err.Error(), "url": url},
			"server error")
		a.plainStatus(w, 500) // error.statusCode || 500 (Go errors carry no code)
	}
}

// plainStatus renders the Node res.sendStatus(code) body (text/html + "<code>
// <text>\n") on the per-request writer the route wrapper hands over.
func (a *App) plainStatus(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = fmt.Fprintf(w, "%d %s\n", code, http.StatusText(code))
}

// --- status line helper (Node: `res.send(line)`) ----------------------------

// statusLine is the one-line body Node's /status and the LB agent push.
func statusLine(text string) string { return text }

// --- query parsing helpers (Node: zod coerce + fallback) --------------------

// coerceInt ports z.coerce.number().int(): empty/absent => 0 (zod coerce of
// "" yields 0 after Number("") semantics via the validation-tools fallback:
// the primary schema coerces, and the fallback schema does the same; an
// unparseable value fails the request before it reaches here).
func coerceInt(raw string) int {
	n, _ := strconv.Atoi(raw)
	return n
}

// parseSyncQuery ports the shared synctex query object
// (file/line/column/page/h/v + imageName/editorId/buildId/
// compileFromClsiCache; routing-only fields are accepted but ignored, as in
// Node).
func parseSyncQuery(v url.Values) cc.SyncQuery {
	var sq cc.SyncQuery
	sq.File = v.Get("file")
	sq.Line = coerceInt(v.Get("line"))
	sq.Column = coerceInt(v.Get("column"))
	sq.Page = coerceInt(v.Get("page"))
	sq.H = coerceInt(v.Get("h"))
	sq.V = coerceInt(v.Get("v"))
	sq.ImageName = v.Get("imageName")
	sq.EditorID = v.Get("editorId")
	sq.BuildID = v.Get("buildId")
	sq.CompileFromClsiCache = v.Get("compileFromClsiCache") == "true"
	return sq
}

// --- lifespan guard (app.js 194-209) ---------------------------------------

// startLifespanGuard ports the Node
//
//	Settings.processTooOld = false
//	if (Settings.processLifespanLimitMs) {
//	  Settings.processLifespanLimitMs -= Settings.processLifespanLimitMs
//	                                  * (Math.random() / 10)
//	  logger.info({target}, 'Lifespan limited')
//	  setTimeout(() => { logger.info('shutting down, process is too old')
//	    Settings.processTooOld = true }, Settings.processLifespanLimitMs)
//	}
//
// The jitter (up to 1h on a 24h limit, spreading VM cycling by up to 2.4h in
// Node terms) is applied identically. Returns a stop func for the cmd
// layer. Documented Go divergence: Node reads Math.random(); Go uses the
// same formula with the injectable rng seam (default math/rand/v2, no import
// churn — seeded from the process clock at construction, as cmd does).
func startLifespanGuard(processTooOld *bool, limitMs int64,
	jitter func() float64) func() {
	if limitMs <= 0 {
		return func() {}
	}
	limitMs -= int64(float64(limitMs) * (jitter() / 10))
	target := time.Now().Add(time.Duration(limitMs) * time.Millisecond)
	logger.Info(map[string]any{"target": target.String()}, "Lifespan limited")
	stop := make(chan struct{})
	go func() {
		t := time.NewTimer(time.Duration(limitMs) * time.Millisecond)
		defer t.Stop()
		select {
		case <-t.C:
			logger.Info(map[string]any{}, "shutting down, process is too old")
			*processTooOld = true
		case <-stop:
		}
	}()
	return func() {
		select {
		case <-stop:
		default:
			close(stop)
		}
	}
}

// --- EPIPE (app.js error-code check) ----------------------------------------

func isEPipeError(err error) bool {
	if err == nil {
		return false
	}
	var e *compilemanager.CompileRunError
	if errors.As(err, &e) {
		return e.Code != nil && *e.Code == "EPIPE"
	}
	return strings.Contains(err.Error(), "EPIPE")
}
