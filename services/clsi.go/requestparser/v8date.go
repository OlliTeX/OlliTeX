package requestparser

// v8date.go — faithful Go port of V8's `new Date(string)` implementation
// (ES5 grammar path + legacy Safari-compatible grammar path + date
// arithmetic: MakeDay/MakeTime/MakeDate, TryTimeClip, local-zone adjustment).
//
// This gives byte-for-byte parity with the Node service for
// `resource.modified`, which is a client-controlled JS string rather than a
// pure epoch-ms number (e.g. "12:00 01/02/03" is a valid V8 date).
//
// Local-zone resolution uses the machine's IANA zone (Go `time.Local`),
// matching Node when both run on the same host. Explicit-offset (Z /
// +/-hh:mm) strings are host-independent (pure offset arithmetic).

import (
	"time"

	_ "time/tzdata" // embed tz database (containers may lack zoneinfo files)
)

// ---------------------------------------------------------------------------
// ECMA / V8 numeric constants and conversions
// ---------------------------------------------------------------------------

const (
	v8kMsPerDay  = int64(86400000)
	v8kMaxTime   = int64(864000000) * 10000000 // 8.64e15
	v8kBeforeUTC = v8kMaxTime + v8kMsPerDay*30 // kMaxTimeBeforeUTCInMs
	v8TokNone    = 0x7fffffff
)

// v8TimeClip ports DateCache::TryTimeClip (inline DoubleToInteger).
func v8TimeClip(v float64) (float64, bool) {
	if v != v || v > float64(v8kMaxTime) || v < -float64(v8kMaxTime) {
		return 0, false
	}
	if v > 0 {
		return float64(int64(v)), true
	}
	if v < 0 {
		// ceil toward zero
		return float64(int64(-(-v))), true
	}
	return 0, true
}

// v8MakeDay ports date.cc MakeDay (ES5 range checks; day is int on all
// reachable paths).
func v8MakeDay(year, month, day int) (float64, bool) {
	if year < -1000000 || year > 1000000 || month < -10000000 || month > 10000000 {
		return 0, false
	}
	y, m := year, month%12
	if m < 0 {
		m += 12
		y--
	}
	const kYearDelta = 399999
	const kBaseDay = 365*(1970+kYearDelta) + (1970+kYearDelta)/4 -
		(1970+kYearDelta)/100 + (1970+kYearDelta)/400
	y1 := y + kYearDelta
	dayFromYear := 365*y1 + y1/4 - y1/100 + y1/400 - kBaseDay
	if (y%4 != 0) || (y%100 == 0 && y%400 != 0) {
		switch m {
		case 0:
		case 1:
			dayFromYear += 31
		case 2:
			dayFromYear += 59
		case 3:
			dayFromYear += 90
		case 4:
			dayFromYear += 120
		case 5:
			dayFromYear += 151
		case 6:
			dayFromYear += 181
		case 7:
			dayFromYear += 212
		case 8:
			dayFromYear += 243
		case 9:
			dayFromYear += 273
		case 10:
			dayFromYear += 304
		case 11:
			dayFromYear += 334
		}
	} else {
		switch m {
		case 0:
		case 1:
			dayFromYear += 31
		case 2:
			dayFromYear += 60
		case 3:
			dayFromYear += 91
		case 4:
			dayFromYear += 121
		case 5:
			dayFromYear += 152
		case 6:
			dayFromYear += 182
		case 7:
			dayFromYear += 213
		case 8:
			dayFromYear += 244
		case 9:
			dayFromYear += 274
		case 10:
			dayFromYear += 305
		case 11:
			dayFromYear += 335
		}
	}
	return float64(dayFromYear-1) + float64(day), true
}

// v8ToUTC ports DateCache::ToUTC: subtract the local-zone offset at the
// instant (whole-millisecond input within the ECMA range).
func v8ToUTC(localMs int64) int64 {
	sec := localMs / 1000
	nsec := (localMs - sec*1000) * 1e6
	t := time.Unix(sec, nsec).In(time.Local)
	_, off := t.Zone() // seconds East of UTC at that instant
	return localMs - int64(off)*1000
}

func v8isMonth(x int) bool { return x >= 1 && x <= 12 }
func v8isDay(x int) bool   { return x >= 1 && x <= 31 }

// ---------------------------------------------------------------------------
// Tokenizer / keyword table (port of dateparser.{h,inl.h,cc})
// ---------------------------------------------------------------------------

const (
	v8TokInvalid = -6
	v8TokUnknown = -5
	v8TokWS      = -4
	v8TokNumber  = -3
	v8TokSymbol  = -2
	v8TokEOF     = -1
	v8KwInvalid  = 0
	v8KwMonth    = 1
	v8KwTz       = 2
	v8KwSep      = 3
	v8KwAmpm     = 4
)

type v8Token struct {
	kind   int
	length int
	value  int
}

func (t v8Token) isNumber() bool        { return t.kind == v8TokNumber }
func (t v8Token) isWS() bool            { return t.kind == v8TokWS }
func (t v8Token) isEOF() bool           { return t.kind == v8TokEOF }
func (t v8Token) isSymbol(ch rune) bool { return t.kind == v8TokSymbol && rune(t.value) == ch }
func (t v8Token) isSign() bool {
	return t.kind == v8TokSymbol && (rune(t.value) == '-' || rune(t.value) == '+')
}
func (t v8Token) isKwType(k int) bool      { return t.kind == k }
func (t v8Token) isFixedLenNum(l int) bool { return t.kind == v8TokNumber && t.length == l }
func (t v8Token) isKeywordZ() bool         { return t.kind == v8KwTz && t.length == 1 && t.value == 0 }
func (t v8Token) asciiSign() int {
	if t.kind == v8TokSymbol && rune(t.value) == '+' {
		return 1
	}
	return -1
}

// v8isWS: ES5 WhiteSpace relevant to V8 InputReader (ASCII + common ranges).
func v8isWS(c rune) bool {
	switch {
	case c == 0x0009 || c == 0x000B || c == 0x000C || c == 0xFEFF || c == 0x0020 ||
		c == 0x00A0 || c == 0x1680 || c == 0x3000 || c == 0x202F || c == 0x205F:
		return true
	case c >= 0x2000 && c <= 0x200A:
		return true
	}
	return false
}

func v8isWSoLT(c rune) bool {
	return v8isWS(c) || c == 0x000A || c == 0x000D || c == 0x2028 || c == 0x2029
}

type v8Reader struct {
	seq []rune
	i   int
	ch  rune
}

func (r *v8Reader) advance() {
	if r.i < len(r.seq) {
		r.ch = r.seq[r.i]
		r.i++
	} else {
		r.ch = 0
		r.i++ // V8 index_ advances past the end unconditionally (length = pos-start)
	}
}

func (r *v8Reader) isEnd() bool   { return r.ch == 0 }
func (r *v8Reader) isDigit() bool { return r.ch >= '0' && r.ch <= '9' }

// skipParentheses mirrors InputReader::SkipParentheses: "(...)" balanced.
func (r *v8Reader) skipParentheses() bool {
	if r.ch != '(' {
		return false
	}
	balance := 0
	for {
		if r.ch == ')' {
			balance--
		} else if r.ch == '(' {
			balance++
		}
		r.advance()
		if balance <= 0 || r.ch == 0 {
			return true
		}
	}
}

// readUnsignedNumeral: skip leading zeros; process at most 9 significant
// digits. Returns (value, totalDigits).
func (r *v8Reader) readUnsignedNumeral() (int, int) {
	start := r.i
	n, sig := 0, 0
	for r.ch == '0' {
		r.advance()
	}
	for r.ch >= '0' && r.ch <= '9' {
		if sig < 9 {
			n = n*10 + int(r.ch-'0')
		}
		sig++
		r.advance()
	}
	return n, r.i - start
}

// readWord mirrors InputReader::ReadWord: lowercased 3-char prefix (A-Z),
// raw chars otherwise.
func (r *v8Reader) readWord() ([3]rune, int) {
	var prefix [3]rune
	length := 0
	for r.ch > 0 && r.ch >= 'A' && !v8isWS(r.ch) {
		if length < 3 {
			c := r.ch
			if c >= 'A' && c <= 'Z' {
				c += 32
			}
			prefix[length] = c
		}
		length++
		r.advance()
	}
	return prefix, length
}

type v8kw struct {
	pre [3]rune
	typ int
	val int
}

// Keyword table (V8 verbatim, order matters for Lookup).
func kw(p string, typ, val int) v8kw {
	var pre [3]rune
	for i, c := range p {
		if i >= 3 {
			break
		}
		pre[i] = c
	}
	return v8kw{pre: pre, typ: typ, val: val}
}

var v8kwTable = []v8kw{
	kw("jan", v8KwMonth, 1),
	kw("feb", v8KwMonth, 2),
	kw("mar", v8KwMonth, 3),
	kw("apr", v8KwMonth, 4),
	kw("may", v8KwMonth, 5),
	kw("jun", v8KwMonth, 6),
	kw("jul", v8KwMonth, 7),
	kw("aug", v8KwMonth, 8),
	kw("sep", v8KwMonth, 9),
	kw("oct", v8KwMonth, 10),
	kw("nov", v8KwMonth, 11),
	kw("dec", v8KwMonth, 12),
	kw("am", v8KwAmpm, 0),
	kw("pm", v8KwAmpm, 12),
	kw("ut", v8KwTz, 0),
	kw("utc", v8KwTz, 0),
	kw("z", v8KwTz, 0),
	kw("gmt", v8KwTz, 0),
	kw("cdt", v8KwTz, -5),
	kw("cst", v8KwTz, -6),
	kw("edt", v8KwTz, -4),
	kw("est", v8KwTz, -5),
	kw("mdt", v8KwTz, -6),
	kw("mst", v8KwTz, -7),
	kw("pdt", v8KwTz, -7),
	kw("pst", v8KwTz, -8),
	kw("t", v8KwSep, 0),
	{typ: v8KwInvalid},
}

func v8kwLookup(pre [3]rune, length int) (int, int) {
	for _, e := range v8kwTable {
		j := 0
		for j < 3 && pre[j] == e.pre[j] {
			j++
		}
		if j == 3 && (length <= 3 || e.typ == v8KwMonth) {
			return e.typ, e.val
		}
	}
	return v8KwInvalid, 0
}

// v8readMilliseconds ports ReadMilliseconds.
func v8readMilliseconds(n, length int) int {
	if length < 3 {
		if length == 1 {
			n *= 100
		} else if length == 2 {
			n *= 10
		}
	} else if length > 3 {
		if length > 9 {
			length = 9
		}
		factor := 1
		for length > 3 {
			factor *= 10
			length--
		}
		n /= factor
	}
	return n
}

type v8Scanner struct {
	in   *v8Reader
	peek v8Token
}

// scan ports DateStringTokenizer::Scan.
func (s *v8Scanner) scan() v8Token {
	in := s.in
	if in.isEnd() {
		return v8Token{kind: v8TokEOF}
	}
	if in.isDigit() {
		n, length := in.readUnsignedNumeral()
		return v8Token{kind: v8TokNumber, length: length, value: n}
	}
	if in.ch == ':' || in.ch == '-' || in.ch == '+' || in.ch == '.' || in.ch == ')' {
		v := in.ch
		in.advance()
		return v8Token{kind: v8TokSymbol, length: 1, value: int(v)}
	}
	if in.ch > 0 && in.ch >= 'A' && !v8isWS(in.ch) {
		prefix, length := in.readWord()
		typ, val := v8kwLookup(prefix, length)
		return v8Token{kind: typ, length: length, value: val}
	}
	if in.ch != 0 && v8isWSoLT(in.ch) {
		in.advance()
		return v8Token{kind: v8TokWS}
	}
	if in.ch == '(' {
		in.skipParentheses()
		return v8Token{kind: v8TokUnknown}
	}
	in.advance()
	return v8Token{kind: v8TokUnknown}
}

func (s *v8Scanner) peekNext() v8Token {
	return s.peek
}

func (s *v8Scanner) next() v8Token {
	t := s.peek
	s.peek = s.scan()
	return t
}

// skipSymbol mirrors DateStringTokenizer::SkipSymbol (peek position).
func (s *v8Scanner) skipSymbol(c rune) bool {
	if s.peek.kind == v8TokSymbol && rune(s.peek.value) == c {
		s.peek = s.scan()
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Composers (Day/Time/TimeZone)
// ---------------------------------------------------------------------------

type v8tzComposer struct {
	sign   int // v8TokNone until set; then -1 or 1
	hour   int
	minute int // v8TokNone until set
}

func (z *v8tzComposer) set(offHours int) {
	if offHours < 0 {
		z.sign = -1
		z.hour = -offHours
	} else {
		z.sign = 1
		z.hour = offHours
	}
	z.minute = 0
}

func (z *v8tzComposer) setSign(s int) {
	if s < 0 {
		z.sign = -1
	} else {
		z.sign = 1
	}
}

func (z *v8tzComposer) setHour(h int)   { z.hour = h }
func (z *v8tzComposer) setMinute(m int) { z.minute = m }
func (z *v8tzComposer) isUTC() bool     { return z.hour == 0 && z.minute == 0 }
func (z *v8tzComposer) empty() bool     { return z.hour == v8TokNone }
func (z *v8tzComposer) expecting(n int) bool {
	return z.hour != v8TokNone && z.minute == v8TokNone && n >= 0 && n <= 59
}

// v8timeComposer ports TimeComposer.
type v8timeComposer struct {
	comp       [4]int
	index      int
	hourOffset int // v8TokNone until set
}

func (t *v8timeComposer) empty() bool { return t.index == 0 }

func (t *v8timeComposer) expecting(n int) bool {
	return (t.index == 1 && n >= 0 && n <= 59) ||
		(t.index == 2 && n >= 0 && n <= 59) ||
		(t.index == 3 && n >= 0 && n <= 999)
}

func (t *v8timeComposer) add(n int) bool {
	if t.index < 4 {
		t.comp[t.index] = n
		t.index++
		return true
	}
	return false
}

func (t *v8timeComposer) addFinal(n int) bool {
	if !t.add(n) {
		return false
	}
	for t.index < 4 {
		t.comp[t.index] = 0
		t.index++
	}
	return true
}

func (t *v8timeComposer) setHourOffset(n int) { t.hourOffset = n }

type v8dateResult struct {
	year, month, day, hour, minute, second, milli int
	hasTZ                                         bool
	tzSec                                         int
}

func (t *v8timeComposer) write(out *v8dateResult) bool {
	for t.index < 4 {
		t.comp[t.index] = 0
		t.index++
	}
	h, mi, s, ms := t.comp[0], t.comp[1], t.comp[2], t.comp[3]
	if t.hourOffset != v8TokNone {
		if h < 0 || h > 12 {
			return false
		}
		h %= 12
		h += t.hourOffset
	}
	ok := h >= 0 && h <= 23 && mi >= 0 && mi <= 59 && s >= 0 && s <= 59 && ms >= 0 && ms <= 999
	if !ok && !(h == 24 && mi == 0 && s == 0 && ms == 0) {
		return false
	}
	out.hour, out.minute, out.second, out.milli = h, mi, s, ms
	return true
}

// v8dayComposer ports DayComposer.
type v8dayComposer struct {
	comp       [3]int
	index      int
	namedMonth int // v8TokNone until set
	isISODate  bool
}

func (d *v8dayComposer) empty() bool { return d.index == 0 }
func (d *v8dayComposer) add(n int) bool {
	if d.index < 3 {
		d.comp[d.index] = n
		d.index++
		return true
	}
	return false
}

func (d *v8dayComposer) setNamedMonth(n int) { d.namedMonth = n }
func (d *v8dayComposer) setISODate()         { d.isISODate = true }

func (d *v8dayComposer) write(out *v8dateResult) bool {
	if d.index < 1 {
		return false
	}
	for d.index < 3 {
		d.comp[d.index] = 1
		d.index++
	}
	year := 0
	month := v8TokNone
	day := v8TokNone
	if d.namedMonth == v8TokNone {
		if d.isISODate || !v8isDay(d.comp[0]) {
			// YMD
			year, month, day = d.comp[0], d.comp[1], d.comp[2]
		} else {
			// MD(Y)
			month, day = d.comp[0], d.comp[1]
			year = d.comp[2]
		}
	} else {
		month = d.namedMonth
		if d.index == 1 {
			// MD or DM
			day = d.comp[0]
		} else if !v8isDay(d.comp[0]) {
			// YMD, MYD, or YDM
			year, day = d.comp[0], d.comp[1]
		} else {
			// DMY, MDY, or DYM
			day, year = d.comp[0], d.comp[1]
		}
	}
	if !d.isISODate {
		if year >= 0 && year <= 49 {
			year += 2000
		} else if year >= 50 && year <= 99 {
			year += 1900
		}
	}
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return false
	}
	out.year, out.month, out.day = year, month-1, day
	return true
}

// ---------------------------------------------------------------------------
// ES5 date-time string parser (port of ParseES5DateTime)
// ---------------------------------------------------------------------------

func v8parseES5DateTime(sc *v8Scanner, day *v8dayComposer, timeC *v8timeComposer, tz *v8tzComposer) v8Token {
	if sc.peekNext().isSign() {
		signTok := sc.next()
		if !sc.peekNext().isFixedLenNum(6) {
			return signTok
		}
		sign := signTok.asciiSign()
		yearTok := sc.next()
		if sign < 0 && yearTok.value == 0 {
			return signTok
		}
		day.add(sign * yearTok.value)
	} else if sc.peekNext().isFixedLenNum(4) {
		yearTok := sc.next()
		day.add(yearTok.value)
	} else {
		return sc.next()
	}
	if sc.skipSymbol('-') {
		if !sc.peekNext().isFixedLenNum(2) || !v8isMonth(sc.peekNext().value) {
			return sc.next()
		}
		day.add(sc.next().value)
		if sc.skipSymbol('-') {
			if !sc.peekNext().isFixedLenNum(2) || !v8isDay(sc.peekNext().value) {
				return sc.next()
			}
			day.add(sc.next().value)
		}
	}
	if !sc.peekNext().isKwType(v8KwSep) {
		if !sc.peekNext().isEOF() {
			return sc.next()
		}
	} else {
		sc.next() // 't'
		hourTok := sc.peekNext()
		if !hourTok.isFixedLenNum(2) || hourTok.value > 24 {
			return v8Token{kind: v8TokInvalid}
		}
		hourIs24 := hourTok.value == 24
		if !timeC.add(sc.next().value) {
			return v8Token{kind: v8TokInvalid}
		}
		if !sc.skipSymbol(':') {
			return v8Token{kind: v8TokInvalid}
		}
		minTok := sc.peekNext()
		if !minTok.isFixedLenNum(2) || minTok.value > 59 || (hourIs24 && minTok.value > 0) {
			return v8Token{kind: v8TokInvalid}
		}
		timeC.add(sc.next().value)
		if sc.skipSymbol(':') {
			secTok := sc.peekNext()
			if !secTok.isFixedLenNum(2) || secTok.value > 59 || (hourIs24 && secTok.value > 0) {
				return v8Token{kind: v8TokInvalid}
			}
			timeC.add(sc.next().value)
			if sc.skipSymbol('.') {
				fracTok := sc.peekNext()
				if !fracTok.isNumber() || (hourIs24 && fracTok.value > 0) {
					return v8Token{kind: v8TokInvalid}
				}
				frac := sc.next()
				timeC.add(v8readMilliseconds(frac.value, frac.length))
			}
		}
		if sc.peekNext().isKeywordZ() {
			sc.next()
			tz.set(0)
		} else if sc.peekNext().isSymbol('+') || sc.peekNext().isSymbol('-') {
			tok := sc.next()
			tz.setSign(tok.asciiSign())
			hmTok := sc.peekNext()
			if hmTok.isFixedLenNum(4) {
				hm := hmTok.value
				if hm/100 > 23 || hm%100 > 59 {
					return v8Token{kind: v8TokInvalid}
				}
				tz.setHour(hm / 100)
				tz.setMinute(hm % 100)
			} else {
				hTok := sc.peekNext()
				if !hTok.isFixedLenNum(2) || hTok.value > 23 {
					return v8Token{kind: v8TokInvalid}
				}
				tz.setHour(sc.next().value)
				if !sc.skipSymbol(':') {
					return v8Token{kind: v8TokInvalid}
				}
				mTok := sc.peekNext()
				if !mTok.isFixedLenNum(2) || mTok.value > 59 {
					return v8Token{kind: v8TokInvalid}
				}
				tz.setMinute(sc.next().value)
			}
		}
		if !sc.peekNext().isEOF() {
			return v8Token{kind: v8TokInvalid}
		}
	}
	// Absent offset: date-only forms are UTC; date-time forms are local.
	if tz.empty() && timeC.empty() {
		tz.set(0)
	}
	day.setISODate()
	return v8Token{kind: v8TokEOF}
}

// ---------------------------------------------------------------------------
// DateParser::Parse (ES5 + legacy grammar)
// ---------------------------------------------------------------------------

func v8dateParse(str []rune, out *v8dateResult) bool {
	reader := &v8Reader{seq: str}
	reader.advance()
	sc := &v8Scanner{in: reader}
	sc.peek = sc.scan()

	day := v8dayComposer{namedMonth: v8TokNone}
	timeC := v8timeComposer{hourOffset: v8TokNone}
	tz := v8tzComposer{sign: v8TokNone, hour: v8TokNone, minute: v8TokNone}

	next := v8parseES5DateTime(sc, &day, &timeC, &tz)
	if next.kind == v8TokInvalid {
		return false
	}
	hasReadNumber := !day.empty()

	for token := next; !token.isEOF(); token = sc.next() {
		if token.isNumber() {
			hasReadNumber = true
			n := token.value
			if sc.skipSymbol(':') {
				if sc.skipSymbol(':') {
					// n + "::"
					if !timeC.empty() {
						return false
					}
					timeC.add(n)
					timeC.add(0)
				} else {
					// n + ":"
					if !timeC.add(n) {
						return false
					}
					if sc.peekNext().isSymbol('.') {
						sc.next()
					}
				}
			} else if sc.skipSymbol('.') && timeC.expecting(n) {
				timeC.add(n)
				if !sc.peekNext().isNumber() {
					return false
				}
				frac := sc.next()
				timeC.addFinal(v8readMilliseconds(frac.value, frac.length))
			} else if tz.expecting(n) {
				tz.setMinute(n)
			} else if timeC.expecting(n) {
				timeC.addFinal(n)
				pk := sc.peekNext()
				if !pk.isEOF() && !pk.isWS() && !pk.isKeywordZ() && !pk.isSign() {
					return false
				}
			} else {
				if !day.add(n) {
					return false
				}
				sc.skipSymbol('-')
			}
		} else if token.kind >= 0 {
			// Keyword (including unrecognized/garbage words).
			// C++ chain: if (type == AM_PM && !time.IsEmpty()) {}
			//              else if (type == MONTH_NAME) {}
			//              else if (type == TIME_ZONE_NAME && has_read_number) {}
			//              else { /* garbage: illegal if a number has been read */ }
			// Note: AM_PM with an empty TimeComposer falls into the garbage
			// branch (e.g. "1 pm" is invalid, "12:00 pm" is valid).
			switch {
			case token.kind == v8KwAmpm && !timeC.empty():
				timeC.setHourOffset(token.value)
			case token.kind == v8KwMonth:
				day.setNamedMonth(token.value)
				sc.skipSymbol('-')
			case token.kind == v8KwTz && hasReadNumber:
				tz.set(token.value)
			default:
				// Garbage words are illegal if a number has been read.
				if hasReadNumber {
					return false
				}
				if sc.peekNext().isNumber() {
					return false
				}
			}
		} else if token.isSign() && (tz.isUTC() || !timeC.empty()) {
			// UTC offset (only after UTC or time).
			tz.setSign(token.asciiSign())
			n := 0
			length := 0
			if sc.peekNext().isNumber() {
				numTok := sc.next()
				n, length = numTok.value, numTok.length
			}
			hasReadNumber = true
			if sc.peekNext().isSymbol(':') {
				tz.setHour(n)
				tz.setMinute(v8TokNone)
			} else if length == 2 || length == 1 {
				// Time zones like GMT-8
				tz.setHour(n)
				tz.setMinute(0)
			} else if length == 4 || length == 3 {
				// hhmm form
				tz.setHour(n / 100)
				tz.setMinute(n % 100)
			} else {
				return false
			}
		} else if (token.isSign() || token.isSymbol(')')) && (token == token && hasReadNumber) {
			// Extra sign or ')' is illegal if a number has been read.
			return false
		}
		// Other characters and whitespace are ignored.
	}

	dayOK := day.write(out)
	timeOK := false
	if dayOK {
		timeOK = timeC.write(out)
	}
	if dayOK && timeOK {
		// TimeZoneComposer::Write: if (sign_ != kNone) { explicit offset }
		// else { out[UTC_OFFSET] = NaN } -> local resolution in v8parseDateString.
		if tz.sign != v8TokNone {
			h, m := tz.hour, tz.minute
			if h == v8TokNone {
				h = 0
			}
			if m == v8TokNone {
				m = 0
			}
			total := h*3600 + m*60
			if tz.sign < 0 {
				total = -total
			}
			out.hasTZ = true
			out.tzSec = total
		}
	}
	return dayOK && timeOK
}

// v8parseDateString: `new Date(string)` -> epoch ms (or false), V8-parity.
func v8parseDateString(s string) (float64, bool) {
	var out v8dateResult
	if !v8dateParse([]rune(s), &out) {
		return 0, false
	}
	day, ok := v8MakeDay(out.year, out.month, out.day)
	if !ok {
		return 0, false
	}
	timeVal := float64(out.hour)*3600000 + float64(out.minute)*60000 +
		float64(out.second)*1000 + float64(out.milli)
	dateVal := timeVal + day*86400000
	if out.hasTZ {
		dateVal -= float64(out.tzSec) * 1000
	} else {
		const bound = float64(v8kBeforeUTC)
		if dateVal < -bound || dateVal > bound {
			return 0, false
		}
		dateVal = float64(v8ToUTC(int64(dateVal)))
	}
	clipped, ok := v8TimeClip(dateVal)
	if !ok {
		return 0, false
	}
	return clipped, true
}

// asJSNumber reports whether v (JSON-decoded Go value) is a JS number.
func asJSNumber(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case bool:
		// V8 ToNumeric: booleans coerce to 1/0.
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// modifiedEpoch returns the epoch-ms for a `resource.modified` value with
// byte-for-byte V8 `new Date(value)` semantics, or false (= Invalid Date).
func modifiedEpoch(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case string:
		return v8parseDateString(x)
	default:
		f, isNum := asJSNumber(v)
		if !isNum {
			return 0, false
		}
		c, ok := v8TimeClip(f)
		if !ok {
			return 0, false
		}
		return float64(c), true
	}
}
