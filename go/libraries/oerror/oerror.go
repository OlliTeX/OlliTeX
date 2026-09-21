// Package oerror is the 1:1 Go port of `libraries/o-error`
// (npm `@overleaf/o-error`, index.cjs).
//
// Node spec (reproduced faithfully; tests + HANDOFF.md §Divergences pin the
// Go adaptations):
//
//	class OError extends Error {
//	  constructor(message, info?, cause?)
//	  withInfo(info) / withCause(cause)        // chainable, return this
//	  static tag(error, message?, info?)       // per-error cap OError.maxTags
//	                                           // (100); DROPPED sentinel slot 1
//	  static getFullInfo(error)                // merged info: cause → info →
//	                                           // tags; last (outermost) wins
//	  static getFullStack(error)               // "Name: message" + tag lines +
//	                                           // "caused by:" chain, 4-space
//	                                           // indent per cause depth
//	}
//
// Go adaptations (documented in go/libraries/HANDOFF.md §Divergences):
//   - Go has no dual stacks / `Error.captureStackTrace`, so `tag`'s V8 stack
//     capture is dropped. The TaggedError *values* (message + info) are the
//     preserved surface; `GetFullStack` renders exactly the line structure
//     the Node suite pins.
//   - Node's `tag` works on *any* object (plain Error, even `{message}`). Go:
//     `Tag` on a non-*OError error wraps it in a *OError so repeated `Tag`
//     calls accumulate on one object (the Node "singleton error" behaviour).
//     A wrapped plain error renders "OError: <its Error() text>".
//   - Node's monkey-patched plain-`Error` fields (`err.cause = ...`,
//     `err.info = ...` on plain Error instances) are not expressible in Go;
//     the Go equivalent is `New(...).WithCause(...) / WithInfo(...)`.
//   - `IsDropped` is structural (message match), not Node's reference-equality
//     `=== DROPPED_TAGS_ERROR` (Go structs can't compare by identity when
//     they hold maps).
package oerror

import "strings"

// MaxTags mirrors `OError.maxTags`: the per-error tag cap (guard against the
// singleton-error leak). Node documents "must be at least 1". Test suites may
// lower it temporarily (the Node suites do) — restore via `MaxTags = 100`.
var MaxTags = 100

// DroppedTags is the `... dropped tags` sentinel occupying slot 1 once the
// cap is hit, mirroring Node's DROPPED_TAGS_ERROR.
var DroppedTags = TaggedError{Message: "... dropped tags"}

// TaggedError is one tag added via Tag. Node records a V8 stack trace here;
// Go keeps message + info and renders as "TaggedError: <message>".
type TaggedError struct {
	Message string
	Info    map[string]any
}

func (t TaggedError) Error() string {
	if t.Message == "" {
		return "TaggedError"
	}
	return "TaggedError: " + t.Message
}

// IsDropped reports whether the tag is the dropped-tags sentinel. Divergence:
// Node checks `=== DROPPED_TAGS_ERROR` (reference equality); Go matches on
// the sentinel's exact message + nil info (a user tag with that message text
// is indistinguishable — document where it matters).
func (t TaggedError) IsDropped() bool {
	return t.Message == DroppedTags.Message && t.Info == nil
}

// OError mirrors the Node OError class. Field mapping:
//
//	message → Message, rendered name → Type ("" = "OError"), info → Info,
//	cause → Cause (any), _oErrorTags → Tags.
type OError struct {
	Type    string
	Message string
	Info    map[string]any
	Cause   any
	Tags    []TaggedError
}

// New mirrors `new OError(message, info?, cause?)`.
//
//	info nil leaves Info unset, matching Node's falsy check.
func New(message string, info map[string]any, cause ...any) *OError {
	e := &OError{Message: message}
	e.WithInfo(info)
	if len(cause) > 0 {
		e.WithCause(cause[0])
	}
	return e
}

// Of mirrors `new OError(message)` — the common no-info, no-cause form.
func Of(message string) *OError { return New(message, nil) }

func (e *OError) Error() string {
	if e.Type == "" {
		return "OError: " + e.Message
	}
	return e.Type + ": " + e.Message
}

// WithName sets the rendered error name (Go stand-in for a Node subclass,
// e.g. `class MyError extends OError { ... }`). Rendered form:
// "MyError: <message>".
func (e *OError) WithName(name string) *OError { e.Type = name; return e }

// WithInfo mirrors `withInfo(info)`: chainable, returns the receiver. Nil
// info is a no-op (Node: `if (info) this.info = info`).
func (e *OError) WithInfo(info map[string]any) *OError {
	if info != nil {
		e.Info = info
	}
	return e
}

// WithCause mirrors `withCause(cause)`: chainable, returns the receiver. Nil
// cause is a no-op (Node: `if (cause) this.cause = cause`).
func (e *OError) WithCause(cause any) *OError {
	if cause != nil {
		e.Cause = cause
	}
	return e
}

// Tag mirrors `OError.tag(err, message, info)` on a *OError instance
// (in-place, Node singleton semantics).
func (e *OError) Tag(message string, info map[string]any) *OError {
	e.tag(message, info)
	return e
}

// tag mirrors the Node cap exactly (ground-traced, see tests):
//
//	if len(tags) >= maxTags:
//	 if len >= 2 && tags[1].isDropped → splice(2, 1)   # drop the sentinel
//	 else                                → tags[1] = DROPPED
//	append t
func (e *OError) tag(message string, info map[string]any) {
	if len(e.Tags) >= MaxTags {
		if len(e.Tags) >= 2 && e.Tags[1].IsDropped() {
			e.Tags = append(e.Tags[:2], e.Tags[3:]...)
		} else if len(e.Tags) == 1 {
			e.Tags = append(e.Tags, DroppedTags)
		} else {
			e.Tags[1] = DroppedTags
		}
	}
	e.Tags = append(e.Tags, TaggedError{Message: message, Info: info})
}

// Unwrap supports stdlib errors.Is/As through any error-typed cause.
func (e *OError) Unwrap() error {
	c, ok := e.Cause.(error)
	if !ok || c == nil {
		return nil
	}
	return c
}

// Tag is the package-level stand-in for the Node static `OError.tag`:
// *OError values are tagged in place (preserving the Node singleton
// behaviour); any other error is wrapped once and tagged (the wrap renders
// "OError: <its Error() text>").
func Tag(err error, message string, info map[string]any) error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*OError); ok {
		e.tag(message, info)
		return e
	}
	e := Of(err.Error())
	e.Type = "OError"
	e.tag(message, info)
	return e
}

// GetFullInfo mirrors `OError.getFullInfo(error)`: merged info across the
// entire cause chain and each level's info + tags; on key collision the
// outermost level wins (Node merges cause FIRST, then its own info/tags).
// nil → {}. Non-*OError values: Node reads `.info` off ANY object in the
// chain — a Go error type can expose its info map by implementing
// InfoProvider (opt-in; plain Go errors carry no info, same as a plain JS
// Error).
func GetFullInfo(err error) map[string]any {
	info := map[string]any{}
	walkInfo(err, info)
	return info
}

// InfoProvider is the Go stand-in for Node `getFullInfo` reading `.info` from
// a NON-OError cause (Node: `if (typeof oError.info === 'object')` applies to
// ANY object, e.g. `const e = new Error('x'); e.info = {a:1}` contributes in
// Node — oracle-verified). Go callers wrap foreign error types behind this
// interface to keep their info visible in GetFullInfo.
type InfoProvider interface {
	OErrorInfo() map[string]any
}

func walkInfo(err error, info map[string]any) {
	if err == nil {
		return
	}
	if e, ok := err.(*OError); ok {
		if c, ok := e.Cause.(error); ok && c != nil {
			walkInfo(c, info) // Node order: cause first
		}
		// Node: `if (typeof oError.info === 'object')` — maps only.
		if e.Info != nil {
			mergeInfo(info, e.Info)
		}
		for _, tg := range e.Tags {
			mergeInfo(info, tg.Info)
		}
		return
	}
	if p, ok := err.(InfoProvider); ok {
		if m := p.OErrorInfo(); m != nil {
			mergeInfo(info, m)
		}
	}
}

func mergeInfo(dst, src map[string]any) {
	for k, v := range src {
		dst[k] = v
	}
}

// GetFullStack mirrors `OError.getFullStack(error)`:
//
//	"<rendered error>"        // "Name: message" (*OError) or Error() text
//	"TaggedError: ..."        // one line per tag
//	"caused by:\n"            // 4-space indent per cause level
//
// nil → "". A non-error `Cause` value renders as "(no stack)" (Node:
// `cause.stack || cause.message || '(no stack)'` on a non-error object).
func GetFullStack(err error) string {
	if err == nil {
		return ""
	}
	var base string
	var tags []TaggedError
	var cause any
	if e, ok := err.(*OError); ok {
		base, tags, cause = e.Error(), e.Tags, e.Cause
	} else {
		base = err.Error()
	}
	out := base
	if len(tags) > 0 {
		lines := make([]string, len(tags))
		for i, tg := range tags {
			lines[i] = tg.Error()
		}
		out += "\n" + strings.Join(lines, "\n")
	}
	cs := causeStack(cause)
	if cs != "" {
		out += "\ncaused by:\n" + indent(cs)
	}
	return out
}

func causeStack(cause any) string {
	switch c := cause.(type) {
	case *OError:
		if c == nil {
			return ""
		}
		return GetFullStack(c)
	case error:
		if c == nil {
			return ""
		}
		return GetFullStack(c)
	case nil:
		return ""
	default:
		return "(no stack)"
	}
}

func indent(s string) string {
	var b strings.Builder
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("    " + line)
	}
	return b.String()
}
