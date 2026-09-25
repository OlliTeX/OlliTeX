// setcontent.go — Go port of services/history-v1/storage/lib/
// build_set_content_change.js.
//
// buildSetContentChange computes a setDoc-like upsert change against the
// current head (the persisted chunk) without committing it. The caller is
// responsible for committing the returned change with BaseVersion as the
// expected end version (rebased first if the head moved in the meantime).
// New content is stored in the blob store as a side effect, so the returned
// change is cheap to serialize and can be committed as-is later.
package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"history-v1/internal/assert"
	"history-v1/internal/core"
	"history-v1/service/blobstore"
)

// BuildSetContentOpts — Node opts of buildSetContentChange(projectId,
// pathname, opts):
//
//	Content      new doc content (exactly one of Content and BlobHash set);
//	BlobHash     hash of an already created blob with the new binary content
//	                (trackChanges ignored for blob: a binary file has no text);
//	Metadata     the file's metadata is always replaced with this value;
//	UserID       the change's v2 author;
//	Timestamp    the change's timestamp (also the tracked-change ts);
//	Origin       origin of the change;
//	TrackChanges record the edit as tracked changes (content path only).
type BuildSetContentOpts struct {
	Content      *string
	BlobHash     *string
	Metadata     json.RawMessage
	UserID       string
	Timestamp    time.Time
	Origin       *core.Origin
	TrackChanges bool
}

// SetContentResult — Node { change, baseVersion }; Change is nil for a noop.
type SetContentResult struct {
	Change      *core.Change
	BaseVersion int
}

// ContentTooLargeError — Node ContentTooLargeError (OError 'content is too
// large'; info: projectId, pathname, contentLength).
type ContentTooLargeError struct {
	ProjectID     string
	Pathname      string
	ContentLength int
}

func (e *ContentTooLargeError) Error() string {
	return fmt.Sprintf("content is too large (projectId=%s, pathname=%s, contentLength=%d)",
		e.ProjectID, e.Pathname, e.ContentLength)
}

// SetContentXorError — Node OError 'buildSetContentChange: exactly one of
// content and blobHash must be given' and
// 'buildSetContentChange: trackChanges requires a userId'.
type SetContentXorError struct{ Msg string }

func (e *SetContentXorError) Error() string { return e.Msg }

// ServiceBlobNotFoundError — Node BlobNotFoundError (OError 'blob not found';
// info: projectId, blobHash).
type ServiceBlobNotFoundError struct {
	ProjectID string
	BlobHash  string
}

func (e *ServiceBlobNotFoundError) Error() string {
	return fmt.Sprintf("blob not found (projectId=%s, blobHash=%s)", e.ProjectID, e.BlobHash)
}

// BuildSetContentChange — Node buildSetContentChange(projectId, pathname,
// opts). Exactly one of opts.Content and opts.BlobHash must be given.
//
// Returns (*SetContentResult{nil, baseVersion}, nil) when the (blob-path)
// file at pathname already has the same content and metadata (Node
// `status === 'noop'`).
func (s *Service) BuildSetContentChange(projectID, pathname string, opts BuildSetContentOpts) (*SetContentResult, error) {
	if a := assert.ProjectID(projectID, "bad projectId"); a != nil {
		return nil, a
	}
	var contentLen int
	if opts.Content != nil {
		contentLen = core.UTF16Length(*opts.Content)
	}
	if contentLen > core.MaxStringLength {
		return nil, &ContentTooLargeError{ProjectID: projectID, Pathname: pathname, ContentLength: contentLen}
	}
	if (opts.Content == nil) == (opts.BlobHash == nil) {
		return nil, &SetContentXorError{
			Msg: "buildSetContentChange: exactly one of content and blobHash must be given",
		}
	}
	if opts.TrackChanges && opts.UserID == "" {
		return nil, &SetContentXorError{
			Msg: "buildSetContentChange: trackChanges requires a userId",
		}
	}

	pbs := s.bs.Project(projectID)

	var binary *core.BinaryRef
	if opts.BlobHash != nil {
		raw, err := pbs.GetHashBlob(*opts.BlobHash)
		if err != nil {
			var bnf *blobstore.BlobNotFoundError
			if errors.As(err, &bnf) {
				return nil, &ServiceBlobNotFoundError{ProjectID: projectID, BlobHash: *opts.BlobHash}
			}
			return nil, err
		}
		binary = &core.BinaryRef{Hash: *opts.BlobHash, ByteLength: len(raw)}
	}

	var tracking *core.TrackingProps
	if opts.TrackChanges {
		tracking = &core.TrackingProps{
			Type:   "insert",
			UserID: opts.UserID,
			TSISO:  opts.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z"),
		}
	}

	chunk, err := s.cs.LoadLatest(projectID)
	if err != nil {
		return nil, err
	}
	snapshot := chunk.GetSnapshot()
	if err := snapshot.ApplyAll(chunk.GetChanges()); err != nil {
		return nil, err
	}
	baseVersion := chunk.GetEndVersion()

	result, err := core.BuildSetContentOperations(core.SetContentArgs{
		File:      snapshot.GetFile(pathname),
		Pathname:  pathname,
		Content:   opts.Content,
		Binary:    binary,
		Metadata:  opts.Metadata,
		Tracking:  tracking,
		BlobStore: pbs,
	})
	if err != nil {
		return nil, err
	}
	if result.Status == "noop" {
		return &SetContentResult{Change: nil, BaseVersion: baseVersion}, nil
	}

	v2Authors := []any{}
	if opts.UserID != "" {
		v2Authors = []any{opts.UserID}
	}
	change := core.NewChange(result.Operations, opts.Timestamp, nil, opts.Origin, v2Authors, "", nil)
	return &SetContentResult{Change: change, BaseVersion: baseVersion}, nil
}
