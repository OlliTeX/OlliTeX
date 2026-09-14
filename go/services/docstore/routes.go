package docstore

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"
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

func fmtF(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

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
