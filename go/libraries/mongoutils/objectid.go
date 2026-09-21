package mongoutils

import (
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ONE_MONTH_IN_MS mirrors the Node constant (31 days — the Node source uses
// 31, not "one calendar month").
const ONE_MONTH_IN_MS = 1000 * 60 * 60 * 24 * 31

// IEdgeFuture mirrors the Node module-scope `ID_EDGE_FUTURE =
// objectIdFromMs(Date.now() + 1000)` — computed once, at "module load"
// (package init in Go: same start-of-process moment).
var IEdgeFuture = objectIdFromMs(time.Now().UnixMilli() + 1000)

// objectIdFromMs mirrors `ObjectId.createFromTime(ms/1000)`.
func objectIdFromMs(ms int64) primitive.ObjectID {
	return primitive.NewObjectIDFromTimestamp(time.UnixMilli(ms))
}

// getMsFromObjectId mirrors `id.getTimestamp().getTime()`.
func getMsFromObjectId(id primitive.ObjectID) int64 {
	return id.Timestamp().UnixMilli()
}

// ObjectIdFromInput mirrors objectIdFromInput(input): a string containing
// 'T' is a date (Node `new Date(input)` → ISO-ish time; Go: RFC3339),
// anything else is a raw 24-hex ObjectID.
//
// Node error on an unparseable date: `${input} is not a valid date`.
func ObjectIdFromInput(input string) (primitive.ObjectID, error) {
	if indexT(input) >= 0 {
		t, err := parseNodeDate(input)
		if err != nil {
			return primitive.ObjectID{}, fmt.Errorf("%s is not a valid date", input)
		}
		return objectIdFromMs(t.UnixMilli()), nil
	}
	return newObjectIDHex(input)
}

// indexT mirrors JS `input.includes('T')`.
func indexT(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == 'T' {
			return i
		}
	}
	return -1
}

// parseNodeDate parses the date forms Node's `new Date(str)` accepts for the
// batched-update id edges: ISO 8601 / RFC 3339 (with or without millis /
// offset). Unparseable → error (message is rendered by the caller, Node-pinned).
func parseNodeDate(input string) (time.Time, error) {
	layouts := []string{
		"2006-01-02T15:04:05.000Z", // Node ISO with millis
		"2006-01-02T15:04:05Z",     // Node ISO
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05", // date-only time (Node accepts)
		"2006-01-02",          // date only
	}
	var lastErr error
	for _, layout := range layouts {
		t, err := time.Parse(layout, input)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

// newObjectIDHex mirrors `new ObjectId(input)` (24-hex string).
func newObjectIDHex(input string) (primitive.ObjectID, error) {
	id, err := primitive.ObjectIDFromHex(input)
	if err != nil {
		return primitive.ObjectID{}, err
	}
	return id, nil
}

// RenderObjectId mirrors renderObjectId: `${objectId} (<timestamp ISO>)`.
// Node's `${objectId}` is the 24-hex form (JS ObjectId.toString()), so Hex()
// is used — the Go driver's default String() renders ObjectID("hex"), which
// would diverge from the Node-pinned string.
func RenderObjectId(objectID primitive.ObjectID) string {
	return fmt.Sprintf("%s (%s)", objectID.Hex(), objectID.Timestamp().UTC().Format("2006-01-02T15:04:05.000Z"))
}
