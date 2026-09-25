// Package assert ports `storage/lib/assert.js`: check-types style ID
// validation plus the OError tagging that carries `{ arg }` debug info.
//
// Mirrors:
//
//	projectID  = /^[0-9a-f]{24}|[1-9][0-9]{0,9}$/  (MONGO_OR_POSTGRES_ID_REGEXP)
//	mongoID    = /^[0-9a-f]{24}$/
//	postgresID = /^[1-9][0-9]{0,9}$/
//	blobHash   = /^[0-9a-f]{40,40}$/
//
// The Go-idiomatic form returns *Assertion errors (the node code throws).
// *Assertion carries the node assertion message as Error() and the
// { arg: <value> } map as Info(), mirroring OError.getFullInfo().
package assert

import (
	"regexp"
	"strconv"

	"history-v1/internal/contenthash"
)

// Raw regex sources (mirrors MONGO_ID_REGEXP / POSTGRES_ID_REGEXP sources).
const (
	MONGOIDRegexpString        = `^[0-9a-f]{24}$`
	POSTGRESIDRegexpString     = `^[1-9][0-9]{0,9}$`
	MONGOORPOSTGRESIDRegexpStr = `^([0-9a-f]{24}|[1-9][0-9]{0,9})$`
)

var (
	mongoIDRegexp           = regexp.MustCompile(MONGOIDRegexpString)
	postgresIDRegexp        = regexp.MustCompile(POSTGRESIDRegexpString)
	mongoOrPostgresIDRegexp = regexp.MustCompile(MONGOORPOSTGRESIDRegexpStr)
)

// Assertion is a check-types assertion failure value. Node:
//
//	assert.match(arg, RX, message)  -> throw (TypeError, message="{message}")
//
// and the error is OError-tagged with { arg }. GetFullInfo() == { arg: arg }.
type Assertion struct {
	Message string
	Arg     any
}

func (a *Assertion) Error() string { return a.Message }

// Info returns the { arg } info map, mirroring OError.getFullInfo().
func (a *Assertion) Info() map[string]any { return map[string]any{"arg": a.Arg} }

// ProjectID asserts arg is a project id (24-hex Mongo or Postgres int).
func ProjectID(arg string, message string) *Assertion {
	if !mongoOrPostgresIDRegexp.MatchString(arg) {
		return &Assertion{Message: message, Arg: arg}
	}
	return nil
}

// ChunkID asserts arg is a chunk id (same shape as project id).
func ChunkID(arg string, message string) *Assertion {
	return ProjectID(arg, message)
}

// MongoID asserts arg is a 24-hex Mongo id.
func MongoID(arg string, message string) *Assertion {
	if !mongoIDRegexp.MatchString(arg) {
		return &Assertion{Message: message, Arg: arg}
	}
	return nil
}

// PostgresID asserts arg is a Postgres positive-integer id.
func PostgresID(arg string, message string) *Assertion {
	if !postgresIDRegexp.MatchString(arg) {
		return &Assertion{Message: message, Arg: arg}
	}
	return nil
}

// BlobHash asserts arg is a 40-hex blob hash.
func BlobHash(arg string, message string) *Assertion {
	if !contenthash.HEXHashRX(arg) {
		return &Assertion{Message: message, Arg: arg}
	}
	return nil
}

// IsPostgres reports whether id is a Postgres integer id and returns it.
func IsPostgres(id string) (bool, int64) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return false, 0
	}
	return true, n
}
