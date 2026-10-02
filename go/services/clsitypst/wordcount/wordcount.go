// Package wordcount ports services/clsi_typst/app/js/WordcountInjector.js
// (112L) — the wordometer-style word-count injector (plan §3.6, F3.1).
//
// clsi runs `texcount` in docker; Typst has no texcount, so clsi_typst does a
// *wordometer-style* compile (the texlyre approach, ported per plan §3.6):
// the root resource is imported by an injected driver document
// (`__clsi_wc_main.typ`) that imports the vendored wordometer under a
// private alias `wo` and renders the counts on a final, marker-only page.
// The injected artifacts are `__clsi_wc_*` files that are removed after the
// count; the user's resources are never mutated.
//
// D5: the rendered-marker TEXT read (app/js/ClsiWordText.js — pdfjs-dist,
// last-pages window) is the fixture-picked Go PDF text engine (gopdf vs
// rsc.io/pdf); it is NOT in this package (no external deps here — the
// engine is wired into the compilemanager's ReadPdfMarker seam at T7 final).
package wordcount

import (
	_ "embed"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	clsl "ollitex/go/services/clsitypst/logger"
)

//go:embed wordometer.typ
var wordometerTyp string

// Artifact names (Node WordcountInjector WC_MAIN/WC_WORDOMETER/WC_OUT_PDF).
const (
	WCMain       = "__clsi_wc_main.typ"
	WCWordometer = "__clsi_wordometer.typ"
	WCOutPDF     = "__clsi_wc_out.pdf"
)

// markerRe ports WORDOMETER_MARKER (whitespace is tolerant: pdfjs splits the
// rendered text into fragments, so spaces between tokens are not stable).
var markerRe = regexp.MustCompile(
	`TOTAL_WORDS:\s*(\d+)\s*HEADING_WORDS:\s*(\d+)\s*NUM_HEADINGS:\s*(\d+)`)

// ParseMarkerText matches the rendered marker over rendered PDF text (the
// text the (D5) engine extracts per page; pages are concatenated with
// spaces, mirroring pdfjs `items.map(i => i.str).join(' ')`).
// Returns (totalWords, headingWords, numHeadings, ok).
func ParseMarkerText(text string) (int, int, int, bool) {
	m := markerRe.FindStringSubmatch(text)
	if m == nil {
		return 0, 0, 0, false
	}
	total, _ := strconv.Atoi(m[1])
	heading, _ := strconv.Atoi(m[2])
	heads, _ := strconv.Atoi(m[3])
	return total, heading, heads, true
}

// typEscape ports _typEscape (backslash + double-quote).
func typEscape(s string) string {
	out := make([]byte, 0, len(s)+8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			out = append(out, '\\', '\\')
		case '"':
			out = append(out, '\\', '"')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

// buildInjectedDoc ports buildInjectedDoc: the injected driver document.
// The alias `wo` is private to this document (a sibling `#include` of the
// user file), so user-level `word-count` etc. cannot collide;
// `#show: wo.word-count` propagates through `#include`/`#import`.
func buildInjectedDoc(rootResourcePath string) string {
	return `#import "` + typEscape(WCWordometer) + `" as wo
#show: wo.word-count

#include "` + typEscape(rootResourcePath) + `"

#pagebreak()
#context {
  let heads = query(heading)
  let headWords = 0
  for h in heads { headWords += wo.word-count-of(h.body).words }
  [WORDOMETER_OUTPUT_START TOTAL_WORDS: #wo.total-words HEADING_WORDS: #headWords NUM_HEADINGS: #heads.len() WORDOMETER_OUTPUT_END]
}
`
}

// writeFile ports _writeFile: the ENOENT-tolerant mkdir-then-write
// (Node: catch ENOENT -> mkdir recursive -> re-write; any other error
// rethrows). Go fs.WriteFile already fails on a missing parent, so the
// mkdir is unconditional (idempotent — parity of the happy path).
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// Inject ports injectWordometer: write the vendored wordometer + the
// injected driver next to the project files in `compileDir`.
func Inject(compileDir, rootResourcePath string) error {
	if err := writeFile(
		filepath.Join(compileDir, WCWordometer), wordometerTyp); err != nil {
		return err
	}
	if err := writeFile(
		filepath.Join(compileDir, WCMain), buildInjectedDoc(rootResourcePath)); err != nil {
		return err
	}
	clsl.Debug(map[string]any{"compileDir": compileDir},
		"injected wordometer artifacts")
	return nil
}

// Remove ports removeArtifacts: fs.rm({force: true}) each artifact — a
// missing file is fine; any other error propagates.
func Remove(compileDir string) error {
	for _, name := range []string{WCMain, WCWordometer, WCOutPDF} {
		err := os.Remove(filepath.Join(compileDir, name))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
