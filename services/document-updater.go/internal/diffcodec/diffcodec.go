// Package diffcodec — 1:1 port of `app/js/DiffCodec.js`.
// Converts a before/after pair of doc lines into ShareJS-style ops
// ({i, p} or {d, p}) via diff-match-patch (100ms budget, then semantic
// cleanup), matching Node's diff-match-patch@1.0.5 outputs (see the otc
// module's dmp_test.go ground-truth harness).
package diffcodec

import (
	"errors"
	"strings"
	"time"

	diffmatchpatch "github.com/sergi/go-diff/diffmatchpatch"
)

// Node parity constants (diff-match-patch op codes).
const (
	ADDED     = 1
	REMOVED   = -1
	UNCHANGED = 0
)

const dmpTimeout = 100 * time.Millisecond // Node: dmp.Diff_Timeout = 0.1

// Op is a ShareJS-style op: { i?: string, d?: string, p: number }.
type Op struct {
	I *string `json:"i,omitempty"`
	D *string `json:"d,omitempty"`
	P int     `json:"p"`
}

// DiffAsShareJsOp diffs `before`/`after` (each a line array) into ops.
//
//	Node: `diffAsShareJsOp(before, after)`.
func DiffAsShareJsOp(before, after []string) ([]Op, error) {
	d := diffmatchpatch.New()
	d.DiffTimeout = dmpTimeout
	diffs := d.DiffMain(strings.Join(before, "\n"), strings.Join(after, "\n"), true)
	diffs = d.DiffCleanupSemantic(diffs)

	ops := make([]Op, 0)
	position := 0
	for _, diff := range diffs {
		switch diff.Type {
		case diffmatchpatch.DiffInsert:
			ops = append(ops, Op{I: &diff.Text, P: position})
			position += len(diff.Text)
		case diffmatchpatch.DiffDelete:
			ops = append(ops, Op{D: &diff.Text, P: position})
		case diffmatchpatch.DiffEqual:
			position += len(diff.Text)
		default:
			return nil, errors.New("Unknown type")
		}
	}
	return ops, nil
}
