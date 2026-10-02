// PDF marker-read engine (D5, fixture-picked 2026-09-29).
//
// D5 fixture result (recorded in HANDOFF §0 + §4 D5):
//   - winner: github.com/ledongthuc/pdf (the gopdf line; the old
//     github.com/ledongthuc/gopdf repo path is DEAD — the repo was renamed,
//     so the module path is .../pdf, pinned pseudo-version below).
//   - loser:  rsc.io/pdf v0.1.1 — returns raw glyph/CID byte sequences for
//     typst 0.15.1 subset-CID (Identity-H) fonts, so the rendered marker
//     can never be matched.
//
// The winner handles /ToUnicode CMaps + Identity-H (a deliberate fork of
// rsc.io/pdf; LICENSE = Go-style permissive "The Go Authors" license).
//
// Node parity (app/js/ClsiWordText.js pdfLastPagesMarkerText): scan the last
// lastPages=2 rendered pages; first page whose text matches the marker
// wins.
package wordcount

import (
	pdf "github.com/ledongthuc/pdf"
)

// lastPages mirrors the Node default (pdfLastPagesMarkerText(pdfPath, marker,
// 2)).
const lastPages = 2

// ReadMarker ports pdfLastPagesMarkerText: extract the rendered text of the
// last `lastPages` pages of the wordometer marker PDF and parse the
// TOTAL_WORDS/HEADING_WORDS/NUM_HEADINGS marker. ok=false when the PDF is
// unreadable or the marker is absent (the caller then falls back to wc -w,
// plan §9: degrade, not fail).
func ReadMarker(pdfPath string) (total, headingWords, numHeadings int, ok bool) {
	f, r, err := pdf.Open(pdfPath)
	if err != nil {
		return 0, 0, 0, false
	}
	defer f.Close()

	n := r.NumPage()
	start := n - lastPages + 1
	if start < 1 {
		start = 1
	}
	for i := start; i <= n; i++ {
		// nil map -> the page builds its own font cache (fork handles it).
		text, err := r.Page(i).GetPlainText(nil)
		if err != nil {
			continue
		}
		if total, heading, heads, ok := ParseMarkerText(text); ok {
			return total, heading, heads, true
		}
	}
	return 0, 0, 0, false
}
