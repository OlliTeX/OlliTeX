package orcidpicker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

var orcidHTTP = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse // Node: redirect:'manual' — hop by hand
	},
}

// resolveRef — Node `new URL(location, current)` (RFC 3986; absolute
// locations pass through).
func resolveRef(base *url.URL, loc string) *url.URL {
	p, err := url.Parse(loc)
	if err != nil {
		return base
	}
	if p.IsAbs() {
		return p
	}
	return base.ResolveReference(p)
}

// safeFetch — Node safeFetch: per-hop SSRF check, 10 s abort, manual
// redirect following (≤ MAX_REDIRECTS), size cap (Content-Length and body),
// non-2xx → "Upstream API responded with <status>".
func safeFetch(ctx context.Context, rawURL, accept string) (string, error) {
	current, err := url.Parse(rawURL)
	if err != nil {
		return "", orcidErr{msg: "fetch failed"}
	}
	for hop := 0; hop <= maxRedirects; hop++ {
		if err := checkHostNotPrivate(ctx, current.Hostname()); err != nil {
			return "", err
		}
		hctx, hcancel := orcidTimeoutCtx(ctx)
		req, err := http.NewRequestWithContext(hctx, http.MethodGet, current.String(), nil)
		if err != nil {
			hcancel()
			return "", orcidErr{msg: "fetch failed"}
		}
		req.Header.Set("Accept", accept)
		res, err := orcidHTTP.Do(req)
		if err != nil {
			hcancel()
			// undici: transport failure → outer AggregateError "fetch
			// failed"; timeout abort → "The operation was aborted".
			return "", orcidErr{msg: transportText(hctx, err)}
		}
		status := res.StatusCode
		loc := res.Header.Get("Location")
		if status >= 300 && status < 400 {
			// Node drains/cancels the body on redirects.
			io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
			hcancel()
			res.Body.Close()
			if loc == "" {
				return "", orcidErr{msg: errNoLocation}
			}
			current = resolveRef(current, loc)
			continue
		}
		if status < 200 || status >= 300 {
			io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
			hcancel()
			res.Body.Close()
			return "", orcidErr{msg: "Upstream API responded with " + strconv.Itoa(status)}
		}
		if cl := res.Header.Get("Content-Length"); cl != "" {
			if n, perr := strconv.ParseInt(cl, 10, 64); perr == nil && n > maxBodySize {
				io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
				hcancel()
				res.Body.Close()
				return "", orcidErr{msg: errTooLarge}
			}
		}
		body, rerr := io.ReadAll(io.LimitReader(res.Body, maxBodySize+1))
		hcancel()
		res.Body.Close()
		if rerr != nil {
			return "", orcidErr{msg: "fetch failed"}
		}
		if len(body) > maxBodySize {
			return "", orcidErr{msg: errTooLarge}
		}
		return string(body), nil
	}
	return "", orcidErr{msg: errTooManyRed}
}

func fetchJson(ctx context.Context, rawURL string) (map[string]json.RawMessage, error) {
	text, err := safeFetch(ctx, rawURL, "application/json")
	if err != nil {
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &top); err != nil {
		// Node: JSON.parse throws SyntaxError → 502 {error: message}.
		rest := strings.TrimLeft(text, " \t\r\n")
		if len(rest) > 5 {
			rest = rest[:5]
		}
		tok := firstRuneTok(rest)
		msg := "Unexpected token " + tok + ", " + rest + " is not valid JSON"
		return nil, orcidErr{msg: msg}
	}
	return top, nil
}

func firstRuneTok(s string) string {
	if s == "" {
		return "end of input"
	}
	_, sz := decodeRuneAt(s, 0)
	if sz > 1 {
		sz = 1
	}
	return "'" + s[:sz] + "'"
}

// ---------- JSON access helpers (Node-faithful) ----------

// jsGet — doc[key] with explicit null-vs-missing awareness.
func jsGet(doc map[string]json.RawMessage, key string) (json.RawMessage, bool, bool) {
	// returns (raw, present, isNull)
	raw, present := doc[key]
	if !present {
		return nil, false, false
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return raw, true, true
	}
	return raw, true, false
}

func asObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, false
		}
		return m, true
	}
	return nil, false
}

func asArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		var a []json.RawMessage
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, false
		}
		return a, true
	}
	return nil, false
}

func jsStringOf(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// isJSFalsyRaw — value is JS-falsy (0 / false / ""): Node `x || default`
// then substitutes the default.
func isJSFalsyRaw(raw json.RawMessage) bool {
	t := strings.TrimSpace(string(raw))
	return t == "0" || t == "false" || t == `""`
}

// jsDefaultStr — raw value with a fallback when missing/null/JS-falsy.
// Passes non-falsy values through as raw JSON (byte-identical to the
// upstream wire bytes, which is what Node res.json() re-serializes to for
// scalars).
func jsDefaultStr(doc map[string]json.RawMessage, key, fallback string) string {
	raw, present, isNull := jsGet(doc, key)
	if !present || isNull || isJSFalsyRaw(raw) {
		return `"` + jsString(fallback) + `"`
	}
	s, ok := jsStringOf(raw)
	if ok {
		return `"` + jsString(s) + `"`
	}
	return string(raw) // number / object / array — pass through
}

func jsDefaultArr(doc map[string]json.RawMessage, key string) string {
	raw, present, isNull := jsGet(doc, key)
	if !present || isNull || isJSFalsyRaw(raw) {
		return "[]"
	}
	return string(raw)
}

// jsChain — doc.k1?.k2?....kn read with Node `?.` semantics: any missing,
// null, or non-object intermediate level → ("", false).
func jsChain(doc map[string]json.RawMessage, keys ...string) (string, bool) {
	if len(keys) == 0 {
		return "", false
	}
	obj := doc
	for i, k := range keys {
		raw, present, isNull := jsGet(obj, k)
		if !present || isNull || isJSFalsyRaw(raw) {
			return "", false
		}
		if i == len(keys)-1 {
			s, ok := jsStringOf(raw)
			return s, ok
		}
		next, ok := asObject(raw)
		if !ok {
			return "", false
		}
		obj = next
	}
	return "", false
}

// ---------- searchAuthors ----------

const errSearchReq = "Search query required"
const errNullOrcid = `Cannot read properties of null (reading 'orcid-id')`

func searchAuthors(ctx context.Context, q string) (string, error) {
	// Service-level guard (the router already enforces non-blank, so the
	// pinned 400 path is the one actually served):
	if strings.TrimSpace(q) == "" {
		return "", orcidErr{msg: errSearchReq}
	}
	trimmed := strings.TrimSpace(q)
	parts := strings.Fields(trimmed)
	var solrQ string
	if len(parts) >= 2 {
		given := strings.Join(parts[:len(parts)-1], " ")
		family := parts[len(parts)-1]
		solrQ = "given-names:" + encU(given) + " AND family-name:" + encU(family)
	} else {
		solrQ = trimmed
	}
	rawURL := orcidPubAPI + "/expanded-search/?q=" + encU(solrQ) + "&start=0&rows=20"
	top, err := fetchJson(ctx, rawURL)
	if err != nil {
		return "", err
	}

	items := []string{}
	raw, present, isNull := jsGet(top, "expanded-result")
	if present && !isNull && !isJSFalsyRaw(raw) {
		arr, ok := asArray(raw)
		if !ok {
			// (data['expanded-result'] || []).map on a non-array object:
			return "", orcidErr{msg: errSearchMap}
		}
		for _, item := range arr {
			if strings.TrimSpace(string(item)) == "null" {
				return "", orcidErr{msg: errNullOrcid}
			}
			obj, ok := asObject(item)
			if !ok {
				// primitive entry: every property read → undefined →
				// orcid omitted from JSON, names '', institutions [].
				items = append(items, `{"givenNames":"","familyNames":"","institutionNames":[]}`)
				continue
			}
			var b strings.Builder
			b.WriteString("{")
			first := true
			writeKey := func(key, val string) {
				if !first {
					b.WriteString(",")
				}
				first = false
				b.WriteString(`"` + key + `":` + val)
			}
			if r, p, _ := jsGet(obj, "orcid-id"); p {
				if isJSFalsyRaw(r) {
					writeKey("orcid", "null")
				} else {
					writeKey("orcid", string(r))
				}
			}
			writeKey("givenNames", jsDefaultStr(obj, "given-names", ""))
			writeKey("familyNames", jsDefaultStr(obj, "family-names", ""))
			writeKey("institutionNames", jsDefaultArr(obj, "institution-name"))
			b.WriteString("}")
			items = append(items, b.String())
		}
	}
	return "[" + strings.Join(items, ",") + "]", nil
}
