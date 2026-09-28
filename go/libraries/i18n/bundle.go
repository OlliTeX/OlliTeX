package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ni18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// Placeholder convention: catalog strings may contain the renderer's
// {{var}} placeholders (the e-mail interpolator). nicksnyder's core
// treats {{...}} as Go-template functions and would either error or
// rewrite them (empirically: `function "app" not defined`), so every
// message gets sentinel delims — the bytes pass through untouched and
// the renderer's interpolator owns substitution (1:1 with the English
// pipeline). Documented in extract.md.
const (
	leftDelim  = "\x00"
	rightDelim = "\x01"
)

// NewBundleFromJSONDir — load <dir>/<locale>.json catalogs.
// Shape per catalog: {"key": "text"} for plain strings, or
// {"key": {"zero": "...", "one": "...", "other": "..."}} for CLDR
// plural messages (en.json defines the key space). nicksyder core is
// CLDR forms (Zero/One/Two/Few/Many/Other), not ICU — plural pin
// covered in i18n_test.go.
func NewBundleFromJSONDir(dir string) (*Bundle, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(n, ".json"))
	}
	if len(names) == 0 {
		return nil, os.ErrNotExist
	}
	sort.Strings(names)
	def := names[0]
	for _, n := range names {
		if n == "en" {
			def = "en"
			break
		}
	}
	b := ni18n.NewBundle(language.Make(def))
	locales := map[string]bool{}
	for _, n := range names {
		path := filepath.Join(dir, n+".json")
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, rerr
		}
		shape := map[string]json.RawMessage{}
		if jerr := json.Unmarshal(raw, &shape); jerr != nil {
			return nil, jerr
		}
		msgs := make([]*ni18n.Message, 0, len(shape))
		for k, v := range shape {
			m := &ni18n.Message{
				ID:         k,
				LeftDelim:  leftDelim,
				RightDelim: rightDelim,
			}
			var txt string
			if jerr := json.Unmarshal(v, &txt); jerr == nil {
				// plain string → all plural forms = same text
				m.Zero, m.One, m.Two = txt, txt, txt
				m.Few, m.Many, m.Other = txt, txt, txt
				msgs = append(msgs, m)
				continue
			}
			forms := map[string]string{}
			if jerr := json.Unmarshal(v, &forms); jerr != nil {
				return nil, jerr
			}
			m.Zero = forms["zero"]
			m.One = forms["one"]
			m.Two = forms["two"]
			m.Few = forms["few"]
			m.Many = forms["many"]
			m.Other = forms["other"]
			msgs = append(msgs, m)
		}
		if aerr := b.AddMessages(language.Make(n), msgs...); aerr != nil {
			return nil, aerr
		}
		locales[n] = true
	}
	return &Bundle{Default: def, Locales: locales, b: b}, nil
}
