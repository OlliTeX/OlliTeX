package views

import "strings"

// Slot model shared by all React page shells (editor P5.1a, hub P6.1):
// the static Node-captured HTML (…_template.go) with per-request values
// slotted as \x01EV:<meta-name>:<data-type>\x02.
//
// Slot rendering rules (byte-verified against the Node oracles):
//
//	<n> data-type="boolean" content  ← boolean TRUE
//	<n> data-type="boolean"         ← boolean false/undefined
//	<n> content="v"                 ← raw (no data-type)
//	<n> data-type="json" content="{…}" ← JSON slots
//	<n> data-type="json"            ← JSON value undefined (Node omits content)
//	<n> data-type="string|number" content="v" ← typed slot with value
//	<n> data-type="string|number"   ← typed slot without value

// PageSlots carries the per-request slot values for a shared template.
type PageSlots struct {
	Nonce       string
	CSRF        string
	Title       string
	ProjectName string // bare name slot (twitter:title / og:title)
	Origin      string
	CurrentURL  string
	JSON        map[string]string
	Raw         map[string]string
	Bool        map[string]bool
	Typed       map[string]string
}

// ApplyPageSlots renders a slotted page template (…_template.go) into final
// HTML. EditorPage forwards here (single implementation).
func ApplyPageSlots(template string, d PageSlots) string {
	s := template
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
