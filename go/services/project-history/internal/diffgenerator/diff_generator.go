// Package diffgenerator mirrors app/js/DiffGenerator.js — the update→diff
// algebra used to build version diffs. Pure logic, no I/O.
//
// Lengths are byte-lengths (JS uses UTF-16 .length; identical for BMP text,
// diverges for non-BMP — flagged). op.P is a numeric string (overleaf op
// convention), kept as int here.
package diffgenerator

import "fmt"

// ConsistencyError mirrors DiffGenerator.ConsistencyError (delete content
// mismatch while applying a delete op to a diff).
type ConsistencyError struct{ Msg string }

func (e *ConsistencyError) Error() string { return "consistency error: " + e.Msg }

func throwConsistency(deleted, op string) {
	panic(&ConsistencyError{fmt.Sprintf("deleted content, '%s', does not match delete op, '%s'", deleted, op)})
}

// PartMeta mirrors the per-update meta stamped onto diff parts.
// Origin carries meta.origin.kind ("history-resync" marks resync parts).
type PartMeta struct {
	Users   []any
	StartTs *int
	EndTs   *int
	Origin  string
}

// Part mirrors { u?, i?, d?, p, meta? }. Pointer fields mark absence so ""
// (present-but-empty) stays distinct from nil.
type Part struct {
	U      *string
	I      *string
	D      *string
	P      int
	Broken bool
	Meta   *PartMeta
}

func partContent(p *Part) string {
	if p.U != nil && *p.U != "" {
		return *p.U
	}
	if p.D != nil && *p.D != "" {
		return *p.D
	}
	if p.I != nil && *p.I != "" {
		return *p.I
	}
	return ""
}

func partLength(p *Part) int { return len(partContent(p)) }

func isEmpty(p *Part) bool { return partContent(p) == "" }

func slicePtr(s string) *string { return &s }

// slicePart mirrors _slicePart: take [from,to) of the part's content, keeping
// its kind (u or i) and meta.
func slicePart(base *Part, from, to int) Part {
	content := partContent(base)
	if to > len(content) {
		to = len(content)
	}
	s := content[from:to]
	var part Part
	if base.U != nil {
		u := s
		part.U = &u
	} else if base.I != nil {
		i := s
		part.I = &i
	}
	part.Meta = base.Meta
	return part
}

// consumeToOffset mirrors _consumeToOffset: append each delete part and the
// part crossing `opOffset` (sliced at the offset); the remainder is returned
// as the tail.
func consumeToOffset(parts []Part, opOffset int) (consumed, remaining []Part) {
	position := 0
	for len(parts) > 0 {
		part := parts[0]
		parts = parts[1:]
		length := partLength(&part)
		if part.D != nil {
			consumed = append(consumed, part)
			continue
		}
		if position+length >= opOffset {
			partOffset := opOffset - position
			if partOffset > 0 {
				seg := slicePart(&part, 0, partOffset)
				if !isEmpty(&seg) {
					consumed = append(consumed, seg)
				}
			}
			if partOffset < length {
				seg := slicePart(&part, partOffset, length)
				if !isEmpty(&seg) {
					remaining = append([]Part{seg}, remaining...)
				}
			}
			break
		}
		position += length
		consumed = append(consumed, part)
	}
	remaining = append(remaining, parts...)
	return consumed, remaining
}

func copyStr(s string) *string { v := s; return &v }

// consumeDeletions mirrors _consumeDiffAffectedByDeleteOp + _consumeDeletedPart.
// Consumes delete content `del` from `remaining`, validating the content
// (ConsistencyError on mismatch). A delete of unchanged text becomes a new
// delete part; insert text is silently dropped; existing delete parts pass
// through. The unconsumed tail is returned for the caller.
func consumeDeletions(remaining []Part, del string, meta *PartMeta) (consumed, rest []Part) {
	consumed = []Part{}
	rest = nil
	for len(del) > 0 && len(remaining) > 0 {
		part := remaining[0]
		remaining = remaining[1:]
		plen := partLength(&part)
		if part.D != nil {
			// Existing delete passes through unchanged (JS pushes newPart=part).
			consumed = append(consumed, part)
			continue
		}
		if plen > len(del) {
			deleted := partContent(&part)[:len(del)]
			if deleted != del {
				throwConsistency(deleted, del)
			}
			// Vendor: remainingDiff.unshift(_slicePart(part, op.d.length)) — the
			// partial tail of this part goes back first, then the parts that
			// were untouched beyond it.
			seg := slicePart(&part, len(del), plen)
			if !isEmpty(&seg) {
				rest = append([]Part{seg}, rest...)
			}
			rest = append(rest, remaining...)
			// Vendor: deleting unchanged (u) text becomes a delete part;
			// deleting (i) insert text silently drops the inserted text
			// (newPart = null) — the remaining insert keeps its tail.
			if part.U != nil {
				consumed = append(consumed, Part{D: copyStr(del), Meta: meta})
			}
			return consumed, rest
		}
		if plen == len(del) {
			deleted := partContent(&part)
			if deleted != del {
				throwConsistency(deleted, del)
			}
			if part.U != nil {
				consumed = append(consumed, Part{D: copyStr(del), Meta: meta})
			}
			// Remaining diff parts (after the fully-consumed part) are not
			// touched by the delete op and must be preserved by the caller.
			rest = append(rest, remaining...)
			return consumed, rest
		}
		// plen < len(del): whole part deleted, delete op continues.
		deleted := partContent(&part)
		opContent := del[:plen]
		if deleted != opContent {
			throwConsistency(deleted, opContent)
		}
		if part.U != nil && *part.U != "" {
			consumed = append(consumed, Part{D: copyStr(*part.U), Meta: meta})
		}
		del = del[plen:]
	}
	return consumed, rest
}

// ApplyOpToDiff mirrors applyOpToDiff.
func ApplyOpToDiff(diff []Part, op Part, meta *PartMeta) []Part {
	consumed, remaining := consumeToOffset(diff, op.P)
	newDiff := make([]Part, 0, len(diff)+2)
	newDiff = append(newDiff, consumed...)
	if op.I != nil {
		newDiff = append(newDiff, Part{I: op.I, Meta: meta})
	} else if op.D != nil {
		c, rem := consumeDeletions(remaining, *op.D, meta)
		newDiff = append(newDiff, c...)
		remaining = rem
	}
	newDiff = append(newDiff, remaining...)
	return newDiff
}

// Update mirrors a text update { op: Op[], meta } fed to BuildDiff.
type Update struct {
	Op   []Part
	Meta *PartMeta
}

// ApplyUpdateToDiff mirrors _mocks.applyUpdateToDiff.
func ApplyUpdateToDiff(diff []Part, update Update) []Part {
	for _, op := range update.Op {
		if op.Broken {
			continue
		}
		diff = ApplyOpToDiff(diff, op, update.Meta)
	}
	return diff
}

// BuildDiff mirrors buildDiff: start {u: initial}, fold updates, compress.
func BuildDiff(initialContent string, updates []Update) []Part {
	diff := []Part{{U: slicePtr(initialContent)}}
	for _, update := range updates {
		diff = ApplyUpdateToDiff(diff, update)
	}
	return CompressDiff(diff)
}

func minI(a, b *int) *int {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if *b < *a {
		return b
	}
	return a
}

func maxI(a, b *int) *int {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if *b > *a {
		return b
	}
	return a
}

// xorNonEmpty mirrors lodash _.xor length check (user sets must match to merge).
func xorNonEmpty(a, b []any) bool {
	ca, cb := map[string]int{}, map[string]int{}
	for _, x := range a {
		ca[fmt.Sprint(x)]++
	}
	for _, x := range b {
		cb[fmt.Sprint(x)]++
	}
	for k, n := range ca {
		if n > cb[k] {
			return true
		}
	}
	for k, n := range cb {
		if n > ca[k] {
			return true
		}
	}
	return false
}

// CompressDiff mirrors _mocks.compressDiff.
func CompressDiff(diff []Part) []Part {
	out := make([]Part, 0, len(diff))
	for _, part := range diff {
		users := []any{}
		if part.Meta != nil {
			users = part.Meta.Users
		}
		if part.Meta != nil && part.Meta.Origin == "history-resync" {
			// Resync: keep unchanged parts only (the caller has already
			// skipped resync deletes and converted resync inserts to u).
			if part.U != nil {
				out = append(out, part)
			}
			continue
		}
		if len(out) == 0 {
			out = append(out, part)
			continue
		}
		last := out[len(out)-1]
		lastUsers := []any{}
		if last.Meta != nil {
			lastUsers = last.Meta.Users
		}
		if xorNonEmpty(users, lastUsers) {
			out = append(out, part)
			continue
		}
		if last.I != nil && part.I != nil {
			merged := *last.I + *part.I
			last.I = &merged
			if last.Meta != nil && part.Meta != nil {
				last.Meta.StartTs = minI(last.Meta.StartTs, part.Meta.StartTs)
				last.Meta.EndTs = maxI(last.Meta.EndTs, part.Meta.EndTs)
			}
			out[len(out)-1] = last
			continue
		}
		if last.D != nil && part.D != nil {
			merged := *last.D + *part.D
			last.D = &merged
			if last.Meta != nil && part.Meta != nil {
				last.Meta.StartTs = minI(last.Meta.StartTs, part.Meta.StartTs)
				last.Meta.EndTs = maxI(last.Meta.EndTs, part.Meta.EndTs)
			}
			out[len(out)-1] = last
			continue
		}
		out = append(out, part)
	}
	return out
}
