package docstore

import (
	"context"
	"net/http"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

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
