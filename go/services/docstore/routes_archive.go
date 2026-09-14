package docstore

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

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
