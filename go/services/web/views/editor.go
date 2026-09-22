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
	InitTheme   string // loading-screen-init-<v> (Node getInitialTheme; default "system")
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
	t := editorTemplate
	if d.Detached {
		t = detachedTemplate
	}
	return ApplyPageSlots(t, PageSlots{
		Nonce:       d.Nonce,
		CSRF:        d.CSRF,
		Title:       d.Title,
		ProjectName: d.ProjectName,
		Origin:      d.Origin,
		CurrentURL:  d.CurrentURL,
		InitTheme:   d.InitTheme,
		JSON:        d.JSON,
		Raw:         d.Raw,
		Bool:        d.Bool,
		Typed:       d.Typed,
	})
}

// HubData — the /hub page per-request values (P6.1).
type HubData struct {
	Nonce      string
	CSRF       string
	Origin     string // site URL (alternate link)
	CurrentURL string // requested path — Node canonical is path-aware (U9)
	HubAdmin   bool
	GitBridge  bool
	JSON       map[string]string
	Raw        map[string]string
}

// HubPage renders /hub (views/hub_template.go + slots).
func HubPage(d HubData) string {
	t := hubTemplate
	cu := d.CurrentURL
	if cu == "" {
		cu = "/hub"
	}
	return ApplyPageSlots(t, PageSlots{
		Nonce:       d.Nonce,
		CSRF:        d.CSRF,
		Title:       "OlliTeX Hub - OlliTeX, Online LaTeX Editor",
		ProjectName: "OlliTeX Hub",
		Origin:      d.Origin,
		CurrentURL:  cu,
		JSON:        d.JSON,
		Raw:         d.Raw,
		Bool: map[string]bool{
			"ol-hub-admin":        d.HubAdmin,
			"ol-gitBridgeEnabled": d.GitBridge,
		},
	})
}
