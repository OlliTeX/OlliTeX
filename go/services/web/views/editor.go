package views

// P5.1a — editor page (GET /editor/:id and legacy GET /Project/:id).
//
// Node oracle (ProjectController.loadEditor + the pug chain
// layout-base → layout-react → ide-react + editor/_meta.pug): the page is a
// single 35 kB HTML line whose per-request surface is (a) the per-request
// CSP nonce, (b) the session csrf token, (c) the project-name title, and
// (d) 68 <meta name="ol-…"> slots. Everything else (assets, cookie banner,
// loading screen, translations, feature config) is build-static and is
// embedded verbatim in editorTemplate.
//
// Slot rendering rules (byte-verified against the Node oracle):
//
//	<n> data-type="boolean" content  ← boolean TRUE
//	<n> data-type="boolean"         ← boolean false/undefined
//	<n> data-type="string|number" content="v" ← present value
//	<n> data-type="string|number"   ← undefined value
//	<n> content="v"                 ← raw (no data-type)
//	<n> data-type="json" content="{…}" ← JSON slots (always present)

import "strings"

const (
	edSlotBeg = "\x01EV:"
	edSlotEnd = "\x02"
)

// EditorData carries all per-request editor-page values (P5.1a).
//
// JSON slots: name → compact JSON string (HTML-escaped on render; must be
// valid JSON — the reader is a browser JSON.parse).
// Raw slots: name → string value.
// Bool slots: name → true renders the bare `content` attribute, false drops
// it (Node/pug semantics).
// Typed slots (string/number): name → value; empty/absent → no content attr.
type EditorData struct {
	Nonce       string
	CSRF        string
	Title       string // project name + " - OlliTeX, Online LaTeX Editor"
	ProjectName string // bare project name (twitter:title / og:title / ol-projectName)
	Origin      string // "http://host[:port]" (alternate link + siteUrl context)
	CurrentURL  string // "/editor/:id" or "/Project/:id" + optional "/detacher"|"/detached"
	Detached    bool   // true → the ide-react-detached chrome (GET .../detached)
	JSON        map[string]string
	Raw         map[string]string
	Bool        map[string]bool
	Typed       map[string]string
}

func edHTMLEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	).Replace(s)
}

// EditorPage renders the editor page with byte parity to the Node oracle.
// When d.Detached is true it renders the ide-react-detached chrome
// (GET .../detached); otherwise the main editor chrome (main + /detacher),
// which is identical except ol-detachRole + currentUrl (both slotted).
func EditorPage(d EditorData) string {
	s := editorTemplate
	if d.Detached {
		s = detachedTemplate
	}
	s = strings.ReplaceAll(s, "__NONCE__", d.Nonce)
	s = strings.ReplaceAll(s, "__PROJNAME__", edHTMLEscape(d.ProjectName))
	s = strings.ReplaceAll(s, "__TITLE__", edHTMLEscape(d.Title))
	s = strings.ReplaceAll(s, "__ORIGIN__", d.Origin)
	s = strings.ReplaceAll(s, "__CURRENTURL__", edHTMLEscape(d.CurrentURL))
	for {
		i := strings.Index(s, edSlotBeg)
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], edSlotEnd)
		if j < 0 {
			break
		}
		inner := s[i+len(edSlotBeg) : i+j] // "ol-…:type"
		k := strings.LastIndexByte(inner, ':')
		name, dtype := inner[:k], inner[k+1:]
		end := i + j + len(edSlotEnd)
		var out string
		switch dtype {
		case "raw":
			if v, ok := d.Raw[name]; ok && v != "" {
				out = `<meta name="` + name + `" content="` + edHTMLEscape(v) + `">`
			} else {
				out = `<meta name="` + name + `">`
			}
		case "json":
			if v, ok := d.JSON[name]; ok {
				out = `<meta name="` + name + `" data-type="json" content="` + edHTMLEscape(v) + `">`
			} else {
				// Node omits content when the value is undefined (e.g. ol-brandVariation)
				out = `<meta name="` + name + `" data-type="json">`
			}
		case "boolean":
			if d.Bool[name] {
				out = `<meta name="` + name + `" data-type="boolean" content>`
			} else {
				out = `<meta name="` + name + `" data-type="boolean">`
			}
		default: // string | number
			if v, ok := d.Typed[name]; ok && v != "" {
				out = `<meta name="` + name + `" data-type="` + dtype + `" content="` + edHTMLEscape(v) + `">`
			} else {
				out = `<meta name="` + name + `" data-type="` + dtype + `">`
			}
		}
		s = s[:i] + out + s[end:]
	}
	return s
}
