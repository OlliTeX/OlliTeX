// Package dmp is a faithful port of the closure the Overleaf project-history
// service uses from the diff-match-patch library (overleaf fork 89805f9,
// verified byte-identical to the Node oracle in .yarn/cache):
//
//	dmp.New()
//	dmp.DiffTimeout = 0.1        // UpdateCompressor L27-28
//	diffs := dmp.DiffMain(before, updated)   // L542: dmp.diff_main(...)
//	dmp.CleanupSemantic(&diffs)              // L543: dmp.diff_cleanupSemantic(...)
//
// The ported closure (Oracle: libraries/overleaf-editor-core/deps/diff-match-
// patch/index.js, read verbatim 2026-09-19):
//
//	diff_main          L95-162
//	diff_compute_      L164-234
//	diff_lineMode_     L236-304   (linesToChars/charsToLines: L456-539)
//	diff_bisect_       L306-428
//	diff_bisectSplit_  L430-454
//	diff_linesToChars_ L456-520
//	diff_charsToLines_ L522-539
//	diff_commonPrefix  L541-570
//	diff_commonSuffix  L572-604
//	diff_commonOverlap_ L606-645
//	diff_halfMatch_    L651-726
//	diff_cleanupSemantic L726-969 (includes cleanupSemanticScore_ L866-919,
//	                       cleanupSemanticLossless L914-990, regex consts L855-859)
//	diff_cleanupMerge  L1080-1210
//
// Porting notes:
//   - JS works in UTF-16 code units (length, charAt, indexOf, substring).
//     Go operates on the same unit granularity: a string is a []uint16 of
//     UTF-16 units here. (Go's []rune/codepoint level would NOT be 1:1; e.g.
//     a lone high surrogate must behave as one unit, exactly like JS.)
//   - JS deadline: `if (Diff_Timeout <= 0) opt_deadline = Number.MAX_VALUE`.
//     In Go, deadline 0 means "no deadline" (unlimited); otherwise it is the
//     wall-clock ms timestamp by which the diff must complete.
//   - diff_bisect_ checks `now > deadline` at the top of each outer d-iteration.
//   - diff_halfMatch_ consults the *instance* DiffTimeout (not the deadline),
//     so it still runs even past the deadline when a timeout was set.
//   - Regex constants (verbatim, L855-859):
//       nonAlphaNumericRegex_ = [^a-zA-Z0-9]
//       whitespaceRegex_      = \s  (JS: \t\n\v\f\r \u00A0\u1680\u2000-\u200A
//                                    \u2028\u2029\u202F\u205F\u3000\uFEFF)
//       linebreakRegex_       = [\r\n]
//       blanklineEndRegex_    = /\n\r?\n$/  (window: ≤3 units before the boundary)
//       blanklineStartRegex_  = /^\r?\n\r?\n/ (window: ≤4 units from the boundary)
//   - CleanupSemantic duplicate-equality removal, overlap pass, and the
//     "shift left" of lossless cleanup are ported operation-for-operation
//     (including the in-place splice bookkeeping).

package dmp

import "time"

// Op mirrors const DIFF_DELETE / DIFF_EQUAL / DIFF_INSERT.
type Op int

const (
	Delete Op = -1
	Equal  Op = 0
	Insert Op = 1
)

// Diff mirrors diff_match_patch.Diff (an [op, text] tuple).
type Diff struct {
	Op   Op
	Text string
}

// DMP mirrors a diff_match_patch instance.
type DMP struct {
	// DiffTimeout mirrors Diff_Timeout (seconds). <= 0 means unlimited.
	DiffTimeout float64
}

// ---------- UTF-16 code-unit encoding (JS parity) ----------
// JS lengths and charAt operate on UTF-16 code units (a lone surrogate pair
// inside a JS string is a valid unit). Ported faithfully: both surrogate
// halves are each one uint16 here. str/u16 form a surrogate-safe bijection:
// a high surrogate immediately followed by a low surrogate is encoded as a
// single U+10000-range scalar (byte-identical to standard UTF-8 for valid
// text); every other unit, including a lone surrogate, is encoded as its own
// scalar and survives the round trip 1:1. A plain string([]rune) codec is
// NOT sufficient because Go's text/range decoding collapses lone surrogates
// to U+FFFD, which corrupts the DMP goldens at emoji boundaries.

// str encodes UTF-16 units via CESU-8 rules (paired surrogate → one U+10000
// scalar byte-identical to UTF-8; unpaired unit → one 1/2/3-byte scalar).
func str(u []uint16) string {
	var buf []byte
	enc := func(cp int) {
		switch {
		case cp < 0x80:
			buf = append(buf, byte(cp))
		case cp < 0x800:
			buf = append(buf, byte(0xC0|(cp>>6)), byte(0x80|(cp&0x3F)))
		case cp < 0x10000:
			buf = append(buf, byte(0xE0|(cp>>12)), byte(0x80|((cp>>6)&0x3F)), byte(0x80|(cp&0x3F)))
		default:
			buf = append(buf,
				byte(0xF0|(cp>>18)),
				byte(0x80|((cp>>12)&0x3F)),
				byte(0x80|((cp>>6)&0x3F)),
				byte(0x80|(cp&0x3F)))
		}
	}
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c >= 0xD800 && c <= 0xDBFF && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] <= 0xDFFF {
			enc(0x10000 + int(c-0xD800)<<10 + int(u[i+1]-0xDC00))
			i++
			continue
		}
		enc(int(c))
	}
	return string(buf)
}

// u16 decodes the CESU-8 output of str, byte-by-byte (never via `range s`,
// which would collapse lone surrogates). Only the encodings str() produces
// are accepted: a 4-byte sequence (lead 0xF0) is a surrogate pair and yields
// two units; 3-byte (lead 0xE0) yields one unit (which for D800..DFFF units
// from str preserves a lone surrogate 1:1); 1- and 2-byte are plain scalars.
func u16(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	b := []byte(s)
	for i := 0; i < len(b); {
		if b[i] < 0x80 {
			out = append(out, uint16(b[i]))
			i++
		} else if b[i]&0xE0 == 0xC0 {
			if i+1 >= len(b) {
				return out
			}
			out = append(out, uint16(int(b[i]&0x1F)<<6|int(b[i+1]&0x3F)))
			i += 2
		} else if b[i]&0xF0 == 0xE0 {
			if i+2 >= len(b) {
				return out
			}
			out = append(out, uint16(int(b[i]&0x0F)<<12|int(b[i+1]&0x3F)<<6|int(b[i+2]&0x3F)))
			i += 3
		} else {
			// Lead byte 0xF0+: surrogate pair encoded as one U+10000 scalar.
			if i+3 >= len(b) {
				return out
			}
			cp := int(b[i]&0x07)<<18 | int(b[i+1]&0x3F)<<12 | int(b[i+2]&0x3F)<<6 | int(b[i+3]&0x3F)
			if cp >= 0x10000 {
				cp -= 0x10000
				out = append(out, uint16(0xD800+(cp>>10)), uint16(0xDC00+(cp&0x3FF)))
			}
			i += 4
		}
	}
	return out
}

// New mirrors `new DMP()` (library default Diff_Timeout = 1.0 s;
// UpdateCompressor overrides it to 0.1 s in its constructor — L26-28).
func New() *DMP { return &DMP{DiffTimeout: 1.0} }

func (d *DMP) diffDeadline() int64 {
	if d.DiffTimeout <= 0 {
		return 0 // unlimited
	}
	return time.Now().UnixMilli() + int64(float64(d.DiffTimeout)*1000.0)
}

// ---------- UTF-16 unit helpers ----------

func indexU(hay, needle []uint16, pos int) int {
	if len(needle) == 0 {
		// JS indexOf("") => pos clamped to [0, len].
		if pos < 0 {
			return 0
		}
		if pos > len(hay) {
			return len(hay)
		}
		return pos
	}
	for i := pos; i+len(needle) <= len(hay); i++ {
		ok := true
		for j := 0; j < len(needle); j++ {
			if hay[i+j] != needle[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

func equalU(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func commonPrefixN(t1, t2 []uint16) int {
	// JS quick check: !t1 || !t2 || charAt(0) mismatch => 0.
	if len(t1) == 0 || len(t2) == 0 || t1[0] != t2[0] {
		return 0
	}
	// JS uses binary search here; linear scan is result-identical.
	lim := len(t1)
	if len(t2) < lim {
		lim = len(t2)
	}
	i := 0
	for i < lim && t1[i] == t2[i] {
		i++
	}
	return i
}

func commonSuffixN(t1, t2 []uint16) int {
	if len(t1) == 0 || len(t2) == 0 || t1[len(t1)-1] != t2[len(t2)-1] {
		return 0
	}
	lim := len(t1)
	if len(t2) < lim {
		lim = len(t2)
	}
	i := 0
	for i < lim && t1[len(t1)-1-i] == t2[len(t2)-1-i] {
		i++
	}
	return i
}

func commonOverlapN(t1, t2 []uint16) int {
	// diff_commonOverlap_ L606-645 (verbatim logic).
	if len(t1) == 0 || len(t2) == 0 {
		return 0
	}
	l1, l2 := len(t1), len(t2)
	// Truncate the longer string (keep tail of t1 / head of t2 as needed).
	if l1 > l2 {
		t1 = t1[l1-l2:]
	} else if l1 < l2 {
		t2 = t2[:l1]
	}
	textLen := len(t1)
	if len(t2) < textLen {
		textLen = len(t2)
	}
	if equalU(t1, t2) {
		return textLen
	}
	best := 0
	length := 1
	for {
		pattern := t1[len(t1)-length:]
		found := indexU(t2, pattern, 0)
		if found == -1 {
			return best
		}
		length += found
		if found == 0 || equalU(t1[len(t1)-length:], t2[:length]) {
			best = length
			length++
		}
	}
}

// ---------- diff_main / diff_compute_ ----------

// DiffMain mirrors dmp.diff_main(text1, text2), deriving checklines and
// deadline the same way the JS default arguments do.
func (d *DMP) DiffMain(text1, text2 string) []Diff {
	return d.diffMain(u16(text1), u16(text2))
}

// diffMain mirrors diff_main(text1, text2, opt_checklines, opt_deadline).
// Unit-based; the deadline is already resolved (unlimited = 0 sentinel).
func (d *DMP) diffMain(t1, t2 []uint16) []Diff {
	return d.diffMainArgs(t1, t2, true, d.diffDeadline())
}

func (d *DMP) diffMainArgs(t1, t2 []uint16, checklines bool, deadline int64) []Diff {
	// Check for equality (speedup).
	if len(t1) == len(t2) && equalU(t1, t2) {
		if len(t1) == 0 {
			return nil
		}
		return []Diff{{Equal, str(t1)}}
	}

	// Trim off common prefix (speedup).
	cl := commonPrefixN(t1, t2)
	commonPrefix := str(t1[:cl])
	t1 = t1[cl:]
	t2 = t2[cl:]

	// Trim off common suffix (speedup).
	cl = commonSuffixN(t1, t2)
	commonSuffix := str(t1[len(t1)-cl:])
	t1 = t1[:len(t1)-cl]
	t2 = t2[:len(t2)-cl]

	// Compute the diff on the middle block.
	diffs := d.diffCompute_(t1, t2, checklines, deadline)

	// Restore the prefix and suffix.
	if commonPrefix != "" {
		diffs = append([]Diff{{Equal, commonPrefix}}, diffs...)
	}
	if commonSuffix != "" {
		diffs = append(diffs, Diff{Equal, commonSuffix})
	}
	d.CleanupMerge(&diffs)
	return diffs
}

// diffCompute_ mirrors diff_compute_ L164-234 (verbatim logic).
func (d *DMP) diffCompute_(t1, t2 []uint16, checklines bool, deadline int64) []Diff {
	if len(t1) == 0 {
		// Just add some text (speedup).
		return []Diff{{Insert, str(t2)}}
	}
	if len(t2) == 0 {
		// Just delete some text (speedup).
		return []Diff{{Delete, str(t1)}}
	}

	// Shorter text inside the longer text.
	var longText, shortText []uint16
	if len(t1) > len(t2) {
		longText, shortText = t1, t2
	} else {
		longText, shortText = t2, t1
	}
	i := indexU(longText, shortText, 0)
	if i != -1 {
		diffs := []Diff{
			{Insert, str(longText[:i])},
			{Equal, str(shortText)},
			{Insert, str(longText[i+len(shortText):])},
		}
		if len(t1) > len(t2) {
			// Swap insertions for deletions if diff is reversed.
			diffs[0].Op = Delete
			diffs[2].Op = Delete
		}
		return diffs
	}

	if len(shortText) == 1 {
		// Single character string.
		// After the previous speedup, the character can't be an equality.
		return []Diff{{Delete, str(t1)}, {Insert, str(t2)}}
	}

	// Check to see if the problem can be split in two.
	if hm := d.halfMatch(t1, t2); hm != nil {
		// A half-match was found, sort out the return data.
		text1a, text1b, text2a, text2b, midCommon := hm[0], hm[1], hm[2], hm[3], hm[4]
		// Send both pairs off for separate processing.
		diffsA := d.diffMainArgs2(text1a, text2a, checklines, deadline)
		diffsB := d.diffMainArgs2(text1b, text2b, checklines, deadline)
		out := make([]Diff, 0, len(diffsA)+len(diffsB)+1)
		out = append(out, diffsA...)
		out = append(out, Diff{Equal, midCommon})
		out = append(out, diffsB...)
		return out
	}

	if checklines && len(t1) > 100 && len(t2) > 100 {
		return d.lineMode(t1, t2, deadline)
	}
	return d.bisect(t1, t2, deadline)
}

// diffMainArgs2 mirrors diff_main(t1, t2, checklines, deadline) where the
// deadline is already resolved (JS passes it explicitly from diff_compute_).
func (d *DMP) diffMainArgs2(text1, text2 string, checklines bool, deadline int64) []Diff {
	return d.diffMainArgs(u16(text1), u16(text2), checklines, deadline)
}

// ---------- diff_bisect_ / diff_bisectSplit_ ----------

// bisect mirrors diff_bisect_ L306-428 (Myers middle-snake). Note:
// the front path checks collision via v2 (the reverse path grid);
// the reverse path checks collision via v1.
func (d *DMP) bisect(t1, t2 []uint16, deadline int64) []Diff {
	l1, l2 := len(t1), len(t2)
	maxD := (l1 + l2 + 1) / 2 // Math.ceil
	vOffset := maxD
	vLength := 2 * maxD
	v1 := make([]int, vLength)
	v2 := make([]int, vLength)
	for i := range v1 {
		v1[i] = -1
		v2[i] = -1
	}
	v1[vOffset+1] = 0
	v2[vOffset+1] = 0
	delta := l1 - l2
	// If the total number of characters is odd, the front path collides
	// with the reverse path.
	front := delta%2 != 0
	k1start, k1end, k2start, k2end := 0, 0, 0, 0

	for dd := 0; dd < maxD; dd++ {
		// Bail out if deadline is reached.
		if d.deadlinePass(deadline) {
			break
		}

		// Walk the front path one step.
		for k1 := -dd + k1start; k1 <= dd-k1end; k1 += 2 {
			k1Offset := vOffset + k1
			var x1 int
			if k1 == -dd || (k1 != dd && v1[k1Offset-1] < v1[k1Offset+1]) {
				x1 = v1[k1Offset+1]
			} else {
				x1 = v1[k1Offset-1] + 1
			}
			y1 := x1 - k1
			for x1 < l1 && y1 < l2 && t1[x1] == t2[y1] {
				x1++
				y1++
			}
			v1[k1Offset] = x1
			if x1 > l1 {
				k1end += 2
			} else if y1 > l2 {
				k1start += 2
			} else if front {
				k2Offset := vOffset + delta - k1
				if k2Offset >= 0 && k2Offset < vLength && v2[k2Offset] != -1 {
					// Mirror x2 onto top-left coordinate system.
					x2 := l1 - v2[k2Offset]
					if x1 >= x2 {
						// Overlap detected.
						return d.bisectSplit(t1, t2, x1, y1, deadline)
					}
				}
			}
		}

		// Walk the reverse path one step.
		for k2 := -dd + k2start; k2 <= dd-k2end; k2 += 2 {
			k2Offset := vOffset + k2
			var x2 int
			if k2 == -dd || (k2 != dd && v2[k2Offset-1] < v2[k2Offset+1]) {
				x2 = v2[k2Offset+1]
			} else {
				x2 = v2[k2Offset-1] + 1
			}
			y2 := x2 - k2
			for x2 < l1 && y2 < l2 && t1[l1-x2-1] == t2[l2-y2-1] {
				x2++
				y2++
			}
			v2[k2Offset] = x2
			if x2 > l1 {
				k2end += 2
			} else if y2 > l2 {
				k2start += 2
			} else if !front {
				k1Offset := vOffset + delta - k2
				if k1Offset >= 0 && k1Offset < vLength && v1[k1Offset] != -1 {
					x1 := v1[k1Offset]
					y1 := vOffset + x1 - k1Offset
					// Mirror x2 onto top-left coordinate system.
					x2m := l1 - v2[k2Offset]
					if x1 >= x2m {
						// Overlap detected.
						return d.bisectSplit(t1, t2, x1, y1, deadline)
					}
				}
			}
		}
	}

	// Diff took too long and hit the deadline, or
	// number of diffs equals number of characters, no commonality at all.
	return []Diff{{Delete, str(t1)}, {Insert, str(t2)}}
}

func (d *DMP) deadlinePass(deadline int64) bool {
	if deadline == 0 {
		return false
	}
	return time.Now().UnixMilli() > deadline
}

// bisectSplit mirrors diff_bisectSplit_ (split at the middle snake).
func (d *DMP) bisectSplit(t1, t2 []uint16, x, y int, deadline int64) []Diff {
	// Compute both diffs serially.
	diffs := d.diffMainArgs(u16(str(t1[:x])), u16(str(t2[:y])), false, deadline)
	diffsb := d.diffMainArgs(u16(str(t1[x:])), u16(str(t2[y:])), false, deadline)
	out := make([]Diff, 0, len(diffs)+len(diffsb))
	out = append(out, diffs...)
	out = append(out, diffsb...)
	return out
}

// ---------- diff_linesToChars_ / diff_charsToLines_ ----------

func (d *DMP) linesToChars(t1, t2 []uint16) (c1 []uint16, c2 []uint16, lineArray []string) {
	// '\x00' is a valid character, but various debuggers don't like it.
	// So we'll insert a junk entry to avoid generating a null character.
	lineArray = []string{""}
	lineHash := map[string]int{}
	maxLines := 0

	munge := func(text []uint16) []uint16 {
		var chars []uint16
		lineStart := 0
		lineEnd := -1
		lineArrayLength := len(lineArray)
		for lineEnd < len(text)-1 {
			lineEnd = indexU(text, u16("\n"), lineStart)
			if lineEnd == -1 {
				lineEnd = len(text) - 1
			}
			line := str(text[lineStart : lineEnd+1])
			if _, ok := lineHash[line]; ok {
				chars = append(chars, uint16(lineHash[line]))
			} else {
				if lineArrayLength == maxLines {
					// Bail out: take the remainder as one line.
					line = str(text[lineStart:])
					lineEnd = len(text)
				}
				lineHash[line] = lineArrayLength
				chars = append(chars, uint16(lineArrayLength))
				lineArray = append(lineArray, line)
				lineArrayLength++
			}
			lineStart = lineEnd + 1
		}
		return chars
	}
	// Allocate 2/3rds of the space for text1, the rest for text2.
	maxLines = 40000
	c1 = munge(t1)
	maxLines = 65535
	c2 = munge(t2)
	return c1, c2, lineArray
}

func (d *DMP) charsToLines(diffs []Diff, lineArray []string) {
	for i := range diffs {
		var text []string
		for _, c := range u16(diffs[i].Text) {
			text = append(text, lineArray[c])
		}
		diffs[i].Text = joiner(text)
	}
}

func joiner(parts []string) string {
	out := ""
	for _, p := range parts {
		out += p
	}
	return out
}

// ---------- diff_lineMode_ ----------

func (d *DMP) lineMode(t1, t2 []uint16, deadline int64) []Diff {
	c1, c2, lineArray := d.linesToChars(t1, t2)
	diffs := d.diffMainArgs(c1, c2, false, deadline)
	// Convert the diff back to original text.
	d.charsToLines(diffs, lineArray)
	// Eliminate freak matches (e.g. blank lines).
	d.CleanupSemantic(&diffs)

	// Rediff any replacement blocks, this time character-by-character.
	// Add a dummy entry at the end.
	diffs = append(diffs, Diff{Equal, ""})
	pointer := 0
	countDelete, countInsert := 0, 0
	textDelete, textInsert := "", ""
	for pointer < len(diffs) {
		switch diffs[pointer].Op {
		case Insert:
			countInsert++
			textInsert += diffs[pointer].Text
		case Delete:
			countDelete++
			textDelete += diffs[pointer].Text
		case Equal:
			if countDelete >= 1 && countInsert >= 1 {
				spliceFrom := pointer - countDelete - countInsert
				head := diffs[:spliceFrom]
				body := diffs[spliceFrom+countDelete+countInsert:]
				subDiff := d.diffMainArgs(u16(textDelete), u16(textInsert), false, deadline)
				out := make([]Diff, 0, len(head)+len(subDiff)+len(body))
				out = append(out, head...)
				out = append(out, subDiff...)
				out = append(out, body...)
				diffs = out
				pointer = spliceFrom + len(subDiff)
			}
			countInsert, countDelete = 0, 0
			textInsert, textDelete = "", ""
		}
		pointer++
	}
	diffs = diffs[:len(diffs)-1] // Remove the dummy entry at the end.
	return diffs
}

// ---------- diff_halfMatch_ ----------

// halfMatch mirrors diff_halfMatch_ L651-726 (returns nil or a 5-tuple).
func (d *DMP) halfMatch(t1, t2 []uint16) *[5]string {
	if d.DiffTimeout <= 0 {
		// Don't risk returning a non-optimal diff if we have unlimited time.
		return nil
	}
	var longtext, shorttext []uint16
	if len(t1) > len(t2) {
		longtext, shorttext = t1, t2
	} else {
		longtext, shorttext = t2, t1
	}
	if len(longtext) < 4 || len(shorttext)*2 < len(longtext) {
		return nil
	}

	// diff_halfMatchI_ — seeded search (L651-726 closure).
	halfMatchI := func(long, short []uint16, i int) [5]string {
		seed := long[i : i+len(long)/4]
		j := -1
		bestCommon := ""
		var longA, longB, shortA, shortB string
		for {
			fi := indexU(short, seed, j+1)
			if fi == -1 {
				break
			}
			j = fi
			prefixLength := commonPrefixN(long[i:], short[j:])
			suffixLength := commonSuffixN(long[:i], short[:j])
			if len(bestCommon) < suffixLength+prefixLength {
				bestCommon = str(short[j-suffixLength : j+prefixLength])
				longA = str(long[:i-suffixLength])
				longB = str(long[i+prefixLength:])
				shortA = str(short[:j-suffixLength])
				shortB = str(short[j+prefixLength:])
			}
		}
		if len(bestCommon)*2 >= len(long) {
			return [5]string{longA, longB, shortA, shortB, bestCommon}
		}
		return [5]string{}
	}

	hm1 := halfMatchI(longtext, shorttext, (len(longtext)+3)/4)
	hm2 := halfMatchI(longtext, shorttext, (len(longtext)+1)/2)
	var out [5]string
	switch {
	case len(hm1[4]) == 0 && len(hm2[4]) == 0:
		return nil
	case len(hm1[4]) == 0:
		out = hm2
	case len(hm2[4]) == 0:
		out = hm1
	case len(hm1[4]) > len(hm2[4]):
		out = hm1
	default:
		// Both matched. Select the longest; JS uses `>` so tie → hm2.
		out = hm2
	}
	// Sort out the return data (L720-738):
	//   text1 longer   → text1 gets the long parts hm[0/1]
	//   equal/shorter  → text2 gets the long parts hm[0/1]
	// Returned tuple: [text1_a, text1_b, text2_a, text2_b, mid_common].
	if len(t1) > len(t2) {
		return &out
	}
	return &[5]string{out[2], out[3], out[0], out[1], out[4]}
}

// ---------- cleanupSemantic / cleanupSemanticScore_ / cleanupMerge ----------

// CleanupSemantic mirrors diff_cleanupSemantic (L726-969):
//  1. "Duplicate record" pass — remove equalities that are no bigger than
//     the edits on both sides (split it into delete + insert).
//  2. Normalize + run the lossless cleanup.
//  3. Overlap pass — pull overlaps from delete+insert pairs out as equalities.
func (d *DMP) CleanupSemantic(diffs *[]Diff) {
	ds := *diffs
	changes := false
	equalities := []int{}
	lastEquality := ""
	lastEqValid := false
	lastEqLen := 0
	pointer := 0
	ins1, del1, ins2, del2 := 0, 0, 0, 0
	for pointer < len(ds) {
		if ds[pointer].Op == Equal {
			// Equality found.
			equalities = append(equalities, pointer)
			ins1, del1 = ins2, del2
			ins2, del2 = 0, 0
			lastEquality = ds[pointer].Text
			lastEqValid = true
			lastEqLen = len(u16(lastEquality))
		} else {
			if ds[pointer].Op == Insert {
				ins2 += len(u16(ds[pointer].Text))
			} else {
				del2 += len(u16(ds[pointer].Text))
			}
			// Eliminate an equality that is smaller or equal to the edits
			// on both sides.
			if lastEqValid && lastEqLen <= maxU(ins1, del1) && lastEqLen <= maxU(ins2, del2) {
				eqIdx := equalities[len(equalities)-1]
				ds = spliceInsert(ds, eqIdx, Diff{Delete, lastEquality})
				// Change the second copy to an insert.
				ds[eqIdx+1].Op = Insert
				// Throw away the equality, and the previous one (re-evaluated).
				// JS: equalities.splice(-2, 2) — removes up to 2 from the tail.
				if len(equalities) <= 2 {
					equalities = equalities[:0]
				} else {
					equalities = equalities[:len(equalities)-2]
				}
				pointer = -1
				if len(equalities) > 0 {
					pointer = equalities[len(equalities)-1]
				}
				ins1, del1, ins2, del2 = 0, 0, 0, 0
				lastEqValid = false
				changes = true
			}
		}
		pointer++
	}

	// Normalize the diff.
	if changes {
		d.CleanupMerge(&ds)
	}
	d.cleanupSemanticLossless(&ds)

	// Find any overlaps between deletions and insertions.
	pointer = 1
	for pointer < len(ds) {
		if ds[pointer-1].Op == Delete && ds[pointer].Op == Insert {
			deletion := u16(ds[pointer-1].Text)
			insertion := u16(ds[pointer].Text)
			delLen, insLen := len(deletion), len(insertion)
			overlap1 := commonOverlapN(deletion, insertion)
			overlap2 := commonOverlapN(insertion, deletion)
			if overlap1 >= overlap2 {
				// Only extract an overlap if it is as big as the edit ahead
				// or behind it (JS: overlap >= length/2, exact JS division).
				if float64(overlap1) >= float64(delLen)/2.0 ||
					float64(overlap1) >= float64(insLen)/2.0 {
					// Overlap found: insert equality, trim surroundings.
					ds = spliceInsert(ds, pointer, Diff{Equal, str(insertion[:overlap1])})
					ds[pointer-1].Text = str(deletion[:delLen-overlap1])
					ds[pointer+1].Text = str(insertion[overlap1:])
					pointer++
				}
			} else {
				if float64(overlap2) >= float64(delLen)/2.0 ||
					float64(overlap2) >= float64(insLen)/2.0 {
					// Reverse overlap found: insert equality and swap/trim.
					ds = spliceInsert(ds, pointer, Diff{Equal, str(deletion[:overlap2])})
					ds[pointer-1].Op = Insert
					ds[pointer-1].Text = str(insertion[:insLen-overlap2])
					ds[pointer+1].Op = Delete
					ds[pointer+1].Text = str(deletion[overlap2:])
					pointer++
				}
			}
			pointer++
		}
		pointer++
	}
	*diffs = ds
}

// cleanupSemanticLossless mirrors diff_cleanupSemanticLossless (L855-998):
// shifts single edits sandwiched by equalities to align to word boundaries.
func (d *DMP) cleanupSemanticLossless(diffs *[]Diff) {
	ds := *diffs
	pointer := 1
	// Intentionally ignore the first and last element.
	for pointer < len(ds)-1 {
		if ds[pointer-1].Op == Equal && ds[pointer+1].Op == Equal {
			// This is a single edit surrounded by equalities.
			equality1 := u16(ds[pointer-1].Text)
			edit := u16(ds[pointer].Text)
			equality2 := u16(ds[pointer+1].Text)
			buffer := append(append([]uint16{}, equality1...), edit...)
			buffer = append(buffer, equality2...)

			// First, shift the edit as far left as possible.
			offsetLeft := commonSuffixN(equality1, edit)
			offsetRight := commonPrefixN(edit, equality2)
			originalEditStart := len(equality1)
			editStart := originalEditStart - offsetLeft
			maxEditStart := originalEditStart + offsetRight
			editEnd := editStart + len(edit)

			// Second, step character by character right, looking for the best fit.
			bestEditStart, bestEditEnd := editStart, editEnd
			bestScore := semanticScore(buffer, editStart) + semanticScore(buffer, editEnd)
			for editStart < maxEditStart {
				editStart++
				editEnd++
				score := semanticScore(buffer, editStart) + semanticScore(buffer, editEnd)
				// The >= encourages trailing rather than leading whitespace.
				if score >= bestScore {
					bestScore = score
					bestEditStart = editStart
					bestEditEnd = editEnd
				}
			}

			if bestEditStart != originalEditStart {
				// We have an improvement, save it back to the diff.
				if bestEditStart > 0 {
					ds[pointer-1].Text = str(buffer[:bestEditStart])
				} else {
					ds = spliceDelete(ds, pointer-1, 1)
					pointer--
				}
				ds[pointer].Text = str(buffer[bestEditStart:bestEditEnd])
				if bestEditEnd < len(buffer) {
					ds[pointer+1].Text = str(buffer[bestEditEnd:])
				} else {
					ds = spliceDelete(ds, pointer+1, 1)
					pointer--
				}
			}
		}
		pointer++
	}
	*diffs = ds
}

// semanticScore mirrors the diff_cleanupSemanticScore_ closure
// (diff_cleanupSemanticScore_, L861-919). Scores range 6 (best) to 0 (worst).
func semanticScore(buf []uint16, index int) int {
	if index == 0 || index == len(buf) {
		// Edges are the best.
		return 6
	}
	char1, char2 := buf[index-1], buf[index]
	na1 := !isAlnum(char1)
	na2 := !isAlnum(char2)
	ws1 := na1 && isJSWhitespace(char1)
	ws2 := na2 && isJSWhitespace(char2)
	lb1 := ws1 && (char1 == 0x0A || char1 == 0x0D)
	lb2 := ws2 && (char2 == 0x0A || char2 == 0x0D)
	blank1 := false
	if lb1 {
		wStart := index - 3 // blanklineEndRegexMaxLength_ = 3
		if wStart < 0 {
			wStart = 0
		}
		blank1 = matchesBlanklineEnd(buf[wStart:index])
	}
	blank2 := false
	if lb2 {
		wEnd := index + 4 // blanklineStartRegexMaxLength_ = 4
		if wEnd > len(buf) {
			wEnd = len(buf)
		}
		blank2 = matchesBlanklineStart(buf[index:wEnd])
	}

	if blank1 || blank2 {
		// Five points for blank lines.
		return 5
	}
	if lb1 || lb2 {
		// Four points for line breaks.
		return 4
	}
	if na1 && !ws1 && ws2 {
		// Three points for end of sentences.
		return 3
	}
	if ws1 || ws2 {
		// Two points for whitespace.
		return 2
	}
	if na1 || na2 {
		// One point for non-alphanumeric.
		return 1
	}
	return 0
}

// matchesBlanklineEnd tests buf against /\r?\n$/ (anchored at the end).
// Window ≤ 3 units; only "\n\n" and "\n\r\n" can match.
func matchesBlanklineEnd(w []uint16) bool {
	return len(w) >= 2 && w[len(w)-1] == 0x0A &&
		(w[len(w)-2] == 0x0A || (len(w) >= 3 && w[len(w)-2] == 0x0D && w[len(w)-3] == 0x0A))
}

// matchesBlanklineStart tests buf against /^\r?\n\r?\n/ (anchored at the start).
// Window ≤ 4 units; can only match "\n\n", "\n\r\n", "\r\n\n", "\r\n\r\n".
func matchesBlanklineStart(w []uint16) bool {
	p := 0
	if len(w) > 0 && w[0] == 0x0D { // leading \r? is optional
		p = 1
	}
	if p >= len(w) || w[p] != 0x0A { // then \n required
		return false
	}
	p++
	if p < len(w) && w[p] == 0x0D { // middle \r? optional
		p++
	}
	// trailing \n required.
	return p < len(w) && w[p] == 0x0A
}

func isAlnum(c uint16) bool {
	// nonAlphaNumericRegex_ = [^a-zA-Z0-9]; inverted.
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// isJSWhitespace mirrors /\s/ as probed against the Node oracle:
// {\t, \n, \v, \f, \r, 0x20, 0xA0, 0x1680, 0x2000-0x200A, 0x2028, 0x2029,
// 0x202F, 0x205F, 0x3000, 0xFEFF}.
// (The library's comment concedes that ports may use platform-native
// definitions; we pin the Node one to stay 1:1.)
func isJSWhitespace(c uint16) bool {
	switch c {
	case 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x20, 0xA0, 0x1680,
		0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007,
		0x2008, 0x2009, 0x200A, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return false
}

func maxU(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func spliceDelete(ds []Diff, at, n int) []Diff {
	out := make([]Diff, 0, len(ds)-n)
	out = append(out, ds[:at]...)
	out = append(out, ds[at+n:]...)
	return out
}

func spliceInsert(ds []Diff, at int, dDiff Diff) []Diff {
	out := make([]Diff, 0, len(ds)+1)
	out = append(out, ds[:at]...)
	out = append(out, dDiff)
	out = append(out, ds[at:]...)
	return out
}

// CleanupMerge mirrors diff_cleanupMerge (L1080-1210):
//  1. Pass 1 — merge delete/insert runs and factor common prefix/suffix,
//     and merge consecutive equalities.
//  2. Pass 2 — shift single edits bounded by equalities sideways (A<ins>BA</ins>C
//     → <ins>AB</ins>AC). If changed, recurse (JS: self-calls when changes).
func (d *DMP) CleanupMerge(diffs *[]Diff) {
	ds := *diffs
	ds = append(ds, Diff{Equal, ""}) // Add a dummy entry at the end.
	pointer := 0
	countDelete, countInsert := 0, 0
	textDelete, textInsert := "", ""

	for pointer < len(ds) {
		switch ds[pointer].Op {
		case Insert:
			countInsert++
			textInsert += ds[pointer].Text
			pointer++
		case Delete:
			countDelete++
			textDelete += ds[pointer].Text
			pointer++
		case Equal:
			// Upon reaching an equality, check for prior redundancies.
			if countDelete+countInsert > 1 {
				if countDelete != 0 && countInsert != 0 {
					// Factor out any common prefixies.
					ins := u16(textInsert)
					del := u16(textDelete)
					commonLength := commonPrefixN(ins, del)
					if commonLength != 0 {
						head := pointer - countDelete - countInsert
						if head > 0 && ds[head-1].Op == Equal {
							ds[head-1].Text += str(ins[:commonLength])
						} else {
							ds = spliceInsert(ds, 0, Diff{Equal, str(ins[:commonLength])})
							pointer++
						}
						ins = ins[commonLength:]
						del = del[commonLength:]
						textInsert, textDelete = str(ins), str(del)
					}
					// Factor out any common suffixies.
					commonSuffix := commonSuffixN(ins, del)
					if commonSuffix != 0 {
						ds[pointer].Text = str(ins[len(ins)-commonSuffix:]) + ds[pointer].Text
						textInsert = textInsert[:len(textInsert)-commonSuffix]
						textDelete = textDelete[:len(textDelete)-commonSuffix]
					}
				}
				// Delete the offending records and add the merged ones.
				pointer -= countDelete + countInsert
				ds = spliceDelete(ds, pointer, countDelete+countInsert)
				if textDelete != "" {
					ds = spliceInsert(ds, pointer, Diff{Delete, textDelete})
					pointer++
				}
				if textInsert != "" {
					ds = spliceInsert(ds, pointer, Diff{Insert, textInsert})
					pointer++
				}
				pointer++
			} else if pointer != 0 && ds[pointer-1].Op == Equal {
				// Merge this equality with the previous one.
				ds[pointer-1].Text += ds[pointer].Text
				ds = spliceDelete(ds, pointer, 1)
			} else {
				pointer++
			}
			countInsert, countDelete = 0, 0
			textDelete, textInsert = "", ""
		}
	}
	if len(ds) > 0 && ds[len(ds)-1].Text == "" {
		ds = ds[:len(ds)-1] // Remove the dummy entry at the end (JS truthiness).
	}

	// Second pass: shift single edits bounded by equalities sideways.
	changes := false
	pointer = 1
	for pointer < len(ds)-1 {
		if ds[pointer-1].Op == Equal && ds[pointer+1].Op == Equal {
			// This is a single edit surrounded by equalities.
			edit := u16(ds[pointer].Text)
			prev := u16(ds[pointer-1].Text)
			next := u16(ds[pointer+1].Text)
			if len(prev) <= len(edit) && equalU(edit[len(edit)-len(prev):], prev) {
				// Shift the edit over the previous equality.
				shifted := make([]uint16, 0, len(prev)+len(edit)-len(prev))
				shifted = append(shifted, prev...)
				shifted = append(shifted, edit[:len(edit)-len(prev)]...)
				ds[pointer].Text = str(shifted)
				newNext := make([]uint16, 0, len(prev)+len(next))
				newNext = append(newNext, prev...)
				newNext = append(newNext, next...)
				ds[pointer+1].Text = str(newNext)
				ds = spliceDelete(ds, pointer-1, 1)
				changes = true
			} else if len(next) <= len(edit) && equalU(edit[:len(next)], next) {
				// Shift the edit over the next equality.
				newPrev := make([]uint16, 0, len(prev)+len(next))
				newPrev = append(newPrev, prev...)
				newPrev = append(newPrev, next...)
				ds[pointer-1].Text = str(newPrev)
				shifted := make([]uint16, 0, len(next)+len(edit)-len(next))
				shifted = append(shifted, edit[len(next):]...)
				shifted = append(shifted, next...)
				ds[pointer].Text = str(shifted)
				ds = spliceDelete(ds, pointer+1, 1)
				changes = true
			}
		}
		pointer++
	}
	// If shifts were made, the diff needs reordering and another shift sweep
	// (JS: `if (changes) { this.diff_cleanupMerge(diffs); }`).
	if changes {
		d.CleanupMerge(&ds)
	}
	*diffs = ds
}

// Str/u16 exported for internal/opmodel (unit-granular string ops must use
// the same CESU-8 codec; see the B2 codec note above).
func Str(u []uint16) string { return str(u) }
func U16(s string) []uint16 { return u16(s) }
