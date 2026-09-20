package library

import (
	"strings"
)

// ---------- LibrarySerializer.mjs (1:1 port) ----------
//
// Escape rule: a `}` not part of a balanced {…} pair gets a backslash
// (a}b → a\}b). Balanced nested braces are preserved; already-escaped
// \{ / \} sequences pass through (i++ skips the pair) and do NOT change
// the brace depth.

// escapeBibValue — port of escapeBibValue.
func escapeBibValue(value string) string {
	out := make([]byte, 0, len(value)+4)
	depth := 0
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == '\\' && i+1 < len(value) && (value[i+1] == '{' || value[i+1] == '}') {
			out = append(out, '\\', value[i+1])
			i++
			continue
		}
		switch c {
		case '{':
			depth++
			out = append(out, c)
		case '}':
			if depth == 0 {
				out = append(out, '\\', '}')
				continue
			}
			depth--
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

// serializeBibEntry — Node serializeBibEntry:
//
//	fields in given order; empty values dropped; key present →
//	  lines ? `@type{key,\n<lines joined ,\n>\n}` : `@type{key}\n`
//	key absent → lines ? `@type{\n<lines>\n}` : `@type{}\n`  (unused for
//	our API — keys are required — but kept for parity completeness)
func serializeBibEntry(key, typ string, fields []fieldOut) string {
	lines := []string{}
	for _, f := range fields {
		name := strings.TrimSpace(f.Name)
		value := strings.TrimSpace(f.Value)
		if name != "" && value != "" {
			lines = append(lines, "  "+name+" = {"+escapeBibValue(value)+"}")
		}
	}
	linesText := strings.Join(lines, ",\n")
	if key != "" {
		if linesText != "" {
			return "@" + typ + "{" + key + ",\n" + linesText + "\n}"
		}
		return "@" + typ + "{" + key + "}\n"
	}
	if linesText != "" {
		return "@" + typ + "{\n" + linesText + "\n}"
	}
	return "@" + typ + "{}\n"
}

// serializeBibFile — trailing newline per entry, entries joined by one
// blank line; empty list → "".
func serializeBibFile(entries []bibEntry) string {
	blocks := []string{}
	for _, e := range entries {
		text := serializeBibEntry(strings.TrimSpace(e.Key), e.Type, e.Fields)
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		blocks = append(blocks, text)
	}
	if len(blocks) == 0 {
		return ""
	}
	return strings.Join(blocks, "\n")
}

// bibEntry — an in-memory entry for serialization (key/type/fields).
type bibEntry struct {
	Key    string
	Type   string
	Fields []fieldOut
}
