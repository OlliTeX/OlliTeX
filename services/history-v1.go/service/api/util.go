package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"history-v1/internal/core"
	"history-v1/service/blobstore"
	"history-v1/service/chunkstore"
	"history-v1/service/persist"
)

// --- Error classification (Node err instanceof X catch blocks) ---

// isChunkNotFound covers the Node Chunk.NotFoundError family. In the Go port
// the family is four distinct types (the Node code catches them all
// because they share a base class via instanceof).
func isChunkNotFound(err error) bool {
	var e interface{ IsNotFound() }
	_ = e
	var nf *core.ChunkNotFoundError
	var vn *core.ChunkVersionNotFoundError
	var bt *core.ChunkBeforeTimestampNotFoundError
	var np *core.ChunkNotPersistedError
	return errors.As(err, &nf) || errors.As(err, &vn) || errors.As(err, &bt) || errors.As(err, &np)
}

// isNotPersisted covers *core.ChunkNotPersistedError (Node Chunk.NotPersistedError,
// also extends NotFoundError).
func isNotPersisted(err error) bool {
	var np *core.ChunkNotPersistedError
	return errors.As(err, &np)
}

// VersionOutOfBounds — Node Chunk.VersionNotFoundError. The Go port maps
// the loadAtVersion "not found" error to *core.ChunkVersionNotFoundError, and
// getChangesSinceVersion (preferNewer) wraps it as *chunkstore.VersionOutOfBoundsError.
// For getChanges the controller catches the specific version error and renders 400.
func isVersionOutOfBounds(err error) bool {
	var ve *chunkstore.VersionOutOfBoundsError
	var vn *core.ChunkVersionNotFoundError
	return errors.As(err, &ve) || errors.As(err, &vn)
}

// isUnprocessable — the 422 error types (Node UnprocessableError, NotEditable,
// PathnameError, EditMissingFileError, InvalidChangeError, ChunkVersionConflictError).
func isUnprocessable(err error) bool {
	var ue *core.UnprocessableError
	var ne *core.NotEditableError
	var pe *core.PathnameError
	var ed *core.EditMissingFileError
	var ic *persist.InvalidChangeError
	var vc *chunkstore.ChunkVersionConflictError
	var ce *persist.ServiceBlobNotFoundError
	var xe *persist.SetContentXorError
	return errors.As(err, &ue) || errors.As(err, &ne) || errors.As(err, &pe) ||
		errors.As(err, &ed) || errors.As(err, &ic) || errors.As(err, &vc) ||
		errors.As(err, &ce) || errors.As(err, &xe)
}

// isConflict — Node AlreadyInitialized (endVersion > 0 on reinitialize).
func isConflict(err error) bool {
	var al *chunkstore.AlreadyInitialized
	return errors.As(err, &al)
}

// isTooLarge — Node ContentTooLargeError.
func isTooLarge(err error) bool {
	var cl *persist.ContentTooLargeError
	return errors.As(err, &cl)
}

// --- Validation (Node createHandleValidationError) ---

// validationErr — Node { error: "<message with field name>", statusCode: <int> }.
// The body shape is { error, statusCode }; status codes are 404 (bad path param)
// or 422 (bad query/body field).
type validationErr struct {
	field       string
	statusCode  int
	isParamsErr bool
}

func (e *validationErr) Error() string { return "validation error" }

// renderValidation — the createHandleValidationError(422) response body.
// Node zod produces a ZodError which is stringified; we mirror the field
// name so the test assertions `body.error.includes('project_id')` pass.
func (v *validationErr) render(w http.ResponseWriter) {
	msg := "Validation failed for "
	if v.isParamsErr {
		msg += "params: [" + v.field + "]"
	} else {
		msg += "query: [" + v.field + "]"
	}
	writeJSON(w, v.statusCode, map[string]any{
		"error":      msg,
		"statusCode": v.statusCode,
	})
}

// --- NotPorted (501) ---

// NotPorted — returned by createZip (requires S3/GCS signed URL, not hermetic).
type NotPorted struct {
	Path string
}

func (n *NotPorted) Error() string { return "Not ported: " + n.Path }

// --- Regexes (Node schema.js zz.projectHistoryId / hexHash) ---

var (
	projectIDRX = regexp.MustCompile(`^([0-9a-f]{24}|[1-9][0-9]{0,9})$`)
	hexHashRX   = regexp.MustCompile(`^[0-9a-f]{40,40}$`)
	numOnlyRX   = regexp.MustCompile(`^[0-9]+$`)
)

// projectIDValid — path/query param project_id validation (Node
// zz.projectHistoryId() = MONGO_OR_POSTGRES_ID). Returns nil when valid.
func projectIDValid(v string) *validationErr {
	if !projectIDRX.MatchString(v) {
		return &validationErr{field: "project_id", statusCode: 404, isParamsErr: true}
	}
	return nil
}

// hashValid — 40-hex blob hash param.
func hashValid(h string) *validationErr {
	if !hexHashRX.MatchString(h) {
		return &validationErr{field: "hash", statusCode: 404, isParamsErr: true}
	}
	return nil
}

// copyFromValid — query param for copyProjectBlob.
func copyFromValid(v string) *validationErr {
	if !projectIDRX.MatchString(v) {
		return &validationErr{field: "copyFrom", statusCode: 422}
	}
	return nil
}

// intParse — returns nil when s is a valid non-negative integer, else a
// version validation error.
func intParse(s string) (int, *validationErr) {
	if !numOnlyRX.MatchString(s) {
		return 0, &validationErr{field: "version", statusCode: 404, isParamsErr: true}
	}
	n, _ := strconv.Atoi(s)
	return n, nil
}

// tsParse — parse ISO 8601 timestamp param (Node zz.datetime() / zod date).
// Returns (time, nil) when valid; (zero, *validationErr) when not.
func tsParse(s string) (time.Time, *validationErr) {
	for _, layout := range []string{
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, &validationErr{field: "timestamp", statusCode: 404, isParamsErr: true}
}

// --- farFutureLimits (Node getPersistLimits) ---

// farFutureLimits — both timestamps set to now()+7d (Node getPersistLimits
// sets min AND max to the same farFuture date) so PersistChanges always
// persists (anyTooOld/tooManyBytes trigger).
func farFutureLimits() persist.Limits {
	farFuture := time.Now().Add(7 * 24 * time.Hour)
	return persist.Limits{
		MaxChanges:         0,
		MinChangeTimestamp: &farFuture,
		MaxChangeTimestamp: &farFuture,
	}
}

// --- JSON marshal helpers ---

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	if b == nil {
		return []byte("null")
	}
	return b
}

var _ = blobstore.BlobNotFoundError{}
var _ = chunkstore.ChunkVersionConflictError{}
var _ = persist.NotPortedError{}
