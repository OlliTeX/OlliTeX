package configschema

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// EmbeddedDefaults is the initial-setup seed file (JSONC) for the SQLite
// config DB — the operator's view of the registry defaults with per-key
// comments (see defaults.jsonc). It is the single seed source: the CLI
// (`configdb import-defaults` / `init`) and the toolkit consume it, so no
// copy in the toolkit needs to stay in sync.
//
//go:embed defaults.jsonc
var EmbeddedDefaults string

// ParseJSONC strips //, /* */, and trailing commas from a JSONC document and
// returns the equivalent JSON. String contents are untouched (a URL like
// "http://…" or a literal "/*" inside a string survives).
func ParseJSONC(src string) (string, error) {
	var out strings.Builder
	out.Grow(len(src))
	inStr := false
	escaped := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inStr {
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out.WriteByte(c)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			i-- // the loop's i++ re-advances past the newline (kept)
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			for i+2 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i += 1 // leave i on '*'
			if i+1 >= len(src) {
				return "", fmt.Errorf("jsonc: unterminated block comment")
			}
			i++ // skip '/'
		default:
			out.WriteByte(c)
		}
	}
	// Pass 2: drop commas whose next significant char closes an object/array.
	res := strings.Builder{}
	inStr = false
	escaped = false
	s := out.String()
	for j := 0; j < len(s); j++ {
		c := s[j]
		if inStr {
			res.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			res.WriteByte(c)
			continue
		}
		if c == ',' {
			k := j + 1
			for k < len(s) && (s[k] == ' ' || s[k] == '\n' || s[k] == '\t' || s[k] == '\r') {
				k++
			}
			if k < len(s) && (s[k] == '}' || s[k] == ']') {
				continue // trailing comma
			}
		}
		res.WriteByte(c)
	}
	return res.String(), nil
}

// Defaults parses the embedded defaults.jsonc into key -> JSON value.
// null entries (no static default) appear as nil.
func Defaults() (map[string]any, error) {
	jsonSrc, err := ParseJSONC(EmbeddedDefaults)
	if err != nil {
		return nil, err
	}
	return parseDefaultsJSON(jsonSrc)
}

// ParseDefaultsFile is the same for an operator-supplied seed file.
func ParseDefaultsFile(src string) (map[string]any, error) {
	jsonSrc, err := ParseJSONC(src)
	if err != nil {
		return nil, err
	}
	return parseDefaultsJSON(jsonSrc)
}

func parseDefaultsJSON(jsonSrc string) (map[string]any, error) {
	m := map[string]any{}
	if err := json.Unmarshal([]byte(jsonSrc), &m); err != nil {
		return nil, fmt.Errorf("configschema: parse defaults: %w", err)
	}
	return m, nil
}
