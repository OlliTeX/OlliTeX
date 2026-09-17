package orcidpicker

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Node OrcidService.mjs constants.
const (
	orcidPubAPI   = "https://pub.orcid.org/v3.0"
	doiBase       = "https://doi.org"
	maxBodySize   = 2 * 1024 * 1024 // MAX_BODY_SIZE
	maxRedirects  = 5               // MAX_REDIRECTS
	doiRegExSrc   = `^10\.\d{4,9}/\S+$`
	orcidRegExSrc = `^\d{4}-\d{4}-\d{4}-\d{3}[\dX]$`
)

var orcidRe = regexp.MustCompile(orcidRegExSrc)
var doiRe = regexp.MustCompile(doiRegExSrc)

// errBlocked / transport-text mapping: Node's router emits err?.message.
const (
	errUpstreamFmt = "Upstream API responded with %d"
	errTooLarge    = "Response too large"
	errTooManyRed  = "Too many redirects"
	errNoLocation  = "Redirect without a Location header"
	errBlocked     = "Blocked request to a non-public network address"
	// undici / V8 texts for the non-gated quirk paths:
	errSearchMap = `data['expanded-result'].map is not a function`
	errGroupIt   = `data.group is not iterable`
	errSNull     = `Cannot read properties of null (reading 'title')`
	errSUnd      = `Cannot read properties of undefined (reading 'title')`
	errExtIt     = `s['external-ids'].external-id is not iterable`
)

// ---------- Node-faithful string helpers ----------

// encU — Node encodeURIComponent: everything except A-Za-z0-9 - _ . ! ~ * ' ( )
// is percent-encoded (UTF-8 bytes, uppercase hex).
func encU(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '-' || c == '_' || c == '.' || c == '!' || c == '~' || c == '*' || c == '\'' || c == '(' || c == ')':
			b.WriteByte(c)
		default:
			if c < 0x80 {
				b.WriteString(fmtPct(c))
			} else {
				// UTF-8 continuation bytes of the rune — encode the whole
				// rune's bytes.
				r, size := decodeRuneAt(s, i)
				enc := []byte(string(r))
				for _, e := range enc {
					b.WriteString(fmtPct(e))
				}
				i += size - 1
			}
		}
	}
	return b.String()
}

func fmtPct(c byte) string {
	hexd := "0123456789ABCDEF"
	return "%" + string(hexd[c>>4]) + string(hexd[c&0xF])
}
func decodeRuneAt(s string, i int) (rune, int) {
	if i >= len(s) {
		return 0, 0
	}
	c := s[i]
	if c < 0x80 {
		return rune(c), 1
	}
	if c < 0xE0 && i+1 < len(s) {
		return rune(c&0x1F)<<6 | rune(s[i+1]&0x3F), 2
	}
	if c < 0xF0 && i+2 < len(s) {
		return rune(c&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F), 3
	}
	if i+3 < len(s) {
		return rune(c&0x07)<<18 | rune(s[i+1]&0x3F)<<12 | rune(s[i+2]&0x3F)<<6 | rune(s[i+3]&0x3F), 4
	}
	return rune(c), 1
}

// jsString — JSON.stringify(string) escaping (no HTML escaping; control
// chars \b \f \n \r \t named, others \u00XX; everything else verbatim).
func jsString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\b':
			b.WriteString(`\b`)
		case c == '\f':
			b.WriteString(`\f`)
		case c < 0x20:
			b.WriteString(fmtPct2(c))
		case c < 0x80:
			b.WriteByte(c)
		default:
			r, size := decodeRuneAt(s, i)
			b.WriteString(string(r))
			i += size - 1
		}
	}
	return b.String()
}

func fmtPct2(c byte) string {
	// \u00XX (JS String.fromCharCode escape of a control byte)
	hexd := "0123456789abcdef"
	return `\u00` + string(hexd[c>>4]) + string(hexd[c&0xF])
}

// encURI — Node encodeURI: like encodeURIComponent but leaves the URI
// reserved characters raw: ; , / ? : @ & = + $ and the unescaped set.
func encURI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '-' || c == '_' || c == '.' || c == '!' || c == '~' || c == '*' || c == '\'' || c == '(' || c == ')' ||
			c == ';' || c == ',' || c == '/' || c == '?' || c == ':' || c == '@' || c == '&' || c == '=' || c == '+' || c == '$':
			b.WriteByte(c)
		default:
			if c < 0x80 {
				b.WriteString(fmtPct(c))
			} else {
				r, size := decodeRuneAt(s, i)
				enc := []byte(string(r))
				for _, e := range enc {
					b.WriteString(fmtPct(e))
				}
				i += size - 1
			}
		}
	}
	return b.String()
}

// jsNumberFinite — Node Number.isFinite(Number(s)): trim; decimal/hex ok;
// "Infinity"/"NaN" not finite; anything else NaN → not finite.
func jsNumberFinite(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	if isJSInf(t) || isJSNaN(t) {
		return false
	}
	if strings.HasPrefix(t, "+") || strings.HasPrefix(t, "-") {
		t = t[1:]
	}
	hex := regexp.MustCompile(`^(0[xX])?[0-9a-fA-F]+$`)
	if (strings.HasPrefix(t, "0x") || strings.HasPrefix(t, "0X")) && hex.MatchString(t[2:]) {
		return true // JS: hex literal → finite
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil {
		// JS Number also accepts "1e5" etc. (ParseFloat does) — but NOT
		// bare "." or "1.2.3" (ParseFloat rejects, like JS).
		return false
	}
	return !math.IsInf(v, 0) && !math.IsNaN(v)
}
func isJSInf(t string) bool {
	l := strings.ToLower(t)
	if strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") {
		l = l[1:]
	}
	return l == "inf" || l == "infinity"
}

func isJSNaN(t string) bool {
	l := strings.ToLower(t)
	if strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") {
		l = l[1:]
	}
	return l == "nan"
}

// isValidOrcid — /^\d{4}-\d{4}-\d{4}-\d{3}[\dX]$/ against raw.trim().
func isValidOrcid(raw string) bool {
	return orcidRe.MatchString(strings.TrimSpace(raw))
}

// jsIntPart — Node parseInt(s, 10) || 0 semantics (leading digit run with
// optional sign; everything after the first non-digit stops the parse; no
// digits at all → NaN → 0).
func jsIntPart(s string) int64 {
	t := strings.TrimSpace(s)
	i := 0
	neg := false
	if i < len(t) && (t[i] == '+' || t[i] == '-') {
		neg = t[i] == '-'
		i++
	}
	var v int64
	digited := false
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		v = v*10 + int64(t[i]-'0')
		digited = true
		i++
	}
	if !digited {
		return 0
	}
	if neg {
		return -v
	}
	return v
}
