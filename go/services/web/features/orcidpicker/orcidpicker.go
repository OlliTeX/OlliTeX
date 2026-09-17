// Package orcidpicker implements the Node services/web/modules/orcid-picker
// surface (OrcidPickerRouter.mjs + OrcidService.mjs) on the Go web service.
//
// Routes (Node registration order; all requireLogin — anonymous is bounced
// by the core global chain before any handler runs, pinned P1):
//
//	GET /orcid-picker/search?q=        searchAuthors       → 200 {"results":[...]}
//	GET /orcid-picker/works?orcid=     fetchWorks          → 200 {"works":[...]}
//	GET /orcid-picker/fetch-bib?orcid=
//	     &putCode=                     fetchBibtexFromOrcid→ 200 {"bibtex":"..."}
//
// Deterministic parity contract (p67 gate, both stacks):
//
//	400s (validation, before any network):
//	  search  no q / blank q          → {"error":"q query parameter required"}
//	  works   no orcid                → {"error":"orcid query parameter required"}
//	  works   malformed orcid         → {"error":"Invalid ORCID identifier"}
//	  fetch-bib no orcid              → {"error":"orcid query parameter required"}
//	  fetch-bib malformed orcid       → {"error":"Invalid ORCID identifier"}
//	  fetch-bib no/invalid putCode    → {"error":"putCode query parameter required"}
//
//	200 (live pub.orcid.org, deterministic in the e2e sandbox):
//	  search "Alan Turing" (fielded)  → {"results":[]} (no registry match)
//	  the non-empty free-text result set is pinned live (both legs fetch the
//	  same ORCID response within the same gate run)
//
//	502 (fixed Node error text — the upstream status is the only variable):
//	  works/fetch-bib with a never-registered ORCID
//	                              → {"error":"Upstream API responded with 404"}
//
//	anonymous (core chain): GET+json 401 text/plain "Unauthorized";
//	  bare GET 302 → /login; non-GET 403 text/plain "Forbidden".
package orcidpicker

import (
	"context"
	"time"

	"ollitex/go/services/web/core"
)

// orcidErr is a Node service/transport error whose .message becomes the
// 502 body `{"error":<msg>}` (express: err?.message || fallback).
type orcidErr struct{ msg string }

func (e orcidErr) Error() string { return e.msg }

func orcidTimeoutCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	// Node: per-hop AbortController at FETCH_TIMEOUT_MS = 10_000.
	return context.WithTimeout(ctx, 10*time.Second)
}

func fail502(res *core.Res, err error) {
	msg := "OrCID request failed"
	if e, ok := err.(orcidErr); ok {
		msg = e.msg
	} else if err != nil && err.Error() != "" {
		msg = err.Error()
	}
	res.JSON(502, []byte(`{"error":"`+jsString(msg)+`"}`))
}

func hSearch(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		q := cxt.Req.URL.Query().Get("q")
		if !isNonBlank(q) {
			res.JSON(400, []byte(`{"error":"q query parameter required"}`))
			return
		}
		ctx, cancel := orcidTimeoutCtx(cxt.Req.Context())
		defer cancel()
		results, err := searchAuthors(ctx, q)
		if err != nil {
			fail502(res, err)
			return
		}
		res.JSON(200, []byte(`{"results":`+results+`}`))
	}
}

func hWorks(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		orcid := cxt.Req.URL.Query().Get("orcid")
		if orcid == "" {
			res.JSON(400, []byte(`{"error":"orcid query parameter required"}`))
			return
		}
		if !isValidOrcid(orcid) {
			res.JSON(400, []byte(`{"error":"Invalid ORCID identifier"}`))
			return
		}
		ctx, cancel := orcidTimeoutCtx(cxt.Req.Context())
		defer cancel()
		works, err := fetchWorks(ctx, orcid)
		if err != nil {
			fail502(res, err)
			return
		}
		res.JSON(200, []byte(`{"works":`+works+`}`))
	}
}

func hFetchBib(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		orcid := cxt.Req.URL.Query().Get("orcid")
		if orcid == "" {
			res.JSON(400, []byte(`{"error":"orcid query parameter required"}`))
			return
		}
		if !isValidOrcid(orcid) {
			res.JSON(400, []byte(`{"error":"Invalid ORCID identifier"}`))
			return
		}
		putCode := cxt.Req.URL.Query().Get("putCode")
		if putCode == "" || !jsNumberFinite(putCode) {
			res.JSON(400, []byte(`{"error":"putCode query parameter required"}`))
			return
		}
		ctx, cancel := orcidTimeoutCtx(cxt.Req.Context())
		defer cancel()
		bib, err := fetchBibtexFromOrcid(ctx, orcid, putCode)
		if err != nil {
			fail502(res, err)
			return
		}
		res.JSON(200, []byte(`{"bibtex":"`+jsString(bib)+`"}`))
	}
}

// Node: typeof x === 'string' && x.trim() !== ” (router: !q || typeof q !==
// 'string' || !q.trim())
func isNonBlank(q string) bool {
	if q == "" {
		return false
	}
	for _, r := range q {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' && r != '\f' && r != '\v' && r != 0x85 && r != 0xA0 && r != 0x2007 && r != 0x202F && r != 0x2000 && r != 0x2001 && r != 0x2002 && r != 0x2003 && r != 0x2004 && r != 0x2005 && r != 0x2006 && r != 0x2008 && r != 0x2009 && r != 0x200A {
			return true
		}
	}
	return false
}

func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "orcidpicker",
		Routes: []core.Route{
			{Method: "GET", Path: "/orcid-picker/search", Handler: hSearch(a)},
			{Method: "GET", Path: "/orcid-picker/works", Handler: hWorks(a)},
			{Method: "GET", Path: "/orcid-picker/fetch-bib", Handler: hFetchBib(a)},
		},
	}
}
