// Package library ports the bib-editor module's Library surface (P6.5).
//
// Node sources (oracles /tmp/p65_oracle.mjs → /tmp/p65_node.json, 57 pins):
//
//	services/web/modules/bib-editor/app/src/BibTypes.mjs          → bibtypes.go
//	services/web/modules/bib-editor/app/src/LibrarySearch.mjs     → search.go
//	services/web/modules/bib-editor/app/src/LibrarySerializer.mjs → serialize.go
//	services/web/modules/bib-editor/app/src/LibraryManager.mjs    → manager.go
//	services/web/modules/bib-editor/app/src/LibraryController.mjs → controller.go
//	services/web/modules/bib-editor/app/src/LibraryRoutes.mjs     → features (routes)
//	services/web/modules/bib-editor/app/src/models/LibraryReference.mjs
//	(same file as above for view — module index.mjs gates on
//	 OVERLEAF_BIB_LIBRARY; ON in this deployment, so the flip is unconditional.)
//
// Routes (web profile; all session-gated by the core global chain —
// anonymous GET → 302 /login, anonymous non-GET → 403 "Forbidden"):
//
//	GET    /library                              → React shell page (libraryView=library)
//	GET    /library/trashed                      → React shell page (libraryView=trash)
//	GET    /library/references                   → {items, nextCursor}
//	POST   /library/references                   → 201 {items}
//	POST   /library/references/match             → {matches}
//	GET    /library/references/count             → {count}
//	GET    /library/references/download          → .bib text/plain attachment
//	GET    /library/references/citation-key-suggestions → {keys}
//	POST   /library/references/delete            → {deletedCount}
//	POST   /library/references/restore           → {restoredCount}
//	PATCH  /library/references/:key              → entry (declared LAST in Node)
//
// Collection: `libraryreferences` (mongo, user_id-scoped string ids; keys
// non-unique — SaaS-allowed duplicates; trash = trashedAt soft-delete with
// a 30-day retention sweep run by list + delete, per Node).
//
// Parity rules inherited: response JSON key order = Node's insertion order;
// lengths are JS UTF-16 code units (string.length); error messages are the
// controller's VALIDATION_MESSAGES table verbatim (incl. curly quotes).
package library

import (
	"regexp"
	"strings"
	"unicode/utf16"
)

// ---------- BibTypes.mjs (48-type vocabulary, pinned order) ----------

var BIB_TYPES = []string{
	"article", "artwork", "audio", "book", "bookinbook", "booklet",
	"commentary", "conference", "collection", "dataset", "electronic",
	"image", "inbook", "incollection", "inproceedings", "inreference",
	"jurisdiction", "legal", "legislation", "letter", "manual",
	"mastersthesis", "misc", "movie", "mvbook", "mvcollection",
	"mvproceedings", "mvreference", "music", "online", "patent",
	"performance", "periodical", "phdthesis", "proceedings", "reference",
	"report", "review", "software", "standard", "suppbook",
	"suppcollection", "suppperiodical", "techreport", "thesis",
	"unpublished", "video", "www",
}

var bibTypeSet = func() map[string]bool {
	m := make(map[string]bool, len(BIB_TYPES))
	for _, t := range BIB_TYPES {
		m[t] = true
	}
	return m
}()

// Node BibTypes.mjs:
//
//	CITATION_KEY_REGEX = /^[A-Za-z0-9._-]+$/        (letters/digits/. _ -)
//	FIELD_NAME_REGEX   = /^[a-z][a-z0-9-]*$/
//	MAX_ENTRY_COUNT=200, MAX_FIELDS_PER_ENTRY=200, MAX_FIELD_LENGTH=32768,
//	MAX_KEY_LENGTH=128, MAX_TYPE_LENGTH=64, MAX_FIELD_NAME_LENGTH=64
//
// Node measures lengths with string.length = UTF-16 code units.
var (
	citationKeyRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	fieldNameRe   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

const (
	maxEntryCount      = 200
	maxFieldsPerEntry  = 200
	maxFieldLength     = 32768
	maxKeyLength       = 128
	maxTypeLength      = 64
	maxFieldNameLength = 64
)

// jsLen — JS string.length semantics (UTF-16 code units), for the length
// gates pinned above.
func jsLen(s string) int { return len(utf16.Encode([]rune(s))) }

// ---------- validation (validateLibraryEntry / validateEntryBatch) ----------

// entryIn — one API entry as JSON-decoded (key/type/fields optional+typed).
type entryIn struct {
	Key    any    `json:"-"`
	Type   any    `json:"-"`
	Fields any    `json:"-"`
	Raw    string `json:"-"`
}

type vfail struct{ reason string }

func vf(s string) vfail { return vfail{s} }

// validateEntry — Node validateLibraryEntry. Returns ok or the first failing
// reason (reason string is stable; the controller maps it to the message).
func validateEntry(e map[string]any) vfail {
	if e == nil {
		return vf("entry-not-object")
	}
	key := ""
	if v, ok := e["key"].(string); ok {
		key = strings.TrimSpace(v)
	}
	if len(key) == 0 {
		return vf("key-missing")
	}
	if jsLen(key) > maxKeyLength {
		return vf("key-too-long")
	}
	if !citationKeyRe.MatchString(key) {
		return vf("key-invalid")
	}
	typ := ""
	if v, ok := e["type"].(string); ok {
		typ = strings.ToLower(strings.TrimSpace(v))
	}
	if len(typ) == 0 || len(typ) > maxTypeLength {
		return vf("type-invalid")
	}
	if !bibTypeSet[typ] {
		return vf("type-unknown")
	}
	if fv, has := e["fields"]; has && fv != nil {
		arr, ok := fv.([]any)
		if !ok {
			return vf("fields-not-array")
		}
		if len(arr) > maxFieldsPerEntry {
			return vf("fields-too-many")
		}
		for _, f := range arr {
			fm, ok := f.(map[string]any)
			if !ok {
				return vf("field-not-object")
			}
			name := ""
			if v, ok := fm["name"].(string); ok {
				name = strings.ToLower(strings.TrimSpace(v))
			}
			if len(name) == 0 || len(name) > maxFieldNameLength || !fieldNameRe.MatchString(name) {
				return vf("field-name-invalid")
			}
			val, hasV := fm["value"]
			if hasV && val != nil {
				s, ok := val.(string)
				if !ok {
					return vf("field-value-not-string")
				}
				if jsLen(s) > maxFieldLength {
					return vf("field-value-too-long")
				}
			}
		}
	}
	return vfail{"ok"}
}

// validateBatch — Node validateEntryBatch (entries array, 1..200, all valid;
// the FIRST failing entry's reason wins).
func validateBatch(entries any) string {
	arr, ok := entries.([]any)
	if !ok || len(arr) == 0 {
		return "entries-missing"
	}
	if len(arr) > maxEntryCount {
		return "entries-too-many"
	}
	for _, e := range arr {
		m, ok := e.(map[string]any)
		r := validateEntry(ifMap(m, ok))
		if r.reason != "ok" {
			return r.reason
		}
	}
	return "ok"
}

func ifMap(m map[string]any, ok bool) map[string]any {
	if ok {
		return m
	}
	return nil
}

// ---------- normalize (normalizeLibraryEntry, last-wins Map semantics) ----------

// normalizeEntry — Node: JS Map over raw fields (insertion order preserved;
// set updates in place; empty value DELETES the name — "empty removes"
// including emptying an earlier non-empty value). Names trimmed+lowercased
// and regex-gated; values trimmed (non-string → "").
func normalizeEntry(key, typ any, fields any) (nk string, ntype string, nfields []fieldOut) {
	nk = jsStringTrim(key)
	ntype = strings.ToLower(jsStringTrim(typ))
	out := []fieldOut{}
	idx := map[string]int{}
	farr, _ := fields.([]any)
	for _, f := range farr {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(jsString(fm["name"])))
		if name == "" || !fieldNameRe.MatchString(name) {
			continue
		}
		val := ""
		if v, ok := fm["value"].(string); ok {
			val = strings.TrimSpace(v)
		}
		if i, exists := idx[name]; exists {
			if val == "" {
				out = append(out[:i], out[i+1:]...)
				delete(idx, name)
			} else {
				out[i].Value = val
			}
			continue
		}
		if val == "" {
			// fresh empty: the map has no entry — and an explicit empty
			// value on a FRESH name does nothing (map.set never happened)
			continue
		}
		idx[name] = len(out)
		out = append(out, fieldOut{Name: name, Value: val})
	}
	return nk, ntype, out
}

func jsString(v any) string {
	s, _ := v.(string)
	return s
}

func jsStringTrim(v any) string { return strings.TrimSpace(jsString(v)) }

// fieldOut — storage + API shape ({name, value}).
type fieldOut struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
