// Package llmsettings — P6.4a: the OlliTeX "llm" module settings surface
// (services/web/modules/llm), Go drop-in for the Node handlers:
//
//	BYO provider rows        /user/llm-providers*  (LLMSettingsController.mjs)
//	selected model           /user/llm/selected-model
//	compliance rubrics       /user/llm/compliance
//	usage (user)             /user/llm-usage
//	grammar prefs            /user/llm-settings/grammar
//	legacy page 301          /user/llm-settings
//	admin settings file CRUD /admin/llm/settings*  (LLMAdminController.mjs)
//	admin usage              /admin/llm/usage
//
// Node authority files (byte pins in /tmp/p64a_node.json):
//
//	LLMSettingsController.mjs, LLMAdminController.mjs, LLMCrypto.mjs,
//	LLMClient.mjs, LLMUsage.mjs, LLMGrammar.mjs, LLMPrompts.mjs.
//
// Conventions (same as P6.3b):
//   - responses are hand-built JSON in the Node exact key order;
//   - the admin settings file is read/written with Node key-order +
//     JSON.stringify(data, null, 2) semantics (ojson below);
//   - anonymous / member-denied responses come from the core global
//     chain; handlers only see authenticated requests.
package llmsettings

import (
	"crypto/rand"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// ---------- ordered JSON (Node key-order preserved) ----------

// obj — ordered JSON object.
type obj []kv

type kv struct {
	k string
	v any
}

func (o obj) get(key string) (any, bool) {
	for _, e := range o {
		if e.k == key {
			return e.v, true
		}
	}
	return nil, false
}

// set — Node semantics: existing key keeps its position (value replaced);
// new key appended at the end.
func (o obj) set(key string, val any) obj {
	for i := range o {
		if o[i].k == key {
			o[i].v = val
			return o
		}
	}
	return append(o, kv{key, val})
}

func (o obj) has(key string) bool {
	_, ok := o.get(key)
	return ok
}

func (o obj) keys() []string {
	out := make([]string, 0, len(o))
	for _, e := range o {
		out = append(out, e.k)
	}
	return out
}

func (o obj) array(key string) []any {
	v, ok := o.get(key)
	if !ok {
		return nil
	}
	a, ok := v.([]any)
	return a
}

func (o obj) string(key string) string {
	v, _ := o.get(key)
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// str — string(key) alias (readability at call sites).
func (o obj) str(key string) string { return o.string(key) }

func (o obj) bool(key string) bool {
	v, _ := o.get(key)
	b, _ := v.(bool)
	return b
}

func (o obj) num(key string) float64 {
	v, _ := o.get(key)
	f, _ := v.(float64)
	return f
}

// ojsonParser — recursive descent into Node-value space:
// objects -> obj (ordered), arrays -> []any, numbers -> float64.
type ojsonParser struct {
	s string
	i int
}

func (p *ojsonParser) ws() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *ojsonParser) peek() (byte, bool) {
	p.ws()
	if p.i >= len(p.s) {
		return 0, false
	}
	return p.s[p.i], true
}

func ojsonParse(s string) (any, error) {
	p := &ojsonParser{s: s}
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.ws()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("trailing data at %d", p.i)
	}
	return v, nil
}

func (p *ojsonParser) value() (any, error) {
	c, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("unexpected end of input")
	}
	switch {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		return p.string()
	case c == 't':
		if strings.HasPrefix(p.s[p.i:], "true") {
			p.i += 4
			return true, nil
		}
		return nil, fmt.Errorf("invalid literal")
	case c == 'f':
		if strings.HasPrefix(p.s[p.i:], "false") {
			p.i += 5
			return false, nil
		}
		return nil, fmt.Errorf("invalid literal")
	case c == 'n':
		if strings.HasPrefix(p.s[p.i:], "null") {
			p.i += 4
			return nil, nil
		}
		return nil, fmt.Errorf("invalid literal")
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	return nil, fmt.Errorf("unexpected character %q", c)
}

func (p *ojsonParser) object() (obj, error) {
	p.i++ // '{'
	out := obj{}
	c, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("unexpected end in object")
	}
	if c == '}' {
		p.i++
		return out, nil
	}
	for {
		c, ok = p.peek()
		if !ok || c != '"' {
			return nil, fmt.Errorf("expected object key")
		}
		key, err := p.string()
		if err != nil {
			return nil, err
		}
		p.ws()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, fmt.Errorf("expected ':'")
		}
		p.i++
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out = out.set(key, v)
		c, ok = p.peek()
		if !ok {
			return nil, fmt.Errorf("unterminated object")
		}
		if c == '}' {
			p.i++
			return out, nil
		}
		if c != ',' {
			return nil, fmt.Errorf("expected ',' in object")
		}
		p.i++
	}
}

func (p *ojsonParser) array() ([]any, error) {
	p.i++ // '['
	out := []any{}
	c, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("unexpected end in array")
	}
	if c == ']' {
		p.i++
		return out, nil
	}
	for {
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		c, ok = p.peek()
		if !ok {
			return nil, fmt.Errorf("unterminated array")
		}
		if c == ']' {
			p.i++
			return out, nil
		}
		if c != ',' {
			return nil, fmt.Errorf("expected ',' in array")
		}
		p.i++
	}
}

func (p *ojsonParser) string() (string, error) {
	p.i++ // opening quote
	var b strings.Builder
	for {
		if p.i >= len(p.s) {
			return "", fmt.Errorf("unterminated string")
		}
		c := p.s[p.i]
		if c == '"' {
			p.i++
			return b.String(), nil
		}
		if c == '\\' {
			p.i++
			if p.i >= len(p.s) {
				return "", fmt.Errorf("bad escape")
			}
			e := p.s[p.i]
			switch e {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				switch e {
				case 'b':
					b.WriteByte('\b')
				case 'f':
					b.WriteByte('\f')
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(e)
				}
				p.i++
			case 'u':
				if p.i+5 > len(p.s) {
					return "", fmt.Errorf("bad unicode escape")
				}
				n, err := strconv.ParseUint(p.s[p.i+1:p.i+5], 16, 32)
				if err != nil {
					return "", fmt.Errorf("bad unicode escape")
				}
				b.WriteRune(rune(n))
				p.i += 5
			default:
				return "", fmt.Errorf("bad escape %q", e)
			}
			continue
		}
		b.WriteByte(c)
		p.i++
	}
}

func (p *ojsonParser) number() (float64, error) {
	start := p.i
	if p.i < len(p.s) && p.s[p.i] == '-' {
		p.i++
	}
	for p.i < len(p.s) {
		c := p.s[p.i]
		if (c >= '0' && c <= '9') || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-' {
			p.i++
		} else {
			break
		}
	}
	tok := p.s[start:p.i]
	f, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		return 0, fmt.Errorf("bad number %q", tok)
	}
	return f, nil
}

// jsString — Node JSON.stringify string escaping.
func jsString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if c < 0x20 {
				b.WriteString(fmt.Sprintf(`\u%04x`, c))
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func jsNum(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "null"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// jsonWrite — compact Node-form serialization.
func jsonWrite(v any) string {
	var b strings.Builder
	jsonWriteVal(&b, v)
	return b.String()
}

func jsonWriteVal(b *strings.Builder, v any) {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		b.WriteString(jsString(t))
	case int:
		b.WriteString(strconv.Itoa(t))
	case int64:
		b.WriteString(strconv.FormatInt(t, 10))
	case float64:
		b.WriteString(jsNum(t))
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			jsonWriteVal(b, e)
		}
		b.WriteByte(']')
	case obj:
		b.WriteByte('{')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(jsString(e.k))
			b.WriteByte(':')
			jsonWriteVal(b, e.v)
		}
		b.WriteByte('}')
	default:
		b.WriteString("null")
	}
}

// jsonIndent — JSON.stringify(value, null, 2) form.
func jsonIndent(v any) string {
	var b strings.Builder
	jsonIndentVal(&b, v, 0)
	return b.String()
}

func jsonIndentVal(b *strings.Builder, v any, depth int) {
	pad := strings.Repeat(" ", 2*depth)
	inner := strings.Repeat(" ", 2*(depth+1))
	switch t := v.(type) {
	case nil, bool, string, int, int64, float64:
		jsonWriteVal(b, v)
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, e := range t {
			if i > 0 {
				b.WriteString(",\n")
			}
			b.WriteString(inner)
			jsonIndentVal(b, e, depth+1)
		}
		b.WriteString("\n" + pad + "]")
	case obj:
		if len(t) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, e := range t {
			if i > 0 {
				b.WriteString(",\n")
			}
			b.WriteString(inner + jsString(e.k) + ": ")
			jsonIndentVal(b, e.v, depth+1)
		}
		b.WriteString("\n" + pad + "}")
	default:
		jsonWriteVal(b, v)
	}
}

// ---------- LLMCrypto.mjs parity (at-rest key encryption) ----------

const lEncPrefix = "enc:v1:"
const lKekSalt = "overleaf-lab-llm-key-v1"

type lCrypto struct {
	secret string // LLM_KEY_SECRET ("" = plaintext mode)
}

func newLCrypto() *lCrypto {
	return &lCrypto{secret: lEnv("LLM_KEY_SECRET")}
}

func (c *lCrypto) key() []byte {
	if c.secret == "" {
		return nil
	}
	// Node crypto.scryptSync(secret, salt, 32): defaults N=16384, r=8, p=1.
	k, err := scryptKey([]byte(c.secret), []byte(lKekSalt), 16384, 8, 1, 32)
	if err != nil {
		return nil
	}
	return k
}

// encrypt — Node encryptSecret: enc:v1:<iv12>:<tag>:<ct> (base64).
func (c *lCrypto) encrypt(plain string) string {
	if plain == "" {
		return plain
	}
	k := c.key()
	if k == nil {
		return plain
	}
	aead, err := aesGCM(k)
	if err != nil {
		return plain
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return plain
	}
	sealed := aead.Seal(nil, iv, []byte(plain), nil)
	if err != nil {
		return plain
	}
	// Go Seal appends the tag last; Node order is iv:tag:ct — re-emit.
	ct := sealed[:len(sealed)-16]
	tag := sealed[len(sealed)-16:]
	return lEncPrefix + b64e(iv) + ":" + b64e(tag) + ":" + b64e(ct)
}

// storedToPlaintext — Node parity: no prefix -> as-is; prefix -> decrypt
// (any failure -> "").
func (c *lCrypto) storedToPlaintext(stored string) string {
	if stored == "" {
		return stored
	}
	if !strings.HasPrefix(stored, lEncPrefix) {
		return stored
	}
	k := c.key()
	if k == nil {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(stored, lEncPrefix), ":")
	if len(parts) != 3 {
		return ""
	}
	iv, err1 := b64d(parts[0])
	tag, err2 := b64d(parts[1])
	ct, err3 := b64d(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || len(iv) != 12 || len(tag) != 16 {
		return ""
	}
	aead, err := aesGCM(k)
	if err != nil {
		return ""
	}
	// Go Open expects ciphertext||tag.
	plain, err := aead.Open(nil, iv, append(append([]byte{}, ct...), tag...), nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

// normalizeStored — already-encrypted as-is; plaintext -> encrypt now.
func (c *lCrypto) normalizeStored(v string) string {
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, lEncPrefix) {
		return v
	}
	return c.encrypt(v)
}

// lEnv — env lookup (test-overridable).
var lEnvOverride map[string]string

// SetEnv — test hook for deterministic env.
func SetEnv(m map[string]string) { lEnvOverride = m }

func lEnv(name string) string {
	if lEnvOverride != nil {
		if v, ok := lEnvOverride[name]; ok {
			return v
		}
		return ""
	}
	return lEnvGet(name)
}

var modelRefRe = regexp.MustCompile(`^[A-Za-z0-9._\-/]+(:[A-Za-z0-9._\-/]+){0,2}$`)
