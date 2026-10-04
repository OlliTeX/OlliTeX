// roomkey.go — the 024 Option B room-identity contract (per-(project,doc)
// Yjs rooms).
//
// A collab room's NAME is a single path segment (ygo derives it from the WS
// path base) and is the system-of-record key for room state, version log,
// review records and the browser IndexedDB cache. Under Option B each
// project DOCUMENT has its own room:
//
//	root document (projects.rootDoc_id) → room = "{projectID}"
//	      UNCHANGED from the D19 single-doc contract: the existing live rooms
//	      (ydoc/version-log/review/IndexedDB "ollitex-collab-<pid>") are the
//	      root doc's rooms, so no migration and no history loss.
//	any other document             → room = "{projectID}-{docID}"
//	      fresh per-doc rooms, seeded from their own docstore content.
//
// Both halves are 24-hex (ObjectID) with a single separator, so parsing is
// unambiguous:
//
//	"6abd00e37a29eb9a30056209"        → pid=…, doc=""
//	"6abd00e37a29eb9a30056209-6abd…" → pid=…, doc=…
//
// Every server surface that previously took "room == projectID" (auth role,
// seeding, legacy backfill, the web /collab/room resolver) routes through
// ParseRoom so the contract lives in exactly one place.

package collab

import (
	"regexp"
)

var (
	roomRoot    = regexp.MustCompile(`^[0-9a-f]{24}$`)
	roomDocPair = regexp.MustCompile(`^([0-9a-f]{24})-([0-9a-f]{24})$`)
)

// IsRootRoom — true when the room name is a bare 24-hex project id (the
// D19 root-document room).
func IsRootRoom(room string) bool { return roomRoot.MatchString(room) }

// IsDocRoom — true when the room name is "{projectID}-{docID}".
func IsDocRoom(room string) bool { return roomDocPair.MatchString(room) }

// ValidRoomName — a room name the service accepts (root or doc room).
func ValidRoomName(room string) bool {
	return IsRootRoom(room) || IsDocRoom(room)
}

// RoomProject — the project id a room belongs to ("" when the name is not a
// valid room name).
func RoomProject(room string) string {
	if IsRootRoom(room) {
		return room
	}
	m := roomDocPair.FindStringSubmatch(room)
	if m == nil {
		return ""
	}
	return m[1]
}

// RoomDoc — the document id a room carries ("" for the root-document room).
func RoomDoc(room string) string {
	m := roomDocPair.FindStringSubmatch(room)
	if m == nil {
		return ""
	}
	return m[2]
}

// RoomFor — resolve (project, document, rootDocument) → room name.
// docID == "" or docID == rootDocID → the root room "{projectID}" (the D19
// contract room); any other docID → the per-doc room "{projectID}-{docID}".
func RoomFor(projectID, docID, rootDocID string) string {
	if docID == "" || (rootDocID != "" && docID == rootDocID) {
		return projectID
	}
	return projectID + "-" + docID
}
