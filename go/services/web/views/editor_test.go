package views

import (
	"strings"
	"testing"
)

// P5.1a/P5.1b — the editor page has two chromes (Node templates):
//   ide-react          (main + /detacher)  → editorTemplate
//   ide-react-detached (/detached)         → detachedTemplate
// The renderer MUST select the right one and must not leak the slotted
// per-request placeholders into the output.

func renderSample(detached bool) string {
	d := EditorData{
		Nonce:       "abc123nonce",
		ProjectName: "MyProj",
		Title:       "MyProj - OlliTeX, Online LaTeX Editor",
		Origin:      "http://h:1",
		CurrentURL:  "/editor/deadbeef",
		Detached:    detached,
		Raw:         map[string]string{"ol-csrfToken": "csrfval"},
		Bool:        map[string]bool{},
		Typed:       map[string]string{"ol-detachRole": ""},
		JSON:        map[string]string{},
	}
	if detached {
		d.Typed["ol-detachRole"] = "detached"
	}
	return EditorPage(d)
}

func TestEditorPageMainChrome(t *testing.T) {
	s := renderSample(false)
	for _, want := range []string{
		`/stylesheets/pages/ide-`,      // main CSS (NOT ide-detached)
		`id="ide-root"`,                // main body root + loading screen
		`content="MyProj"`,             // __PROJNAME__ resolved (twitter/og title)
		`>MyProj - OlliTeX, Online LaTeX Editor</title>`, // __TITLE__ resolved
		`<meta name="ol-csrfToken" content="csrfval">`,
		`<meta name="ol-detachRole" data-type="string">`, // empty → bare
	} {
		if !strings.Contains(s, want) {
			t.Errorf("main chrome missing %q", want)
		}
	}
	for _, bad := range []string{
		"ide-detached-",           // detached CSS must NOT appear
		"pdf-preview-detached-root", // detached root must NOT appear
		"__NONCE__", "__TITLE__", "__PROJNAME__", "__ORIGIN__", "__CURRENTURL__",
		"\x01EV:", // no slot markers may leak
	} {
		if strings.Contains(s, bad) {
			t.Errorf("main chrome leaked/mis-selected %q", bad)
		}
	}
	// Every nonce site resolved to the single per-page nonce.
	if got := strings.Count(s, `nonce="abc123nonce"`); got == 0 {
		t.Errorf("expected nonce attribute to be filled")
	}
}

func TestEditorPageDetachedChrome(t *testing.T) {
	s := renderSample(true)
	for _, want := range []string{
		`/stylesheets/ide-detached-`,       // detached CSS
		`id="pdf-preview-detached-root"`,   // detached body root
		`src="/js/ide-detached-`,           // detached entry chunk
		`content="MyProj"`,                 // __PROJNAME__ resolved
		`<meta name="ol-detachRole" data-type="string" content="detached">`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("detached chrome missing %q", want)
		}
	}
	for _, bad := range []string{
		`/stylesheets/pages/ide-`,  // main CSS must NOT appear
		`id="ide-root"`,            // main root must NOT appear
		"__NONCE__", "__TITLE__", "__PROJNAME__", "__ORIGIN__", "__CURRENTURL__",
		"\x01EV:",
	} {
		if strings.Contains(s, bad) {
			t.Errorf("detached chrome leaked/mis-selected %q", bad)
		}
	}
}
