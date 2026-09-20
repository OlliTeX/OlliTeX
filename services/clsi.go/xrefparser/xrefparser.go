// Package xrefparser ports services/clsi/app/js/XrefParser.js.
//
// Node parity:
//
//   - xref path is filePath + "xref" (verbatim string concat, so
//     "output.pdf" -> "output.pdfxref").
//   - stat: not a regular file -> "xref file invalid type";
//     size == 0 -> "xref file empty"; size > 1 MiB -> "xref file too large".
//   - read the file; regex
//     /^\d{1,9}\/\d{1,9}: uncompressed; offset = (\d{1,9})$/gm (multiline).
//   - The list is seeded with {offset: 0} BEFORE the matched entries.
//   - If only the seed is present -> "xref file has no objects".
//   - Error wrap: NoXrefTableError is rethrown as-is; fs errors with an
//     "err.code" (Go: *os.PathError) are wrapped as
//     "xref file error <code>"; everything else is
//     "xref file parse error".
//
// Port note: Node uses fs.readFile with flag O_RDONLY|O_NOFOLLOW and
// encoding 'ascii'; Go's os.ReadFile is byte-transparent and the caller
// passes a plain path that is on local disk.
package xrefparser

import (
	"os"
	"regexp"
	"strconv"
)

const MAX_XREF_FILE_SIZE = 1024 * 1024

var xrefRe = regexp.MustCompile(`^\d{1,9}/\d{1,9}: uncompressed; offset = (\d{1,9})$`)

// XrefEntry is one parsed "N/M: uncompressed; offset = K" row.
type XrefEntry struct {
	Offset       int
	Uncompressed bool
}

type noXrefError struct{ msg string }

func (e noXrefError) Error() string { return e.msg }

// wrapXrefError mirrors Node's catch block: NoXrefTableError is rethrown;
// errors with a code (Node: err.code, Go: *os.PathError) are wrapped as
// "xref file error <code>"; everything else is "xref file parse error".
func wrapXrefError(err error) error {
	if _, ok := err.(noXrefError); ok {
		return err
	}
	if pe, ok := err.(*os.PathError); ok {
		return noXrefError{"xref file error " + pe.Err.Error()}
	}
	return noXrefError{"xref file parse error"}
}

// ParseXrefTable ports parseXrefTable(filePath, pdfFileSize).
func ParseXrefTable(filePath string, pdfFileSize int64) (entries []XrefEntry, err error) {
	defer func() {
		if err != nil {
			err = wrapXrefError(err)
		}
	}()

	xrefPath := filePath + "xref"
	st, err := os.Stat(xrefPath)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, noXrefError{"xref file invalid type"}
	}
	if st.Size() == 0 {
		return nil, noXrefError{"xref file empty"}
	}
	if st.Size() > MAX_XREF_FILE_SIZE {
		return nil, noXrefError{"xref file too large"}
	}

	content, err := os.ReadFile(xrefPath)
	if err != nil {
		return nil, err
	}

	// Seed {offset: 0} BEFORE matches (JS: "include a zero-index object for
	// backwards compatibility with our existing xref table parsing code").
	out := []XrefEntry{{Offset: 0}}

	// JS matchAll + $ with m flag: every line that fully matches
	// "<id>/<gen>: uncompressed; offset = <n>". Go: match each line
	// individually (line-split preserves \r so anchored match behaves
	// identically to JS).
	for _, line := range splitLines(content) {
		m := xrefRe.FindStringSubmatch(string(line))
		if m == nil {
			continue
		}
		off, _ := strconv.Atoi(m[1])
		out = append(out, XrefEntry{Offset: off, Uncompressed: true})
	}

	if len(out) == 1 {
		return nil, noXrefError{"xref file has no objects"}
	}
	return out, nil
}

// splitLines splits on \n only (JS $ anchors: \r does not end a line).
func splitLines(content []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			lines = append(lines, content[start:i])
			start = i + 1
		}
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}
	return lines
}

// IsNoXrefError reports whether err originates from the no-xref path
// (unexported type; exported helper for cross-package mapping, e.g. CCM).
func IsNoXrefError(err error) bool {
	_, ok := err.(noXrefError)
	return ok
}
