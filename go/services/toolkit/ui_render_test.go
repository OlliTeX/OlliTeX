package toolkit

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestUI_RenderAllScreens — the classic console layout renders for every
// screen at a representative size (the look-and-feel contract): title ·
// menu bar · two bordered panes · status line · keystrip; plus the mc
// prompt boxes (confirm + prompt) and the open menu dropdown.
func TestUI_RenderAllScreens(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)
	a.width, a.height = 110, 32

	for _, screen := range []string{"dashboard", "stack", "shells", "settings", "actions", "doctor", "backup", "about"} {
		a.screen = screen
		v := a.View()
		if !strings.Contains(v, "OlliTeX Toolkit") {
			t.Errorf("%s: title bar missing:\n%s", screen, v)
		}
		if !strings.Contains(v, "S C R E E N S") {
			t.Errorf("%s: the master pane (the left list) missing", screen)
		}
		if !strings.Contains(v, "File") || !strings.Contains(v, "Help") {
			t.Errorf("%s: the menu bar missing", screen)
		}
		if !strings.Contains(v, "[j/k] move") {
			t.Errorf("%s: the keystrip (the bracketed-key row) missing", screen)
		}
	}

	// the stop-confirm box (the overlay compositor + the [y]/[n] buttons)
	a.screen = "stack"
	a.askStop()
	v := a.View()
	if !strings.Contains(v, "STOP THE STACK") || !strings.Contains(v, "[y] yes") || !strings.Contains(v, "[n] no") {
		t.Errorf("the stop-confirm box did not render its title + y/n buttons:\n%s", v)
	}
	if i := strings.Index(v, "STOP THE STACK"); i < 0 || !strings.Contains(v[:i], "Stack") {
		t.Errorf("the confirm box must render OVER the panes (the background survives)")
	}
	a.dlg = nil

	// the prompt box (the typed-input field)
	a.dialogPrompt("ngram", "n-gram models", "languages (en,de — empty = all five):")
	a.dlg.value = "en,de"
	v = a.View()
	if !strings.Contains(v, "en,de") || !strings.Contains(v, "[enter]") {
		t.Errorf("the prompt box did not render the input line + [enter] button:\n%s", v)
	}
	a.dlg = nil

	// the open menu (the menubar dropdown, driven by a real key)
	a.menuActive(true)
	next, _ := a.menu.Update(tea.KeyMsg{Type: tea.KeyEnter})
	a.menu = next
	v = a.View()
	if !strings.Contains(v, "Stack panel") || !strings.Contains(v, "Quit the toolkit") {
		t.Errorf("the open menu dropdown did not render its items:\n%s", v)
	}

	// the zone markers must never leak into the rendered output (the Scan
	// pass strips them; a leak is visible garbage in the real terminal)
	for _, s := range []string{a.View()} {
		if strings.Contains(s, "z\x1b") || strings.Contains(s, "z [") {
			t.Errorf("zone markers leaked into the rendered output:\n%s", s)
		}
	}
}
