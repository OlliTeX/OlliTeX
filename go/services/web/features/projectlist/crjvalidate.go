package projectlist

// changes/reject body validation — a hand-written mirror of Node's
// trackChangesRejectedSchema (services/web/app/src/Features/Documents/
// DocumentController.mjs) + the express.json strict body-parser boundary.
//
// Schema (zod):
//
//	body = z.strictObject({
//	  rejectedChangeAuthorIds: z.array(zz.objectId())   // required
//	  userId: zz.objectId().nullish()                    // optional, may be null
//	  previews: z.array(changePreview).optional()        // optional
//	})
//	changePreview = z.strictObject({
//	  sectionPath: z.array(z.string())
//	  startLine:   z.number().int().min(1)
//	  changes:     z.array(z.strictObject({ i?: string, d?: string, p: z.number().int().min(0) }))
//	  slice:       z.string()
//	  sliceStart:  z.number().int().min(0)
//	  userIds:     z.array(zz.objectId())
//	})
//
// Wire (pinned Node api :3000, 2026-09-24):
//   - valid object body            → 204 (empty body; XPB via route wrapper;
//     ETag W/"a-..." over "No Content")
//   - any zod violation            → 400 application/json; charset=utf-8,
//     {"error":"Validation error: <issue>; <issue>…","statusCode":400}
//     (XPB; all issues collected, SCHEMA-declaration order, then unknown-keys,
//     joined with "; ")
//   - array body                   → 400 JSON "expected object, received array at body"
//   - scalar / null / bad-JSON body→ 400 text/html; charset=utf-8 (705B express
//     error page; XPB; W/"2c1-…" etag)
//
// Issue grammar (zod-validation-error):
//   Invalid input: expected <exp>, received <got> at "<path>"
//   Invalid Mongo ObjectId at "<path>"
//   Too small: expected number to be >=<min> at "<path>"
//   Unrecognized key: "k" at "<path>"   (…Unrecognized keys: for ≥2)
//
// int-enforcement short-circuits min: a non-int number reports only
// "expected int, received number" (never also "Too small").

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"ollitex/go/services/web/core"
)

// crjHTML705 — the exact 400 page Node returns on a scalar / null /
// unparseable JSON body (express.json strict body-parser error). Pinned
// byte-exact on :3000 (705B; two \n at offsets 494/540; no backticks).
const crjHTML705 = `<!DOCTYPE html><html lang="en"><head><title>Something went wrong</title><link rel="icon" href="/favicon.ico"></head><body class="full-height"><main class="content content-alt full-height" id="main-content"><div class="container full-height"><div class="error-container full-height"><div class="error-details"><p class="error-status">Something went wrong, sorry.</p><p class="error-description">There was a problem with your request.</p><p class="error-description">Please go back and try again.
If the problem persists, please contact us at
<a href="mailto:undefined" target="_blank"></a>.</p><p class="error-actions"><a class="btn btn-primary" href="/">Home</a></p></div></div></div></main></body></html>`

func crjSend705(res *core.Res) {
	// express err-handler runs BEFORE the csrf/helmet middleware: NO CSP /
	// nosniff / referrer — only content-type(text/html), X-Powered-By, weak
	// ETag, Content-Length. Pinned :3000 (2026-09-24): full-header match.
	h := res.W.Header()
	for _, k := range []string{
		"Referrer-Policy", "X-Content-Type-Options", "X-Download-Options",
		"X-Frame-Options", "X-XSS-Protection", "X-Permitted-Cross-Domain-Policies",
		"Cross-Origin-Opener-Policy", "Cross-Origin-Resource-Policy",
		"Cache-Control", "Expires", "Pragma", "Surrogate-Control",
		"Permissions-Policy", "Set-Cookie", "Content-Security-Policy",
	} {
		h.Del(k)
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Powered-By", "Express")
	h.Set("ETag", core.EtagWeakBody(crjHTML705))
	h.Set("Content-Length", strconv.Itoa(len(crjHTML705)))
	res.W.WriteHeader(400)
	_, _ = res.W.Write([]byte(crjHTML705))
}

// crjSendVA — 400 JSON validation error (joined issues already escaped).
func crjSendVA(res *core.Res, joined string) {
	res.JSON(400, []byte(`{"error":"Validation error: `+joined+`","statusCode":400}`))
}

func crjSend204(res *core.Res) {
	// Node sends status 204 — express computes the weak ETag over the
	// (stripped) 10-byte "No Content" body: W/"a-…".
	res.W.Header().Set("ETag", core.EtagWeakBody("No Content"))
	res.NoContent()
}

// crjServe — api-profile body handling AFTER the basic-auth gate.
func crjServe(cxt *core.Cxt, res *core.Res, req *http.Request) {
	obj, kind, ok := apReadBody(req)
	switch {
	case ok:
		joined, valid := crjValidate(obj)
		if !valid {
			crjSendVA(res, joined)
			return
		}
		crjSend204(res)
	case kind == "array":
		crjSendVA(res, `Invalid input: expected object, received array at \"body\"`)
	default: // scalar / null / bad-JSON → express err-handler: Accept-negotiated
		crjRawErr(cxt, res)
	}
}

// crjRawErr — Node's express error handler content-negotiates a parse /
// strict-JSON rejection (scalar / null / bad-JSON root):
//
//	req.accepts('html')   → 400 HTML 705B error page (text/html; XPB; W/"2c1-…")
//	…req.accepts('json')  → 400 JSON {} (application/json; charset=utf-8; XPB; W/"2-…")
//	neither              → 400 empty
//
// html is checked FIRST ("text/html, application/json" → HTML); a media type
// with q=0 never matches; */* / text/* / application/* are wildcards.
// Pinned :3000 (2026-09-24): no-accept|text/html|*/* → HTML 705B;
// accept:application/json → 400 `{}`.
func crjRawErr(cxt *core.Cxt, res *core.Res) {
	accept := cxt.Req.Header.Get("Accept")
	if crjAcceptsType(accept, "text/html") {
		crjSend705(res)
		return
	}
	if crjAcceptsType(accept, "application/json") {
		// BareWrite: body-parser rejection precedes the csrf/helmet middleware,
		// so the 400 {} carries NO CSP / nosniff / referrer (only content-type,
		// X-Powered-By, weak ETag, Content-Length) — pinned :3000 (2026-09-24).
		res.BareWrite(400, []byte("{}"))
		return
	}
	res.W.WriteHeader(400) // neither accepted → empty body
}

// crjAcceptsType — true when media type `typ` (e.g. "text/html") is
// acceptable per the Accept header (an empty header accepts all). Matches
// express negotiator: exact / subtype-wildcard (text/*) / full-wildcard
// (*/*); a q=0 entry never matches.
func crjAcceptsType(accept, typ string) bool {
	if accept == "" {
		return true
	}
	typL := strings.ToLower(typ)
	found := false
	for _, part := range strings.Split(accept, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		mt := part
		q := 1.0
		if semi := strings.Index(part, ";"); semi >= 0 {
			mt = strings.TrimSpace(part[:semi])
			for _, p := range strings.Split(part[semi+1:], ";") {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(p, "q=") {
					if f, err := strconv.ParseFloat(p[2:], 64); err == nil {
						q = f
					}
				}
			}
		}
		mt = strings.ToLower(mt)
		switch {
		case mt == "*/*":
		case strings.HasSuffix(mt, "/*"):
			if !strings.HasPrefix(typL, mt[:len(mt)-1]) {
				continue
			}
		default:
			if mt != typL {
				continue
			}
		}
		if q > 0 {
			found = true
		}
	}
	return found
}

// ---------- message helpers ----------

func crjExpected(exp, got, path string) string {
	return `Invalid input: expected ` + exp + `, received ` + got + ` at \"` + path + `\"`
}
func crjOIDErr(path string) string { return `Invalid Mongo ObjectId at \"` + path + `\"` }
func crjTooSmall(min, path string) string {
	return `Too small: expected number to be >=` + min + ` at \"` + path + `\"`
}
func crjIntErr(path string) string {
	return `Invalid input: expected int, received number at \"` + path + `\"`
}
func crjLastKey(path string) string {
	i := strings.LastIndex(path, ".")
	if i < 0 {
		return path
	}
	return path[i+1:]
}

// ---------- top-level body (schema declaration order) ----------

func crjValidate(b apOOBj) (string, bool) {
	var iss []string
	iss = append(iss, crjValRCA(b)...)
	iss = append(iss, crjValUserID(b)...)
	iss = append(iss, crjValPreviews(b)...)
	if u, ok := apUnknown(b, "body", "rejectedChangeAuthorIds", "userId", "previews"); !ok {
		iss = append(iss, u)
	}
	j := strings.Join(iss, "; ")
	return j, j == ""
}

// rejectedChangeAuthorIds: z.array(zz.objectId()) — required.
func crjValRCA(b apOOBj) []string {
	v, has := b.M["rejectedChangeAuthorIds"]
	if !has {
		return []string{crjExpected("array", "undefined", "body.rejectedChangeAuthorIds")}
	}
	arr, ok := v.([]any)
	if !ok {
		return []string{crjExpected("array", apType(v), "body.rejectedChangeAuthorIds")}
	}
	var iss []string
	for i, e := range arr {
		if s, okk := e.(string); !okk || !delHex24(s) {
			iss = append(iss, crjOIDErr("body.rejectedChangeAuthorIds["+itoa(int64(i))+"]"))
		}
	}
	return iss
}

// userId: zz.objectId().nullish() — optional; undefined or null are both ok.
func crjValUserID(b apOOBj) []string {
	v, has := b.M["userId"]
	if !has || v == nil {
		return nil
	}
	if s, ok := v.(string); ok {
		if !delHex24(s) {
			return []string{crjOIDErr("body.userId")}
		}
		return nil
	}
	return []string{crjExpected("string", apType(v), "body.userId")}
}

// previews: z.array(changePreview).optional() — optional.
func crjValPreviews(b apOOBj) []string {
	v, has := b.M["previews"]
	if !has {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return []string{crjExpected("array", apType(v), "body.previews")}
	}
	var iss []string
	for i, e := range arr {
		p := "body.previews[" + itoa(int64(i)) + "]"
		o, ok := apObject(e)
		if !ok {
			iss = append(iss, crjExpected("object", apType(e), p))
			continue
		}
		iss = append(iss, crjValPreview(o, p)...)
	}
	return iss
}

// changePreview (schema declaration order), then unknown keys.
func crjValPreview(o apOOBj, p string) []string {
	var iss []string
	iss = append(iss, crjValStringArr(o, "sectionPath", p+".sectionPath")...)
	iss = append(iss, crjValNumIntMin(o, "startLine", p+".startLine", 1)...)
	iss = append(iss, crjValChanges(o, "changes", p+".changes")...)
	iss = append(iss, crjValString(o, "slice", p+".slice")...)
	iss = append(iss, crjValNumIntMin(o, "sliceStart", p+".sliceStart", 0)...)
	iss = append(iss, crjValOIDArr(o, "userIds", p+".userIds")...)
	if u, ok := apUnknown(o, p, "sectionPath", "startLine", "changes", "slice", "sliceStart", "userIds"); !ok {
		iss = append(iss, u)
	}
	return iss
}

// ---------- field validators ----------

// z.string() — required.
func crjValString(o apOOBj, key, path string) []string {
	v, has := o.M[key]
	if !has {
		return []string{crjExpected("string", "undefined", path)}
	}
	if _, ok := v.(string); ok {
		return nil
	}
	return []string{crjExpected("string", apType(v), path)}
}

// z.array(z.string()) — required.
func crjValStringArr(o apOOBj, key, path string) []string {
	v, has := o.M[key]
	if !has {
		return []string{crjExpected("array", "undefined", path)}
	}
	arr, ok := v.([]any)
	if !ok {
		return []string{crjExpected("array", apType(v), path)}
	}
	var iss []string
	for i, e := range arr {
		if _, okk := e.(string); !okk {
			iss = append(iss, crjExpected("string", apType(e), path+"["+itoa(int64(i))+"]"))
		}
	}
	return iss
}

// z.array(zz.objectId()) — required.
func crjValOIDArr(o apOOBj, key, path string) []string {
	v, has := o.M[key]
	if !has {
		return []string{crjExpected("array", "undefined", path)}
	}
	arr, ok := v.([]any)
	if !ok {
		return []string{crjExpected("array", apType(v), path)}
	}
	var iss []string
	for i, e := range arr {
		if s, okk := e.(string); !okk || !delHex24(s) {
			iss = append(iss, crjOIDErr(path+"["+itoa(int64(i))+"]"))
		}
	}
	return iss
}

// z.number().int().min(min) — required.
func crjValNumIntMin(o apOOBj, key, path string, min int) []string {
	v, has := o.M[key]
	if !has {
		return []string{crjExpected("number", "undefined", path)}
	}
	f, ok := v.(float64)
	if !ok {
		return []string{crjExpected("number", apType(v), path)}
	}
	if f != math.Trunc(f) {
		return []string{crjIntErr(path)} // int check short-circuits min
	}
	if f < float64(min) {
		return []string{crjTooSmall(strconv.Itoa(min), path)}
	}
	return nil
}

// changes: z.array(z.strictObject({ i?: string, d?: string, p: z.number().int().min(0) }))
func crjValChanges(o apOOBj, key, path string) []string {
	v, has := o.M[key]
	if !has {
		return []string{crjExpected("array", "undefined", path)}
	}
	arr, ok := v.([]any)
	if !ok {
		return []string{crjExpected("array", apType(v), path)}
	}
	var iss []string
	for i, e := range arr {
		ep := path + "[" + itoa(int64(i)) + "]"
		el, ok := apObject(e)
		if !ok {
			iss = append(iss, crjExpected("object", apType(e), ep))
			continue
		}
		iss = append(iss, crjValNumIntMin(el, "p", ep+".p", 0)...)
		iss = append(iss, crjValOptString(el, "i", ep+".i")...)
		iss = append(iss, crjValOptString(el, "d", ep+".d")...)
		if u, ok := apUnknown(el, ep, "i", "d", "p"); !ok {
			iss = append(iss, u)
		}
	}
	return iss
}

// optional z.string() (i/d in changes elements).
func crjValOptString(o apOOBj, key, path string) []string {
	v, has := o.M[key]
	if !has {
		return nil
	}
	if _, ok := v.(string); ok {
		return nil
	}
	return []string{crjExpected("string", apType(v), path)}
}
