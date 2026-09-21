package ologger

import (
	ollitex "ollitex/go/libraries/oerror"
)

// Req is the Go stand-in for the express request object the req-serializer
// reads from. `Params` is the (possibly throwing, under lockdown) `req.params`;
// when `LockdownInstalled` is set the manager MUST read `RawParams` (the
// symbol-keyed raw accessor) instead of `Params` - mirroring
// `getRawReqInput` in @overleaf/validation-tools.
type Req struct {
	Method      string
	OriginalURL string
	URL         string
	IP          string
	Headers     map[string]string

	// Params is `req.params`. Under lockdown (LockdownInstalled) it is the
	// throwing getter and must NOT be read; RawParams is the safe source.
	Params            map[string]any
	RawParams         map[string]any
	RawQuery          map[string]any
	RawBody           any
	LockdownInstalled bool
}

// getRawReqInput mirrors `getRawReqInput(req)` from @overleaf/validation-tools:
// returns the `{params}` the serializer may safely inspect, routing around the
// lockdown throwing getter when installed.
func getRawReqInput(req any) map[string]any {
	r, ok := req.(Req)
	if !ok {
		return nil
	}
	if r.LockdownInstalled {
		if r.RawParams != nil {
			return r.RawParams
		}
		return nil
	}
	if r.Params != nil {
		return r.Params
	}
	return nil
}

// getRemoteIp mirrors getRemoteIp in serializers.js:
// `req.headers['x-forwarded-from']?.split(',')[0] || req.ip`.
func getRemoteIp(req Req) string {
	if h, ok := req.Headers["x-forwarded-from"]; ok && h != "" {
		// first comma-delimited segment (JS .split(',')[0])
		for i := 0; i < len(h); i++ {
			if h[i] == ',' {
				return h[:i]
			}
		}
		return h
	}
	return req.IP
}

// errSerializer mirrors errSerializer in serializers.js. Node:
//
//	if (!error) return {}
//	if (error instanceof OError) return {message: OError.getFullInfo(error).message, stack: OError.getFullStack(error)}
//	return {message: error.message, stack: error.stack}
//
// Go: an `error` value yields {message: Error(), stack: GetFullStack(err)}
// (GetFullStack renders OError name:msg + tags + caused-by, plain errors just
// the message). A nil/absent value yields {}.
func ErrSerializer(value any) any {
	switch v := value.(type) {
	case nil:
		return map[string]any{}
	case error:
		return map[string]any{
			"message": v.Error(),
			"stack":   ollitex.GetFullStack(v),
		}
	case map[string]any:
		// Node plain-object error: return .message/.stack if present.
		out := map[string]any{}
		if m, ok := v["message"]; ok {
			out["message"] = m
		}
		if s, ok := v["stack"]; ok {
			out["stack"] = s
		}
		return out
	}
	return map[string]any{}
}

// reqSerializer mirrors reqSerializer in serializers.js.
func ReqSerializer(value any) any {
	if value == nil {
		return nil // Node: return (falsy) req untouched
	}
	req, ok := value.(Req)
	if !ok {
		return value // not an express request we can normalize: untouched
	}

	entry := map[string]any{
		"method":        req.Method,
		"url":           firstNonEmpty(req.OriginalURL, req.URL),
		"remoteAddress": getRemoteIp(req),
		"headers": map[string]any{
			"referer":        firstNonEmpty(req.Header("referer"), req.Header("referrer")),
			"user-agent":     req.Header("user-agent"),
			"content-length": req.Header("content-length"),
		},
	}

	params := getRawReqInput(req)
	if params != nil {
		if v, ok := firstPresent(params, "projectId", "project_id"); ok {
			entry["projectId"] = v
		}
		if v, ok := firstPresent(params, "userId", "user_id"); ok {
			entry["userId"] = v
		}
		if v, ok := firstPresent(params, "docId", "doc_id"); ok {
			entry["docId"] = v
		}
	}
	return entry
}

// resSerializer mirrors resSerializer: returns {}.
func ResSerializer(any) any {
	return map[string]any{}
}

// Req convenience accessor (case-insensitive header lookup).
func (r Req) Header(key string) string {
	for k, v := range r.Headers {
		if equalFold(k, key) {
			return v
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// firstPresent mirrors JS `a ?? b`: first non-nil value, ok=false if none.
func firstPresent(m map[string]any, keys ...string) (any, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v, true
		}
	}
	return nil, false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
