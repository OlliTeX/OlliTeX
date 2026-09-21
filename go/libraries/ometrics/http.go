package ometrics

import (
	"strconv"
	"strings"
	"sync"
)

// Logger is the narrow logging surface RequestLogger drives (Node:
// `logger[this._level](info, '%s %s', method, url)` — the level is dynamic,
// so a single Log(level, ...) entry point mirrors it exactly).
type Logger interface {
	Log(level string, info map[string]any, msg string, args ...any)
}

// RoutePath is `req.route` `{ path }`.
type RoutePath struct {
	Path string
}

// SwaggerPath is `req.swagger` `{ apiPath }`.
type SwaggerPath struct {
	APIPath string
}

// Request is the express request surface http.js reads. `Logger` is assigned
// by Monitor (Node: `req.logger`).
type Request struct {
	Method      string
	URL         string
	OriginalURL string
	Headers     map[string]string
	Route       *RoutePath
	Swagger     *SwaggerPath
	Logger      *RequestLogger
}

// Responder is the express response surface. `Status` is `res.statusCode`
// (nil = unset, matching Node's undefined in the oracle). `End` is the original
// `res.end`, which Monitor replaces with a metrics/logging wrapper.
type Responder struct {
	Status *int
	End    func(...any)
}

// statusAny mirrors `res.statusCode` (nil = undefined).
func statusAny(res *Responder) any {
	if res == nil || res.Status == nil {
		return nil
	}
	return *res.Status
}

// HTTPMonitor mirrors http.monitor: returns middleware `(req, res, next)`.
func HTTPMonitor(logger Logger, level string) func(req *Request, res *Responder, next func()) {
	if level == "" {
		level = "debug"
	}
	return func(req *Request, res *Responder, next func()) {
		startTime := NowMS()
		rl := NewRequestLogger(logger, level)
		req.Logger = rl

		origEnd := res.End
		res.End = func(args ...any) {
			origEnd(args...)
			responseTimeMs := float64(NowMS() - startTime)
			requestSize := parseFloatStr(req.Headers["content-length"])
			if path, ok := getRoutePath(req); ok {
				labels := map[string]any{
					"method":      req.Method,
					"status_code": statusAny(res),
					"path":        path,
				}
				recorder.Timing("http_request", responseTimeMs, labels)
				if requestSize != 0 {
					recorder.Summary("http_request_size_bytes", requestSize, labels)
				}
			}
			rl.AddFields(map[string]any{"responseTimeMs": responseTimeMs})
			rl.emit(req, res)
		}
		if next != nil {
			next()
		}
	}
}

// getRoutePath mirrors http.getRoutePath: `req.route.path` (transformed) →
// `req.swagger.apiPath` → null. Returns (path, ok).
func getRoutePath(req *Request) (string, bool) {
	if req.Route != nil {
		p := req.Route.Path
		p = strings.ReplaceAll(p, "/", "_")
		p = strings.ReplaceAll(p, ":", "")
		if len(p) > 0 {
			p = p[1:] // Node .slice(1)
		}
		return p, true
	}
	if req.Swagger != nil {
		return req.Swagger.APIPath, true
	}
	return "", false
}

// parseFloatStr mirrors `parseInt(headers['content-length'], 10)` ("" → 0).
func parseFloatStr(s string) float64 {
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

// RequestLogger mirrors http.RequestLogger.
type RequestLogger struct {
	logger   Logger
	level    string
	info     map[string]any
	disabled bool
	mu       sync.Mutex
}

// NewRequestLogger builds a RequestLogger (Node: `new RequestLogger(logger, level)`).
func NewRequestLogger(logger Logger, level string) *RequestLogger {
	return &RequestLogger{logger: logger, level: level, info: map[string]any{}}
}

// AddFields merges fields (Node: `addFields` = Object.assign).
func (r *RequestLogger) AddFields(fields map[string]any) {
	r.mu.Lock()
	for k, v := range fields {
		r.info[k] = v
	}
	r.mu.Unlock()
}

// SetLevel mirrors `setLevel`.
func (r *RequestLogger) SetLevel(level string) {
	r.mu.Lock()
	r.level = level
	r.mu.Unlock()
}

// Disable mirrors `disable`.
func (r *RequestLogger) Disable() {
	r.mu.Lock()
	r.disabled = true
	r.mu.Unlock()
}

// Emit mirrors `emit(req, res)`: adds {req,res}, logs at the configured level
// with `'%s %s', method, url`.
func (r *RequestLogger) emit(req *Request, res *Responder) {
	r.mu.Lock()
	disabled := r.disabled
	level := r.level
	logger := r.logger
	info := r.info
	r.mu.Unlock()

	if disabled {
		return
	}
	reqRes := map[string]any{"req": req, "res": res}
	for k, v := range reqRes {
		info[k] = v
	}
	url := req.OriginalURL
	if url == "" {
		url = req.URL
	}
	logger.Log(level, info, "%s %s", req.Method, url)
}
