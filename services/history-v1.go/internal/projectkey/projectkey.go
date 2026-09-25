// Package projectkey ports `@overleaf/object-persistor` ProjectKey.format and
// history-v1's `makeGlobalKey` / `makeProjectKey` key builders.
//
// ProjectKey.format(projectId) (Node):
//
//	function format(projectId) {
//	  const prefix = naiveReverse(pad(projectId))
//	  return path.join(prefix.slice(0, 3), prefix.slice(3, 6), prefix.slice(6))
//	}
//	function pad(number) {
//	  return (number || 0).toString().padStart(9, '0')
//	}
//
// JS semantics: `(number || 0)` is falsy only for `0`, `""`, `null`, `undefined`,
// `NaN`; everything else is `.toString()`-ed and left as-is. For the ids
// history-v1 receives (assert.projectId): Postgres ids are decimal strings
// (no leading zeros); Mongo ids are 24-hex strings and are passed through AS
// A STRING (not parsed). Both are then padded to 9 chars and reversed; the
// reversed string is split into `3 / 3 / rest`.
//
//	format("123")                          -> "321/000/000"
//	format("123456789012345678901234")     -> reverse("123456789012345678901234")
//	format("507f1f77bcf86cd799439011")     -> 24-hex reversed, split 3/3/18
//
// Key builders (blob_store/index.js):
//
//	makeGlobalKey(hash)   = `${hash[0:2]}/${hash[2:4]}/${hash[4:]}`
//	makeProjectKey(pid,h) = `${format(pid)}/${hash[0:2]}/${hash[2:]}`
package projectkey

import "fmt"

const (
	// EmptyID is what format("") produces: falsy -> 0 -> "000000000" reversed.
	EmptyID = "000/000/000"
)

// Format mirrors ProjectKey.format(id) for the string id (id may be a
// Postgres decimal id string or a Mongo 24-hex id string). Returns the
// "a/b/c" path prefix. Does NOT throw; callers pass through assert first.
func Format(projectID string) string {
	s := projectID
	if s == "" {
		s = "0"
	}
	for len(s) < 9 {
		s = "0" + s
	}
	r := reverseStr(s)
	if len(r) < 6 {
		// len(r) >= 9 always after pad; defensive.
		return r + "/"
	}
	return r[0:3] + "/" + r[3:6] + "/" + r[6:]
}

// Pad mirrors ProjectKey.pad(id): decimal-string pad to 9 leading zeros.
//
//	Pad("4")        -> "000000004"
//	Pad("40733513") -> "004073351" (no-op when already >= 9 chars; longer ids
//	                       are left as-is, matching Node padStart semantics).
func Pad(id string) string {
	s := id
	for len(s) < 9 {
		s = "0" + s
	}
	return s
}

// GlobalKey mirrors blob_store's makeGlobalKey(hash): hash split 2/2/rest.
func GlobalKey(hash string) (string, error) {
	if len(hash) < 6 {
		return "", fmt.Errorf("projectkey: hash too short: %q", hash)
	}
	return hash[0:2] + "/" + hash[2:4] + "/" + hash[4:], nil
}

// ProjectKey mirrors blob_store's makeProjectKey(projectId, hash):
// format(projectId) + "/" + hash[0:2] + "/" + hash[2:].
func ProjectKey(projectID, hash string) (string, error) {
	if len(hash) < 40 {
		// hex hash length check; the caller asserts, this is the guard.
		return "", fmt.Errorf("projectkey: hash must be 40-hex, got %q", hash)
	}
	return Format(projectID) + "/" + hash[0:2] + "/" + hash[2:], nil
}

func reverseStr(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}
