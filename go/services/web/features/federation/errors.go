// Error handling for the federation package.
package federation

import (
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// ErrNotFound — federation's not-found sentinel. The v1 mongo driver has no
// exported not-found sentinel (that's a v2 thing); FindOne/FindOneContext
// return `driver.ErrNoDocuments`, which is `mongo.ErrNoDocuments`. We re
// // export that so callers compare with errors.Is.
//
// Note (parity gotcha, 09 §3.3 "v1 driver quirk"): v1 `FindOne` returns
// (cur, err) tuples + `ErrNoDocuments` — NOT v2's plain err + `ErrNotFound`.
var ErrNotFound = mongo.ErrNoDocuments

func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// Errorf — federation error formatter (the S2S / HTTP surfaces pass the
// message through verbatim, mirroring Node `throw new Error(msg)`).
func Errorf(format string, args ...any) error {
	return errorf(fmt.Sprintf(format, args...))
}

// errorf — the concrete error type.
type errorf string

func (e errorf) Error() string { return string(e) }
