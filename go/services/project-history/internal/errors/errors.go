// Package errors mirrors app/js/Errors.js (11 error classes extending OError).
package errors

type baseError struct {
	msg string
}

func (e *baseError) Error() string   { return e.msg }
func (e *baseError) Message() string { return e.msg }

func New(kind, msg string) *baseError { return &baseError{msg: msg} }

// The 11 OError subclasses from Errors.js. Kind is retained for dispatch.
type Kind string

const (
	KindNotFound            = Kind("NotFoundError")
	KindBadRequest          = Kind("BadRequestError")
	KindSync                = Kind("SyncError")
	KindSyncOngoing         = Kind("SyncOngoingError")
	KindOpsOutOfOrder       = Kind("OpsOutOfOrderError")
	KindInconsistentChunk   = Kind("InconsistentChunkError")
	KindUpdateUnknownFormat = Kind("UpdateWithUnknownFormatError")
	KindUnexpectedOpType    = Kind("UnexpectedOpTypeError")
	KindTooManyRequests     = Kind("TooManyRequestsError")
	KindNeedFullResync      = Kind("NeedFullProjectStructureResyncError")
	KindFileContentEmpty    = Kind("FileContentEmptyError")
)

type Error struct {
	Kind Kind
	msg  string
	// Timeout mirrors OError info: {key: ...} → 423 redis lock response.
	Timeout bool
	Key     string
}

func (e *Error) Error() string            { return e.msg }
func NotFound(msg string) *Error          { return &Error{Kind: KindNotFound, msg: msg} }
func BadRequest(msg string) *Error        { return &Error{Kind: KindBadRequest, msg: msg} }
func Sync(msg string) *Error              { return &Error{Kind: KindSync, msg: msg} }
func SyncOngoing(msg string) *Error       { return &Error{Kind: KindSyncOngoing, msg: msg} }
func OpsOutOfOrder(msg string) *Error     { return &Error{Kind: KindOpsOutOfOrder, msg: msg} }
func InconsistentChunk(msg string) *Error { return &Error{Kind: KindInconsistentChunk, msg: msg} }
func UpdateWithUnknownFormat(msg string) *Error {
	return &Error{Kind: KindUpdateUnknownFormat, msg: msg}
}
func UnexpectedOpType(msg string) *Error { return &Error{Kind: KindUnexpectedOpType, msg: msg} }
func TooManyRequests(msg string) *Error  { return &Error{Kind: KindTooManyRequests, msg: msg} }
func NeedFullProjectStructureResync(msg string) *Error {
	return &Error{Kind: KindNeedFullResync, msg: msg}
}
func FileContentEmpty(msg string) *Error { return &Error{Kind: KindFileContentEmpty, msg: msg} }

// LockTimeout mirrors the OError("Timeout", {key}) contract → 423 dispatch
// (server.js L60-66 checks err.message === 'Timeout' + err.info?.key).
func LockTimeout(key string) *Error {
	return &Error{Kind: Kind("Timeout"), msg: "Timeout", Timeout: true, Key: key}
}

// SyncOngoingErrorMessage mirrors Errors.SYNC_ONGOING_ERROR_MESSAGE.
const SyncOngoingErrorMessage = "sync ongoing"
