package docstore

// healthcheck.go — app/js/HealthChecker.js 1:1:
//
//	docId = new ObjectId()
//	projectId = new ObjectId(settings.docstore.healthCheck.project_id) // unset → throw
//	POST /project/:projectId/doc/:docId  {lines, version: 42, ranges: {}}
//	body = GET the same URL
//	finally: db.docs.deleteOne({_id: docId, project_id: projectId})   // ALWAYS
//	if (!_.isEqual(body.lines, lines)) → OError('health check lines not equal')
//
// The Node code does an HTTP round-trip against itself; the Go port drives
// the very same handlers in-process (identical observable outcomes), then
// compares the GET result's lines exactly like _.isEqual.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
)

// errHealthCheck is the in-process failure sentinel; app.js's HealthChecker
// handler turns any thrown error into a 500 (no body).
var errHealthCheck = errors.New("docstore: health check failed")

func (s *Server) runHealthCheck(ctx context.Context) error {
	// Node: new ObjectId(undefined/invalid) throws → 500 generic.
	if !hex24.MatchString(s.cfg.HealthCheckProjectID) {
		return errHealthCheck
	}
	pid := s.cfg.HealthCheckProjectID
	did := random24Hex()

	randoms := make([]byte, 32)
	_, _ = rand.Read(randoms)
	lines := []string{"smoke test - delete me", hex.EncodeToString(randoms)}

	// Node `finally`: the smoke doc is always removed afterwards.
	defer func() {
		_ = s.store.DeleteDoc(context.Background(), pid, did)
	}()

	postBody, _ := json.Marshal(map[string]any{"lines": lines, "version": 42, "ranges": map[string]any{}})
	req := httptest.NewRequest("POST", "/project/"+pid+"/doc/"+did, bytes.NewReader(postBody))
	req.Header.Set("Content-Type", "application/json")
	wPost := httptest.NewRecorder()
	s.hUpdateDoc(ctx, wPost, routeParams{projectID: pid, docID: did}, req)
	if wPost.Code != 200 {
		return errHealthCheck // handler failure → check() rejects → /health_check 500
	}

	reqGet := httptest.NewRequest("GET", "/project/"+pid+"/doc/"+did, nil)
	wGet := httptest.NewRecorder()
	s.hGetDoc(ctx, wGet, routeParams{projectID: pid, docID: did}, reqGet)
	if wGet.Code != 200 {
		return errHealthCheck
	}
	var body struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(wGet.Body.Bytes(), &body); err != nil {
		return errHealthCheck
	}
	if !linesEqual(body.Lines, lines) {
		return errHealthCheck // 'health check lines not equal' → 500
	}
	return nil
}

func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func random24Hex() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b) // uniqueness is all the deleteOne lookup needs
	return hex.EncodeToString(b)
}
