// Package sitesettings ports the admin "Manage Site" SiteSettings leaf
// (P3.6 flip unit) — the five admin-tools routes:
//
//	GET  /admin/site                     → 302 /hub#/site
//	GET  /admin/site-settings            → all sections (secrets masked) + counts
//	PUT  /admin/site-settings/:section   → replace one section (validated)
//	POST /admin/site-settings/email/test → one test mail via the stored E-mail section
//	GET  /admin/site/template-admins     → users with the template-admin grant
//
// Node ground truth (services/web/app/src/Features/SiteSettings/
// SiteSettingsManager.mjs + modules/admin-tools/app/src/SiteSettingsController.mjs
// + EmailTestController.mjs + StorageEnvFile.mjs + SecretCipher.mjs), every
// response pinned live against the running e2e stack (ss_*.json).
//
// The web response is BYTE-EXACT: Node serialises with JSON.stringify, which
// preserves key INSERTION ORDER (not sorted). Go's encoding/json sorts map
// keys, so this package builds ordered objects (Obj) and serialises them by
// hand (Encode) — the only way to match the Node wire exactly.
package sitesettings

import (
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Obj is an ordered JSON object (insertion-ordered key/value pairs) — the
// Go equivalent of a JS plain object that preserves key order.
type Obj []KV

// KV is one ordered key/value pair.
type KV struct {
	Key string
	Val any
}

// MergeOrdered implements JS {...seed, ...stored}: seed keys keep their
// order (stored value wins where present); stored-only keys are appended
// in stored order.
func MergeOrdered(seed, stored Obj) Obj {
	seen := make(map[string]bool, len(seed))
	out := make(Obj, 0, len(seed)+len(stored))
	if stored != nil {
		sv := make(map[string]any, len(stored))
		for _, kv := range stored {
			sv[kv.Key] = kv.Val
		}
		for _, kv := range seed {
			val := kv.Val
			if v, ok := sv[kv.Key]; ok {
				val = v
			}
			out = append(out, KV{kv.Key, val})
			seen[kv.Key] = true
		}
		for _, kv := range stored {
			if !seen[kv.Key] {
				out = append(out, kv)
			}
		}
	} else {
		out = append(out, seed...)
	}
	return out
}

// ObjGet returns the value for key (ok=false when absent).
func ObjGet(o Obj, key string) (any, bool) {
	for _, kv := range o {
		if kv.Key == key {
			return kv.Val, true
		}
	}
	return nil, false
}

// ObjSet inserts or updates key (existing position kept when present).
func ObjSet(o Obj, key string, val any) Obj {
	for i := range o {
		if o[i].Key == key {
			o[i].Val = val
			return o
		}
	}
	return append(o, KV{key, val})
}

// ObjKeys returns the key order.
func ObjKeys(o Obj) []string {
	keys := make([]string, 0, len(o))
	for _, kv := range o {
		keys = append(keys, kv.Key)
	}
	return keys
}

// ObjToMap converts an ordered Obj to a plain map (values shared; used for
// validation where order is irrelevant).
func ObjToMap(o Obj) map[string]any {
	m := make(map[string]any, len(o))
	for _, kv := range o {
		m[kv.Key] = kv.Val
	}
	return m
}


func asString(v any) string {
	s, _ := v.(string)
	return s
}

// jsString mimics JS JSON.stringify(str) — escapes only what JS escapes
// (" \ and control chars); it does NOT HTML-escape < > & and does NOT
// escape non-ASCII (UTF-8 bytes pass through), matching Node.
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
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
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

// Encode serialises an ordered JSON value (string/bool/nil/number/[]any/Obj)
// to the exact JSON.stringify byte form.
func Encode(v any) []byte {
	b := &strings.Builder{}
	writeVal(b, v)
	return []byte(b.String())
}

func writeVal(b *strings.Builder, v any) {
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
	case int32:
		b.WriteString(strconv.FormatInt(int64(t), 10))
	case int64:
		b.WriteString(strconv.FormatInt(t, 10))
	case float64:
		writeNumber(b, t)
	case float32:
		writeNumber(b, float64(t))
	case []any:
		writeArray(b, t)
	case []string:
		writeStringArr(b, t)
	case []int:
		writeIntArr(b, t)
	case Obj:
		writeObj(b, t)
	default:
		// unknown → encode via fmt (should not happen)
		b.WriteString(jsString(fmt.Sprint(t)))
	}
}

func writeNumber(b *strings.Builder, f float64) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		b.WriteString("null")
		return
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		b.WriteString(strconv.FormatInt(int64(f), 10))
		return
	}
	b.WriteString(strconv.FormatFloat(f, 'g', -1, 64))
}

func writeArray(b *strings.Builder, arr []any) {
	b.WriteByte('[')
	for i, v := range arr {
		if i > 0 {
			b.WriteByte(',')
		}
		writeVal(b, v)
	}
	b.WriteByte(']')
}

func writeStringArr(b *strings.Builder, arr []string) {
	b.WriteByte('[')
	for i, s := range arr {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(jsString(s))
	}
	b.WriteByte(']')
}

func writeIntArr(b *strings.Builder, arr []int) {
	b.WriteByte('[')
	for i, n := range arr {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(n))
	}
	b.WriteByte(']')
}

func writeObj(b *strings.Builder, o Obj) {
	b.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(jsString(kv.Key))
		b.WriteByte(':')
		writeVal(b, kv.Val)
	}
	b.WriteByte('}')
}

// hexEq — constant for cipher sanity (hex salt/iv length).
const cipherSaltLen = 16
const cipherIVLen = 16
const cipherKeyLen = 32

var _ = hex.EncodeToString
