package validtools

// compose.go — generic composite Vals added for later consumers (LIB-06
// rangestracker schemas: z.array(comment), insertOp.or(deleteOp); documented
// in HANDOFF §Decisions as framework additions, not new Node API).

// ArrayVal ports z.array(item): validates an array and composes the element
// schema over every element (StringArrayVal is the z.array(z.string())
// special case; ArrayVal accepts any Val, e.g. nested strict objects).
// Element issues carry the numeric path segment (wire: `at "N"`).
type ArrayVal struct {
	Item Val // nil → StringVal{}
}

// Validate implements Val.
func (a ArrayVal) Validate(present bool, v any) (any, []Issue) {
	item := a.Item
	if item == nil {
		item = StringVal{}
	}
	if !present {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected array, received undefined")}
	}
	if isNull(v) {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected array, received null")}
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected array, received "+jsTypeName(v))}
	}
	out := make([]any, len(arr))
	issues := []Issue{}
	for i, e := range arr {
		val, iss := item.Validate(true, e)
		out[i] = val
		for n := range iss {
			issues = append(issues, iss[n].At(IndexSeg(i)))
		}
	}
	return out, issues
}

// UnionVal ports zod's z.union([a, b, ...]) / a.or(b): the first arm that
// validates with ZERO issues wins (its normalised value is returned). If
// every arm fails, a single invalid_union issue is emitted whose groups hold
// each arm's issues (same wire shape as the DTSchema datetime union —
// NewUnionIssue), preserving per-arm detail for the wire renderer.
//
// Note: Node zod evaluates every arm; Go short-circuits at the first clean
// arm. The observable difference is confined to which normalised value a
// multi-arm input would map to — for well-formed inputs (exactly one arm
// clean) the behaviour is identical, and failure inputs get the same
// failure verdict. Documented deviation.
type UnionVal struct {
	Arms []Val
}

// NewUnion builds a union from its arms (z.union([...]) / a.or(b)).
func NewUnion(arms ...Val) *UnionVal {
	return &UnionVal{Arms: arms}
}

// Validate implements Val.
func (u *UnionVal) Validate(present bool, v any) (any, []Issue) {
	if len(u.Arms) == 1 {
		return u.Arms[0].Validate(present, v)
	}
	groups := make([][]Issue, 0, len(u.Arms))
	for _, arm := range u.Arms {
		val, iss := arm.Validate(present, v)
		if len(iss) == 0 {
			return val, nil
		}
		groups = append(groups, iss)
	}
	return nil, []Issue{NewUnionIssue(groups)}
}
