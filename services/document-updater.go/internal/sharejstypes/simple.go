// Port of app/js/sharejs/types/simple.js (54 LOC).
//
// Node source:
//
//	exports.name = 'simple'
//	exports.create = () => ({})
//	exports.apply = (snapshot, op) => {
//	  if (!(op.position >= 0 && op.position <= snapshot.str.length)) throw new Error('Invalid position')
//	  const { str } = snapshot
//	  const str' = str.slice(0, op.position) + op.text + str.slice(op.position)
//	  return { str: str' }
//	}
//	exports.transform = (op1, op2, sym) => {
//	  let pos = op1.position
//	  if (op2.position < pos || (op2.position === pos && sym === 'left')) pos += op2.text.length
//	  return { position: pos, text: op1.text }
//	}
//
// `simple` is a really simple text OT type which only allows inserts (no
// deletes). Snapshot: `{ str: string }`. Op: `{ position, text }`.
package sharejstypes

// SimpleName is the registered name for this type.
const SimpleName = "simple"

// SimpleSnapshot mirrors {str:string}.
type SimpleSnapshot struct {
	Str string `json:"str"`
}

// SimpleOp mirrors {position:number, text:string}.
type SimpleOp struct {
	Position int    `json:"position"`
	Text     string `json:"text"`
}

// Simple is the vendored 'simple' type.
type Simple struct{}

func (Simple) Name() string { return SimpleName }

func (Simple) Create() any { return SimpleSnapshot{} }

// Apply mirrors the vendored apply: only inserts are allowed.
func (Simple) Apply(snapshot any, op any) (any, error) {
	s, ok := snapshot.(SimpleSnapshot)
	if !ok {
		return nil, InvalidSnapshot("simple snapshot must be SimpleSnapshot")
	}
	o, ok := op.(SimpleOp)
	if !ok {
		return nil, InvalidOp("simple op must be SimpleOp")
	}
	if o.Position < 0 || o.Position > len(s.Str) {
		return nil, InvalidOp("Invalid position")
	}
	out := s.Str[:o.Position] + o.Text + s.Str[o.Position:]
	return SimpleSnapshot{Str: out}, nil
}

// Transform mirrors the vendored transform: shift op1's position when op2 is
// at-or-before it (and 'left' wins the same-position tie).
func (Simple) Transform(op1 any, op2 any, side string) (any, error) {
	o1, ok := op1.(SimpleOp)
	if !ok {
		return nil, InvalidOp("simple op1 must be SimpleOp")
	}
	o2, ok := op2.(SimpleOp)
	if !ok {
		return nil, InvalidOp("simple op2 must be SimpleOp")
	}
	pos := o1.Position
	if o2.Position < pos || (o2.Position == pos && side == "left") {
		pos += len(o2.Text)
	}
	return SimpleOp{Position: pos, Text: o1.Text}, nil
}
