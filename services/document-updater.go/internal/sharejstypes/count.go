// Port of app/js/sharejs/types/count.js (37 LOC).
//
// Node source:
//
//	exports.name = 'count'
//	exports.create = () => 1
//	exports.apply = (snapshot, op) => {
//	  const [v, inc] = op
//	  if (snapshot !== v) throw new Error(`Op ${v} != snapshot ${snapshot}`)
//	  return snapshot + inc
//	}
//	exports.transform = (op1, op2) => {
//	  if (op1[0] !== op2[0]) throw new Error(`Op1 ${op1[0]} != op2 ${op2[0]}`)
//	  return [op1[0] + op2[1], op1[1]]
//	}
//	exports.compose = (op1, op2) => {
//	  if (op1[0] + op1[1] !== op2[0]) throw new Error(`Op1 ${op1} + 1 != op2 ${op2}`)
//	  return [op1[0], op1[1] + op2[1]]
//	}
//	exports.generateRandomOp = doc => [[doc, 1], doc + 1]
//
// A simple type used for testing other OT code. Each op is
// [expectedSnapshot, increment]. Snapshot: a number.
package sharejstypes

import (
	"fmt"
)

// CountName is the registered name for this type.
const CountName = "count"

// Count is the vendored 'count' type. Snapshots are ints; ops are [v, inc].
type Count struct{}

func (Count) Name() string { return CountName }

func (Count) Create() any { return 1 }

// Apply mirrors the vendored apply.
func (Count) Apply(snapshot any, op any) (any, error) {
	s, ok := snapshot.(int)
	if !ok {
		return nil, InvalidSnapshot("count snapshot must be int")
	}
	pair, ok := op.([]int)
	if !ok || len(pair) != 2 {
		return nil, InvalidOp("count op must be [v, inc]")
	}
	if s != pair[0] {
		return nil, InvalidOpf("Op %d != snapshot %d", pair[0], s)
	}
	return s + pair[1], nil
}

// Transform mirrors the vendored transform.
func (Count) Transform(op1 any, op2 any, _ string) (any, error) {
	p1, ok := op1.([]int)
	if !ok || len(p1) != 2 {
		return nil, InvalidOp("count op1 must be [v, inc]")
	}
	p2, ok := op2.([]int)
	if !ok || len(p2) != 2 {
		return nil, InvalidOp("count op2 must be [v, inc]")
	}
	if p1[0] != p2[0] {
		return nil, InvalidOpf("Op1 %d != op2 %d", p1[0], p2[0])
	}
	return []int{p1[0] + p2[1], p1[1]}, nil
}

// Compose mirrors the vendored compose: op2's expected-start must equal
// op1's resulting value.
func (Count) Compose(op1, op2 any) (any, error) {
	p1, ok := op1.([]int)
	if !ok || len(p1) != 2 {
		return nil, InvalidOp("count op1 must be [v, inc]")
	}
	p2, ok := op2.([]int)
	if !ok || len(p2) != 2 {
		return nil, InvalidOp("count op2 must be [v, inc]")
	}
	if p1[0]+p1[1] != p2[0] {
		return nil, InvalidOpf("Op1 %s + 1 != op2 %s", fmtSplice(p1), fmtSplice(p2))
	}
	return []int{p1[0], p1[1] + p2[1]}, nil
}

// fmtSplice formats [a,b] with the JS array stringification (comma, no
// space).
func fmtSplice(p []int) string {
	return fmt.Sprintf("[%d,%d]", p[0], p[1])
}

// GenerateRandomOp mirrors the vendored generateRandomOp:
//
//	doc -> [op, nextSnapshot] where op = [doc, 1], next = doc + 1.
func (Count) GenerateRandomOp(doc int) (op []int, next int) {
	return []int{doc, 1}, doc + 1
}
