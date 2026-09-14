package docstore

import (
	"context"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// ---- simple routes -----------------------------------------------------------------

func (s *Server) hStatus(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	expressSendText(w, http.StatusOK, "docstore is alive")
}

func (s *Server) hHealthCheck(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if err := s.runHealthCheck(ctx); err != nil {
		s.logf("docstore: health check: %v", err)
		expressSendStatus(w, http.StatusInternalServerError)
		return
	}
	expressSendStatus(w, http.StatusOK)
}

func (s *Server) hDeleteDoc(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	// The Node handler is deliberately a 500 stub (no validation, no work).
	expressSendText(w, http.StatusInternalServerError, "DELETE-ing a doc is DEPRECATED. PATCH the doc instead.")
}

// ---- project list routes -------------------------------------------------------------

func (s *Server) hAllDocs(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if !s.checkParams(w, p, false) {
		return
	}
	docs, err := s.allNonDeleted(ctx, p.projectID, docProj{Lines: true, Rev: true})
	if err != nil {
		s.mapError(w, err)
		return
	}
	views := make([]any, 0, len(docs))
	for _, d := range docs {
		views = append(views, s.listDocView(d))
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) hAllDocsRanges(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if !s.checkParams(w, p, false) {
		return
	}
	docs, err := s.allNonDeleted(ctx, p.projectID, docProj{Lines: true, Rev: true, Ranges: true})
	if err != nil {
		s.mapError(w, err)
		return
	}
	views := make([]any, 0, len(docs))
	for _, d := range docs {
		views = append(views, s.listDocView(d))
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) hDocVersions(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if !s.checkParams(w, p, false) {
		return
	}
	// getAllDocVersions: no unarchive (Node comment), projection {_id, version}
	docs, err := s.store.ProjectDocs(ctx, p.projectID, ProjectDocOpts{WantVersion: true})
	if err != nil {
		s.mapError(w, err)
		return
	}
	out := make([]any, 0, len(docs))
	for _, d := range docs {
		v := jobj{{"_id", d.ID}}
		if d.Version != nil {
			v = append(v, jpair{"version", *d.Version})
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) hAllRanges(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if !s.checkParams(w, p, false) {
		return
	}
	docs, err := s.allNonDeleted(ctx, p.projectID, docProj{Ranges: true})
	if err != nil {
		s.mapError(w, err)
		return
	}
	views := make([]any, 0, len(docs))
	for _, d := range docs {
		views = append(views, s.docView(d))
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) hAllDeletedDocs(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if !s.checkParams(w, p, false) {
		return
	}
	docs, err := s.store.DeletedDocs(ctx, p.projectID, s.cfg.MaxDeletedDocs)
	if err != nil {
		s.mapError(w, err)
		return
	}
	out := make([]any, 0, len(docs))
	for _, d := range docs {
		v := jobj{{"_id", d.ID}}
		if d.Name != nil {
			v = append(v, jpair{"name", *d.Name})
		}
		if d.DeletedAt != nil {
			v = append(v, jpair{"deletedAt", jsDate(*d.DeletedAt)})
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- single-doc routes -----------------------------------------------------------------

func (s *Server) hGetDoc(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	includeDeleted := false
	var iss []issue
	validateRouteParams(p, true, &iss)
	q := parseRawQuery(r.URL.RawQuery)
	validateQuery("query", q, []string{"include_deleted"}, func(k string, v bool) { includeDeleted = v }, &iss)
	if len(iss) > 0 {
		writeValidationError(w, iss)
		return
	}
	doc, err := s.fullDoc(ctx, p.projectID, p.docID, docProj{Lines: true, Rev: true, Deleted: true, Version: true, Ranges: true, InS3: true})
	if err != nil {
		s.mapError(w, err)
		return
	}
	if doc.Deleted != nil && *doc.Deleted && !includeDeleted {
		s.logf("docstore: not found (soft-deleted doc)")
		expressSendStatus(w, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, s.docView(doc))
}

func (s *Server) hIsDocDeleted(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if !s.checkParams(w, p, true) {
		return
	}
	doc, err := s.store.FindDoc(ctx, p.projectID, p.docID, docProj{Deleted: true}, false)
	if err != nil {
		s.mapError(w, err)
		return
	}
	// Node isDocDeleted: !doc → NotFoundError
	if doc == nil {
		s.mapError(w, ErrNotFound)
		return
	}
	deleted := false
	if doc.Deleted != nil {
		deleted = *doc.Deleted
	}
	writeJSON(w, http.StatusOK, jobj{{"deleted", deleted}})
}

func (s *Server) hGetRaw(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if !s.checkParams(w, p, true) {
		return
	}
	doc, err := s.fullDoc(ctx, p.projectID, p.docID, docProj{Lines: true, InS3: true})
	if err != nil {
		s.mapError(w, err)
		return
	}
	if doc.Lines == nil {
		s.mapError(w, ErrNoLines) // Node: DocWithoutLinesError → 500
		return
	}
	content := strings.Join(*doc.Lines, "\n")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(content))
}

func (s *Server) hPeek(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if !s.checkParams(w, p, true) {
		return
	}
	doc, err := s.peekDoc(ctx, p.projectID, p.docID, false)
	if err != nil {
		s.mapError(w, err)
		return
	}
	status := "active"
	if doc.InS3 != nil && *doc.InS3 {
		status = "archived"
	}
	w.Header().Set("x-doc-status", status)
	writeJSON(w, http.StatusOK, s.docView(doc))
}

// ---- updateDoc / patchDoc -----------------------------------------------------------------

// ---- updateDoc / patchDoc -----------------------------------------------------------------

func (s *Server) hUpdateDoc(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	// Node order: express.json (limit + parse) → parseReq (params + body
	// issues in one 404/400 response).
	b, ok := s.readJSONBody(w, r, s.cfg.MaxJSONRequestSize)
	if !ok {
		return
	}
	var iss []issue
	validateRouteParams(p, true, &iss)
	if b.state != bodyObject {
		got := "undefined"
		if b.state == bodyArray {
			got = "array"
		}
		iss = append(iss, issue{"body", typeIssue("object", got, "body")})
		writeValidationError(w, iss)
		return
	}
	var lines []string
	var version float64
	var rangesTree map[string]any
	failed := false
	strictObject("body", "body", b.obj, []string{"lines", "version", "ranges"}, func(key string) {
		switch key {
		case "lines":
			l, okL := vArrayStrings(b.obj, "lines", "body.lines", "body", &iss)
			if okL {
				lines = l
			} else {
				failed = true
			}
		case "version":
			n, okV := vNumber(b.obj, "version", "body.version", "body", &iss)
			if okV {
				version = n
			} else {
				failed = true
			}
		case "ranges":
			if !validRangesField(b.obj, "ranges", "body.ranges", "body", &iss, &rangesTree) {
				failed = true
			}
		}
	}, &iss)
	if failed || len(iss) > 0 {
		writeValidationError(w, iss)
		return
	}

	bodyLength := 0
	for _, l := range lines {
		bodyLength += utf16Len(l)
	}
	if int64(bodyLength) > s.cfg.MaxDocLength {
		s.logf("docstore: document body too large (%d)", bodyLength)
		expressSendText(w, http.StatusRequestEntityTooLarge, "document body too large")
		return
	}

	updated, rev, err := s.updateDoc(ctx, p.projectID, p.docID, lines, version, rangesTree)
	if err != nil {
		s.mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jobj{{"modified", updated}, {"rev", rev}})
}

func (s *Server) hPatchDoc(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	b, ok := s.readJSONBody(w, r, 100*1024) // express.json() default limit
	if !ok {
		return
	}
	var iss []issue
	validateRouteParams(p, true, &iss)
	if b.state != bodyObject {
		got := "undefined"
		if b.state == bodyArray {
			got = "array"
		}
		iss = append(iss, issue{"body", typeIssue("object", got, "body")})
		writeValidationError(w, iss)
		return
	}
	var deletedAt time.Time
	var name string
	failed := false
	strictObject("body", "body", b.obj, []string{"deleted", "deletedAt", "name"}, func(key string) {
		switch key {
		case "deleted":
			if _, okD := vLiteralTrue(b.obj, "deleted", "body.deleted", "body", &iss); !okD {
				failed = true
			}
		case "deletedAt":
			t, okD := vCoerceDate(b.obj, "deletedAt", "body.deletedAt", "body", &iss)
			if okD {
				deletedAt = t
			} else {
				failed = true
			}
		case "name":
			n, okN := vRequiredString(b.obj, "name", "body.name", "body", &iss)
			if okN {
				name = n
			} else {
				failed = true
			}
		}
	}, &iss)
	if failed || len(iss) > 0 {
		writeValidationError(w, iss)
		return
	}

	if err := s.patchDoc(ctx, p.projectID, p.docID, deletedAt, name); err != nil {
		s.mapError(w, err)
		return
	}
	expressSendStatus(w, http.StatusNoContent)
}

// ---- shared view builders -----------------------------------------------------------------

// docView: _buildDocView 1:1 — _id then lines/rev/version/ranges/deleted in
// that order, each only when present (!= null).

// ---- shared view builders -----------------------------------------------------------------

// docView: _buildDocView 1:1 — _id then lines/rev/version/ranges/deleted in
// that order, each only when present (!= null).
func (s *Server) docView(d *Doc) jobj {
	v := jobj{{"_id", d.ID}}
	if d.Lines != nil {
		v = append(v, jpair{"lines", *d.Lines})
	}
	if d.Rev != nil {
		v = append(v, jpair{"rev", *d.Rev})
	}
	if d.Version != nil {
		v = append(v, jpair{"version", *d.Version})
	}
	if d.Ranges != nil {
		v = append(v, jpair{"ranges", d.Ranges})
	}
	if d.Deleted != nil {
		v = append(v, jpair{"deleted", *d.Deleted})
	}
	return v
}

// listDocView: the /doc and /doc-with-ranges views — 1:1 with the handlers'
// lines backfill (docView.lines = [] when the doc has none, warning logged).

// listDocView: the /doc and /doc-with-ranges views — 1:1 with the handlers'
// lines backfill (docView.lines = [] when the doc has none, warning logged).
func (s *Server) listDocView(d *Doc) jobj {
	if d.Lines == nil {
		s.logf("docstore: missing doc lines (project %s, doc %s)", d.ProjectID, d.ID)
		empty := []string{}
		d = &Doc{ID: d.ID, ProjectID: d.ProjectID, Lines: &empty,
			Rev: d.Rev, Version: d.Version, Ranges: d.Ranges, Deleted: d.Deleted, InS3: d.InS3}
	}
	return s.docView(d)
}

// ---- business flows (DocManager 1:1) ---------------------------------------------------------

// fullDoc = _getDoc 1:1 (inS3 unarchive + recursion); the projection decides
// which fields decode. NotFound → ErrNotFound.

// ---- business flows (DocManager 1:1) ---------------------------------------------------------

// fullDoc = _getDoc 1:1 (inS3 unarchive + recursion); the projection decides
// which fields decode. NotFound → ErrNotFound.
func (s *Server) fullDoc(ctx context.Context, pid, did string, p docProj) (*Doc, error) {
	if !p.InS3 {
		panic("docstore: _getDoc requires the inS3 projection")
	}
	doc, err := s.store.FindDoc(ctx, pid, did, p, false)
	if err != nil {
		return nil, err
	}
	// Node _getDoc: doc == null → NotFoundError
	if doc == nil {
		return nil, ErrNotFound
	}
	if doc.InS3 != nil && *doc.InS3 {
		if err := s.unarchiveDoc(ctx, pid, did); err != nil {
			return nil, err
		}
		return s.fullDoc(ctx, pid, did, p)
	}
	if p.Ranges {
		fixCommentIDs(doc)
	}
	return doc, nil
}

// allNonDeleted = getAllNonDeletedDocs 1:1 (unarchive loop first when
// enabled).

// allNonDeleted = getAllNonDeletedDocs 1:1 (unarchive loop first when
// enabled).
func (s *Server) allNonDeleted(ctx context.Context, pid string, p docProj) ([]*Doc, error) {
	if err := s.unarchiveAllDocs(ctx, pid); err != nil {
		return nil, err
	}
	docs, err := s.store.ProjectDocs(ctx, pid, ProjectDocOpts{
		WantLines:   p.Lines,
		WantRev:     p.Rev,
		WantRanges:  p.Ranges,
		WantVersion: p.Version,
	})
	if err != nil {
		return nil, err
	}
	if p.Ranges {
		for _, d := range docs {
			fixCommentIDs(d)
		}
	}
	return docs, nil
}

// peekDoc = peekDoc/_peekRawDoc 1:1 (archived read without unarchive, rev
// check, one primary retry on DocModifiedError).

// peekDoc = peekDoc/_peekRawDoc 1:1 (archived read without unarchive, rev
// check, one primary retry on DocModifiedError).
func (s *Server) peekDoc(ctx context.Context, pid, did string, useSecondary bool) (*Doc, error) {
	peekOnce := func(secondary bool) (*Doc, error) {
		doc, err := s.store.FindDoc(ctx, pid, did, docProj{Deleted: true, InS3: true, Lines: true, Ranges: true, Rev: true, Version: true}, secondary)
		if err != nil {
			return nil, err
		}
		// Node _peekRawDoc: doc == null → NotFoundError
		if doc == nil {
			return nil, ErrNotFound
		}
		if doc.InS3 != nil && *doc.InS3 {
			archived, err := s.getArchivedDoc(ctx, pid, did)
			if err != nil {
				return nil, err
			}
			lines := archived.Lines
			doc.Lines = &lines
			if archived.Ranges != nil {
				doc.Ranges = archived.Ranges
			}
			if archived.Rev != nil {
				doc.Rev = archived.Rev
			}
			if err := s.checkRevUnchanged(ctx, did, doc.Rev); err != nil {
				return nil, err
			}
		}
		if doc.Ranges != nil {
			fixCommentIDs(doc)
		}
		return doc, nil
	}
	doc, err := peekOnce(useSecondary)
	if err == ErrDocModified {
		return peekOnce(false)
	}
	return doc, err
}

// checkRevUnchanged = MongoManager.checkRevUnchanged 1:1 (NaN/missing revs →
// DocRevValueError; mismatch → DocModifiedError).

// checkRevUnchanged = MongoManager.checkRevUnchanged 1:1 (NaN/missing revs →
// DocRevValueError; mismatch → DocModifiedError).
func (s *Server) checkRevUnchanged(ctx context.Context, did string, rev *int64) error {
	cur, ok, err := s.store.GetDocRev(ctx, did)
	if err != nil || !ok || rev == nil {
		// Node: missing doc/rev → DocRevValueError; db error → 500 generic
		if err != nil {
			return err
		}
		return ErrDocRevValue
	}
	if *rev != cur {
		return ErrDocModified
	}
	return nil
}

// updateDoc = updateDoc/_tryUpdateDoc 1:1 (two attempts, 100–200 ms sleep on
// DocRevValue).

// updateDoc = updateDoc/_tryUpdateDoc 1:1 (two attempts, 100–200 ms sleep on
// DocRevValue).
func (s *Server) updateDoc(ctx context.Context, pid, did string, lines []string, version float64, ranges map[string]any) (bool, int64, error) {
	const maxAttempts = 2
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		updated, rev, err := s.tryUpdateDoc(ctx, pid, did, lines, version, ranges)
		if err == nil {
			return updated, rev, nil
		}
		if err == ErrDocRevValue && attempt < maxAttempts {
			s.logf("docstore: concurrent updateDoc detected, retrying")
			time.Sleep(100*time.Millisecond + time.Duration(rand.Intn(100))*time.Millisecond)
			continue
		}
		return false, 0, err
	}
	return false, 0, ErrDocRevValue
}

func (s *Server) tryUpdateDoc(ctx context.Context, pid, did string, lines []string, version float64, ranges map[string]any) (bool, int64, error) {
	// Node guard 'no lines, version or ranges provided' is unreachable in
	// enforce mode (the schema guarantees all three fields).
	doc, err := s.fullDoc(ctx, pid, did, docProj{Version: true, Rev: true, Lines: true, Ranges: true, InS3: true})
	if err == ErrNotFound {
		doc = nil
	} else if err != nil {
		return false, 0, err
	}

	rangesConv := jsonRangesToMongo(ranges)

	var updateLines, updateRanges, updateVersion bool
	if doc == nil {
		updateLines, updateVersion, updateRanges = true, true, true
	} else {
		docVersion := 0
		if doc.Version != nil {
			docVersion = int(*doc.Version)
		}
		if docVersion > int(version) {
			s.logf("docstore: rejecting stale update (doc version %d > %v)", docVersion, version)
			return false, 0, ErrVersionDown
		}
		// Node: doc.lines.length metrics read TypeErrors on a line-less doc →
		// 500 generic; equivalent 500 here.
		if doc.Lines == nil {
			return false, 0, ErrNoLines
		}
		updateLines = !jsonEqual(*doc.Lines, lines)
		updateVersion = docVersion != int(version)
		updateRanges = !jsonEqual(orEmptyRanges(doc.Ranges), rangesConv)
	}

	modified := false
	rev := int64(0)
	if doc != nil && doc.Rev != nil {
		rev = *doc.Rev
	}

	if updateLines || updateRanges || updateVersion {
		u := WriteUpdates{}
		if updateLines {
			l := lines
			u.Lines = &l
		}
		if updateRanges {
			rng := any(rangesConv)
			u.Ranges = &rng
		}
		if updateVersion {
			v := int64(version)
			u.Version = &v
		}
		if updateLines || updateRanges {
			rev += 1
		}
		modified = true
		prev := int64(0)
		if doc != nil && doc.Rev != nil {
			prev = *doc.Rev
		}
		if err := s.store.UpsertDoc(ctx, pid, did, prev, u); err != nil {
			return false, 0, err
		}
	}
	return modified, rev, nil
}

func orEmptyRanges(v any) any {
	if v == nil {
		return map[string]any{}
	}
	return v
}

// patchDoc = DocManager.patchDoc 1:1 (findDoc 404 → fire-and-forget archive →
// $set meta).
