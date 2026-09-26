package templates

import (
	"regexp"
	"strings"
)

// mdToHtml — the CommonMark-lite subset of marked.parse (gfm:false,
// breaks:false, html renderer stripped) that the template-gallery flows
// feed into cleanHtml. The fixture corpus is plain text / simple inline
// markup; the subset covers:
//
//   blocks  : paragraphs (blank-line separated), no lists/tables/fences
//   inline  : **bold** → <strong>  *italic* → <em>  `code` → <code>
//             [text](href) → <a href="…">  raw HTML tags → stripped
//
// The output then goes through cleanHtml's option-specific keep/drop, so
// only the well-formed tag shapes matter for parity.

var mdReStrong = regexp.MustCompile(`\*\*([^*]+)\*\*`)
var mdReItalic = regexp.MustCompile(`\*([^*\n]+)\*`)
var mdReCode = regexp.MustCompile("`([^`\n]+)`")
var mdReLink = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)\)`)

// mdStripInline — ordered inline substitutions (links → code → strong →
// italic). Escape values are re-serialised by the later cleanHtml pass;
// keep the hrefs as-is for the fixture domain.
func mdInline(s string) string {
	s = mdReLink.ReplaceAllString(s, `<a href="$2">$1</a>`)
	s = mdReCode.ReplaceAllString(s, "<code>$1</code>")
	s = mdReStrong.ReplaceAllString(s, "<strong>$1</strong>")
	s = mdReItalic.ReplaceAllString(s, "<em>$1</em>")
	return s
}

var reHTMLTag = regexp.MustCompile(`<[^>\n]+>`)

// mdToHtml — marked.parse equivalent for this subset:
//
//	""                  → ""
//	"text"              → "<p>text</p>\n"
//	"a\n\nb"            → "<p>a</p>\n<p>b</p>\n"
//	raw HTML tags       → stripped before the pass (marked html→'')
func mdToHtml(md string) string {
	if md == "" {
		return ""
	}
	// The marked CustomRenderer drops raw HTML blocks/inline tags.
	md = reHTMLTag.ReplaceAllString(md, "")
	if strings.TrimSpace(md) == "" {
		return ""
	}
	paras := strings.Split(md, "\n\n")
	out := make([]string, 0, len(paras))
	for _, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// single newlines inside a paragraph are literal (breaks:false:
		// they stay as whitespace, not <br>)
		p = strings.Join(strings.Fields(p), " ")
		out = append(out, "<p>"+mdInline(p)+"</p>")
	}
	return strings.Join(out, "\n") + "\n"
}
