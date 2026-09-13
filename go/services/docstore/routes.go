package docstore

// routes.go — the 21 routes, 1:1 with services/docstore (HttpController +
// DocManager + DocArchiveManager + HealthChecker flows).

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func (s *Server) specs() []routeSpec {
	return []routeSpec{
		{"GET", []string{"status"}, s.hStatus},
		{"GET", []string{"health_check"}, s.hHealthCheck},
		{"GET", []string{"project", ":project_id", "doc-deleted"}, s.hAllDeletedDocs},
		{"GET", []string{"project", ":project_id", "doc"}, s.hAllDocs},
		{"GET", []string{"project", ":project_id", "doc-with-ranges"}, s.hAllDocsRanges},
		{"GET", []string{"project", ":project_id", "doc-versions"}, s.hDocVersions},
		{"GET", []string{"project", ":project_id", "ranges"}, s.hAllRanges},
		{"GET", []string{"project", ":project_id", "comment-thread-ids"}, s.hCommentThreadIDs},
		{"GET", []string{"project", ":project_id", "tracked-changes-user-ids"}, s.hTrackedChangeUserIDs},
		{"GET", []string{"project", ":project_id", "has-ranges"}, s.hHasRanges},
		{"GET", []string{"project", ":project_id", "doc", ":doc_id"}, s.hGetDoc},
		{"GET", []string{"project", ":project_id", "doc", ":doc_id", "deleted"}, s.hIsDocDeleted},
		{"GET", []string{"project", ":project_id", "doc", ":doc_id", "raw"}, s.hGetRaw},
		{"GET", []string{"project", ":project_id", "doc", ":doc_id", "peek"}, s.hPeek},
		{"POST", []string{"project", ":project_id", "doc", ":doc_id"}, s.hUpdateDoc},
		{"PATCH", []string{"project", ":project_id", "doc", ":doc_id"}, s.hPatchDoc},
		{"DELETE", []string{"project", ":project_id", "doc", ":doc_id"}, s.hDeleteDoc},
		{"POST", []string{"project", ":project_id", "archive"}, s.hArchiveAll},
		{"POST", []string{"project", ":project_id", "doc", ":doc_id", "archive"}, s.hArchiveDoc},
		{"POST", []string{"project", ":project_id", "unarchive"}, s.hUnarchiveAll},
		{"POST", []string{"project", ":project_id", "destroy"}, s.hDestroy},
	}
}

// ---- validation entry points ----------------------------------------------------

func (s *Server) checkParams(w http.ResponseWriter, p routeParams, wantDoc bool) bool {
	var iss []issue
	validateRouteParams(p, wantDoc, &iss)
	if len(iss) == 0 {
		return true
	}
	writeValidationError(w, iss)
	return false
}

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

func (s *Server) hArchiveAll(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if !s.checkParams(w, p, false) {
		return
	}
	if err := s.archiveAllDocs(ctx, p.projectID); err != nil {
		s.mapError(w, err)
		return
	}
	expressSendStatus(w, http.StatusNoContent)
}

func (s *Server) hArchiveDoc(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if !s.checkParams(w, p, true) {
		return
	}
	if err := s.archiveDoc(ctx, p.projectID, p.docID); err != nil {
		s.mapError(w, err)
		return
	}
	expressSendStatus(w, http.StatusNoContent)
}

func (s *Server) hUnarchiveAll(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if !s.checkParams(w, p, false) {
		return
	}
	if err := s.unarchiveAllDocs(ctx, p.projectID); err != nil {
		if err == ErrDocRevValue {
			s.logf("docstore: failed to unarchive doc")
			expressSendStatus(w, http.StatusConflict)
			return
		}
		s.mapError(w, err)
		return
	}
	expressSendStatus(w, http.StatusOK)
}

func (s *Server) hDestroy(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if !s.checkParams(w, p, false) {
		return
	}
	if err := s.destroyProject(ctx, p.projectID); err != nil {
		s.mapError(w, err)
		return
	}
	expressSendStatus(w, http.StatusNoContent)
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

func (s *Server) hCommentThreadIDs(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if !s.checkParams(w, p, false) {
		return
	}
	docs, err := s.allNonDeleted(ctx, p.projectID, docProj{Ranges: true})
	if err != nil {
		s.mapError(w, err)
		return
	}
	out := jobj{}
	for _, d := range docs {
		ids := []any{}
		seen := map[string]bool{}
		for i := range commentsOf(d) {
			c, isMap := commentsOf(d)[i].(map[string]any)
			if !isMap {
				continue
			}
			op, isOp := c["op"].(map[string]any)
			if !isOp {
				continue
			}
			t := op["t"]
			key := threadKey(t)
			if seen[key] {
				continue
			}
			seen[key] = true
			ids = append(ids, t)
		}
		if len(ids) > 0 {
			out = append(out, jpair{d.ID, ids})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) hTrackedChangeUserIDs(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	if !s.checkParams(w, p, false) {
		return
	}
	docs, err := s.allNonDeleted(ctx, p.projectID, docProj{Ranges: true})
	if err != nil {
		s.mapError(w, err)
		return
	}
	users := []any{}
	seen := map[string]bool{}
	for _, d := range docs {
		rng, isMap := d.Ranges.(map[string]any)
		if !isMap {
			continue
		}
		changes, isArr := rng["changes"].([]any)
		if !isArr {
			continue
		}
		for i := range changes {
			ch, isCh := changes[i].(map[string]any)
			if !isCh {
				continue
			}
			md, isMd := ch["metadata"].(map[string]any)
			if !isMd {
				// Node: TypeError on missing metadata → 500 generic
				s.mapError(w, ErrNoLines)
				return
			}
			uid := md["user_id"]
			if str, isStr := uid.(string); isStr && str == "anonymous-user" {
				continue
			}
			key := threadKey(uid)
			if seen[key] {
				continue
			}
			seen[key] = true
			users = append(users, uid)
		}
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) hHasRanges(ctx context.Context, w http.ResponseWriter, p routeParams, r *http.Request) {
	useSecondary := false
	var iss []issue
	validateRouteParams(p, false, &iss)
	q := parseRawQuery(r.URL.RawQuery)
	validateQuery("query", q, []string{"useSecondary"}, func(k string, v bool) { useSecondary = v }, &iss)
	if len(iss) > 0 {
		writeValidationError(w, iss)
		return
	}
	has, err := s.projectHasRanges(ctx, p.projectID, useSecondary)
	if err != nil {
		s.mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jobj{{"projectHasRanges", has}})
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
func (s *Server) patchDoc(ctx context.Context, pid, did string, deletedAt time.Time, name string) error {
	doc, err := s.store.FindDoc(ctx, pid, did, docProj{Deleted: true}, false)
	if err != nil {
		return err
	}
	if doc == nil {
		return ErrNotFound
	}
	if s.cfg.ArchiveOnSoftDelete {
		go func() {
			defer func() {
				if rec := recover(); rec != nil {
					s.logf("docstore: background archive panic: %v", rec)
				}
			}()
			if err := s.archiveDoc(context.Background(), pid, did); err != nil {
				s.logf("docstore: archiving a single doc in the background failed: %v", err)
			}
		}()
	}
	return s.store.PatchDocMeta(ctx, pid, did, deletedAt, name)
}

// ---- archive flows (DocArchiveManager 1:1) ---------------------------------------------------

// archiveEnabled = _isArchivingEnabled 1:1 (the archive runs against the
// configured object persistor: fs or s3/SeaweedFS backend).
func (s *Server) archiveEnabled() bool {
	return s.cfg.Backend == "fs" || s.cfg.Backend == "s3"
}

func (s *Server) nonArchivedDocIDs(ctx context.Context, pid string) ([]string, error) {
	docs, err := s.store.ProjectDocs(ctx, pid, ProjectDocOpts{NonArchivedOnly: true})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	return ids, nil
}

func (s *Server) archivedDocIDs(ctx context.Context, pid string) ([]string, error) {
	docs, err := s.store.ProjectDocs(ctx, pid, ProjectDocOpts{ArchivedOnly: true, Limit: s.cfg.ArchiveBatchSize})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	return ids, nil
}

func (s *Server) nonDeletedArchivedDocIDs(ctx context.Context, pid string) ([]string, error) {
	// nonDeletedArchivedDocsList: {deleted: {$ne: true}, inS3: true} — the
	// deleted predicate is the ProjectDocs default (IncludeDeleted=false).
	docs, err := s.store.ProjectDocs(ctx, pid, ProjectDocOpts{ArchivedOnly: true, Limit: s.cfg.ArchiveBatchSize})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	return ids, nil
}

func (s *Server) archiveAllDocs(ctx context.Context, pid string) error {
	if !s.archiveEnabled() {
		return nil
	}
	ids, err := s.nonArchivedDocIDs(ctx, pid)
	if err != nil {
		return err
	}
	return parallelMap(ctx, s.cfg.ParallelArchiveJobs, ids, func(id string) error {
		return s.archiveDoc(ctx, pid, id)
	})
}

// archiveDoc 1:1 (lock → serialize {lines,ranges,rev,schema_v:1} → persistor
// with md5 → mark archived).
func (s *Server) archiveDoc(ctx context.Context, pid, did string) error {
	if !s.archiveEnabled() {
		return nil
	}
	doc, err := s.store.GetDocForArchiving(ctx, pid, did, time.Now().Add(s.cfg.ArchivingLockMS))
	if err != nil {
		return err
	}
	if doc == nil {
		return nil // not found / already archived / lock not acquired
	}
	if doc.Lines == nil {
		return ErrNoLines
	}
	fixCommentIDs(doc)
	payload, err := archiveDocJSON(doc)
	if err != nil {
		return err
	}
	if strings.Contains(string(payload), "\x00") {
		s.logf("docstore: null bytes detected in doc %s", did)
		return ErrNullByte
	}
	sourceMD5 := md5hex(payload)
	if err := s.arch.Send(ctx, s.cfg.Bucket, pid+"/"+did, payload, sourceMD5); err != nil {
		return err
	}
	rev := int64(0)
	if doc.Rev != nil {
		rev = *doc.Rev
	}
	return s.store.MarkDocAsArchived(ctx, did, rev)
}

// getArchivedDoc = DocArchive.getDoc 1:1 (fetch + md5 check + deserialize).
func (s *Server) getArchivedDoc(ctx context.Context, pid, did string) (ArchivedDoc, error) {
	data, storedMD5, err := s.arch.Get(ctx, s.cfg.Bucket, pid+"/"+did)
	if err != nil {
		return ArchivedDoc{}, err
	}
	if md5hex(data) != storedMD5 {
		s.logf("docstore: md5 mismatch when downloading doc %s/%s", pid, did)
		return ArchivedDoc{}, ErrMd5Mismatch
	}
	return deserializeArchivedDoc(data)
}

// unarchiveDoc = DocArchive.unarchiveDoc 1:1.
func (s *Server) unarchiveDoc(ctx context.Context, pid, did string) error {
	doc, err := s.store.FindDoc(ctx, pid, did, docProj{InS3: true, Rev: true}, false)
	if err != nil {
		return err
	}
	if doc == nil {
		return ErrNotFound // Node: TypeError on null.inS3 → 500 generic
	}
	if doc.InS3 == nil || !*doc.InS3 {
		return nil // already unarchived
	}
	if !s.archiveEnabled() {
		return ErrArchiveNoCfg
	}
	archived, err := s.getArchivedDoc(ctx, pid, did)
	if err != nil {
		return err
	}
	rev := archived.Rev
	if rev == nil {
		rev = doc.Rev // older archived docs carried no rev
	}
	rng := archived.Ranges
	if rng == nil {
		rng = map[string]any{}
	}
	revVal := int64(0)
	if rev != nil {
		revVal = *rev
	}
	return s.store.RestoreArchivedDoc(ctx, pid, did, archived.Lines, rng, revVal)
}

func (s *Server) unarchiveAllDocs(ctx context.Context, pid string) error {
	if !s.archiveEnabled() {
		return nil
	}
	for {
		var ids []string
		var err error
		if s.cfg.KeepSoftDeletedDocsArchived {
			ids, err = s.nonDeletedArchivedDocIDs(ctx, pid)
		} else {
			ids, err = s.archivedDocIDs(ctx, pid)
		}
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := parallelMap(ctx, s.cfg.ParallelArchiveJobs, ids, func(id string) error {
			return s.unarchiveDoc(ctx, pid, id)
		}); err != nil {
			return err
		}
	}
}

// destroyProject 1:1 (mongo deleteMany + persistor sweep, concurrent like
// Promise.all).
func (s *Server) destroyProject(ctx context.Context, pid string) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	setErr := func(e error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = e
		}
		mu.Unlock()
	}
	wg.Add(1)
	go func() { defer wg.Done(); setErr(s.store.DestroyProjectDocs(ctx, pid)) }()
	if s.archiveEnabled() {
		wg.Add(1)
		go func() { defer wg.Done(); setErr(s.arch.DeleteDirectory(ctx, s.cfg.Bucket, pid)) }()
	}
	wg.Wait()
	return firstErr
}

// projectHasRanges = DocManager.projectHasRanges 1:1.
func (s *Server) projectHasRanges(ctx context.Context, pid string, useSecondary bool) (bool, error) {
	docs, err := s.store.ProjectDocs(ctx, pid, ProjectDocOpts{UseSecondary: useSecondary})
	if err != nil {
		return false, err
	}
	for _, d := range docs {
		doc, err := s.peekDoc(ctx, pid, d.ID, useSecondary)
		if err != nil {
			return false, err
		}
		rng, isMap := doc.Ranges.(map[string]any)
		if !isMap {
			continue
		}
		if comments, ok := rng["comments"].([]any); ok && len(comments) > 0 {
			return true, nil
		}
		if changes, ok := rng["changes"].([]any); ok && len(changes) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// parallelMap = p-map 1:1 (bounded concurrency, first error wins).
func parallelMap(ctx context.Context, concurrency int, ids []string, run func(id string) error) error {
	if concurrency <= 0 {
		concurrency = 1
	}
	jobs := make(chan int)
	errs := make([]error, len(ids))
	var wg sync.WaitGroup
	workers := func() {
		for i := range jobs {
			errs[i] = run(ids[i])
		}
	}
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); workers() }()
	}
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// ---- small helpers --------------------------------------------------------------------------

func commentsOf(d *Doc) []any {
	rng, isMap := d.Ranges.(map[string]any)
	if !isMap {
		return nil
	}
	c, isArr := rng["comments"].([]any)
	if !isArr {
		return nil
	}
	return c
}

// threadKey = JS Set-member identity for ranges ids (ObjectIDs dedupe by
// value, undefined/null is one distinct member, strings/numbers by value).
func threadKey(v any) string {
	switch t := v.(type) {
	case nil:
		return "\\x00nil"
	case bool:
		if t {
			return "b:1"
		}
		return "b:0"
	case float64:
		return "n:" + fmtF(t)
	case string:
		return "s:" + t
	case objectIDHex:
		return "o:" + string(t)
	case primitive.ObjectID:
		return "o:" + t.Hex()
	}
	return "x:" + fmtSprint(v)
}

func fmtF(f float64) string  { return strconv.FormatFloat(f, 'g', -1, 64) }
func fmtSprint(v any) string { return fmt.Sprint(v) }

func utf16Len(s string) int {
	// JS String.length: UTF-16 code units.
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func md5hex(data []byte) string {
	h := md5.Sum(data)
	return hex.EncodeToString(h[:])
}
