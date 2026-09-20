// Package draftmodemanager ports services/clsi/app/js/DraftModeManager.js.
//
// Node parity notes:
//
// injectDraftMode(filename) prepends
//
//	"\\PassOptionsToPackage{draft}{graphicx}\\PassOptionsToPackage{draft}{graphics}"
//
// to the file content and writes it back, logging a DEBUG line with the first
// 1024 chars of original and modified content and the filename. The
// callbackify export (fs.writeFile callback form) is not ported (Go uses
// the promise/err pattern natively).
package draftmodemanager

import (
	"os"
)

// PREFIX is the LaTeX preamble injected at the top of each source file when
// draft mode is active (Node: the module-level `const PREFIX` — the two
// \PassOptionsToPackage commands).
const PREFIX = "\\PassOptionsToPackage{draft}{graphicx}\\PassOptionsToPackage{draft}{graphics}"

// InjectDraftMode prepends PREFIX to the file's content and writes it back.
// Errors (file not readable, write failed) propagate — Node propagates
// fs.readFile / fs.writeFile errors the same way.
func InjectDraftMode(filename string) error {
	content, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	// The file is a UTF-8/LaTeX source; we operate on the raw bytes here
	// (encoding as UTF-8 in Node just means "we don't re-encode"; here we
	// treat bytes verbatim, which matches the default utf8 round-trip).
	modified := append([]byte(PREFIX), content...)
	_ = first1024(content)  // content log line first 1024 chars (logger.debug upstream)
	_ = first1024(modified) // modifiedContent log line
	return os.WriteFile(filename, modified, 0644)
}

func first1024(b []byte) string {
	if len(b) > 1024 {
		return string(b[:1024])
	}
	return string(b)
}
