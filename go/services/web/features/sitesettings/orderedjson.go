package sitesettings

import (
	"strconv"
	"strings"
	"unicode/utf16"
)

// ordered JSON parser — preserves object key insertion order (encoding/json
// maps drop it). Produces the package's value types: Obj, []any, string,
// bool, float64, nil. Used to decode request bodies so cleanSectionInput +
// setSection + re-emission keep the Node wire order.

type jsonParser struct {
	s string
	i int
}

func parseOrderedJSON(s string) (any, bool) {
	p := &jsonParser{s: s, i: 0}
	p.skipWS()
	v, ok := p.parseValue()
	if !ok {
		return nil, false
	}
	p.skipWS()
	return v, true
}

func (p *jsonParser) skipWS() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *jsonParser) peek() (byte, bool) {
	if p.i >= len(p.s) {
		return 0, false
	}
	return p.s[p.i], true
}

func (p *jsonParser) parseValue() (any, bool) {
	p.skipWS()
	c, ok := p.peek()
	if !ok {
		return nil, false
	}
	switch {
	case c == '{':
		return p.parseObject()
	case c == '[':
		return p.parseArray()
	case c == '"':
		s, ok := p.parseString()
		return s, ok
	case c == 't' || c == 'f':
		return p.parseBool()
	case c == 'n':
		return p.parseNull()
	}
	return p.parseNumber()
}

func (p *jsonParser) parseObject() (Obj, bool) {
	// consume '{'
	p.i++
	out := Obj{}
	p.skipWS()
	if c, ok := p.peek(); ok && c == '}' {
		p.i++
		return out, true
	}
	for {
		p.skipWS()
		key, ok := p.parseString()
		if !ok {
			return out, false
		}
		p.skipWS()
		if c, ok := p.peek(); !ok || c != ':' {
			return out, false
		}
		p.i++
		val, ok := p.parseValue()
		if !ok {
			return out, false
		}
		out = append(out, KV{key, val})
		p.skipWS()
		c, ok := p.peek()
		if !ok {
			return out, false
		}
		if c == ',' {
			p.i++
			continue
		}
		if c == '}' {
			p.i++
			return out, true
		}
		return out, false
	}
}

func (p *jsonParser) parseArray() ([]any, bool) {
	p.i++ // '['
	out := []any{}
	p.skipWS()
	if c, ok := p.peek(); ok && c == ']' {
		p.i++
		return out, true
	}
	for {
		val, ok := p.parseValue()
		if !ok {
			return out, false
		}
		out = append(out, val)
		p.skipWS()
		c, ok := p.peek()
		if !ok {
			return out, false
		}
		if c == ',' {
			p.i++
			continue
		}
		if c == ']' {
			p.i++
			return out, true
		}
		return out, false
	}
}

func (p *jsonParser) parseBool() (bool, bool) {
	if strings.HasPrefix(p.s[p.i:], "true") {
		p.i += 4
		return true, true
	}
	if strings.HasPrefix(p.s[p.i:], "false") {
		p.i += 5
		return false, true
	}
	return false, false
}

func (p *jsonParser) parseNull() (any, bool) {
	if strings.HasPrefix(p.s[p.i:], "null") {
		p.i += 4
		return nil, true
	}
	return nil, false
}

func (p *jsonParser) parseNumber() (float64, bool) {
	start := p.i
	digits := false
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c >= '0' && c <= '9' {
			digits = true
			p.i++
			continue
		}
		if c == '.' || (c == 'e' || c == 'E') || c == '+' || (c == '-' && p.i > start) {
			p.i++
			continue
		}
		break
	}
	if !digits {
		return 0, false
	}
	tok := p.s[start:p.i]
	f, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func (p *jsonParser) parseString() (string, bool) {
	// expects opening quote
	if c, ok := p.peek(); !ok || c != '"' {
		return "", false
	}
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '"' {
			p.i++
			return b.String(), true
		}
		if c == '\\' {
			p.i++
			if p.i >= len(p.s) {
				return "", false
			}
			e := p.s[p.i]
			switch e {
			case '"':
				b.WriteByte('"')
				p.i++
			case '\\':
				b.WriteByte('\\')
				p.i++
			case '/':
				b.WriteByte('/')
				p.i++
			case 'b':
				b.WriteByte('\b')
				p.i++
			case 'f':
				b.WriteByte('\f')
				p.i++
			case 'n':
				b.WriteByte('\n')
				p.i++
			case 'r':
				b.WriteByte('\r')
				p.i++
			case 't':
				b.WriteByte('\t')
				p.i++
			case 'u':
				if p.i+4 >= len(p.s)+0 && p.i+4 > len(p.s) {
					return "", false
				}
				if p.i+4 >= len(p.s) {
					return "", false
				}
				hexs := p.s[p.i+1 : p.i+5]
				r, err := strconv.ParseUint(hexs, 16, 32)
				if err != nil {
					return "", false
				}
				p.i += 5
				if r <= 0xFFFF {
					b.WriteRune(rune(r))
				} else {
					// surrogate pair
					if p.i+6 <= len(p.s) && p.s[p.i] == '\\' && p.s[p.i+1] == 'u' {
						hexs2 := p.s[p.i+2 : p.i+6]
						r2, err2 := strconv.ParseUint(hexs2, 16, 32)
						if err2 == nil && r2 <= 0xFFFF {
							b.WriteRune(0x10000 + ((rune(r-0xD800) << 10) | rune(r2-0xDC00)))
							p.i += 6
						} else {
							b.WriteRune(rune(r))
						}
					} else {
						b.WriteRune(rune(r))
					}
				}
			default:
				return "", false
			}
			continue
		}
		b.WriteByte(c)
		p.i++
	}
	return "", false
}

var _ = utf16.DecodeRune
