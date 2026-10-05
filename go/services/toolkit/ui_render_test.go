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

// TestUI_EscZeroMenuFallback — the terminal-friendly menu fallbacks
// (owner: F10 does not arrive in their terminal; mc users use esc+0):
// ESC then 0 opens the menu, F9 is an alias, and a LONE esc still gets its
// back/close/quit semantics once the 300ms window expires.
func TestUI_EscZeroMenuFallback(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)
	a.width, a.height = 110, 32

	// ESC enters the pending-fallback window (it does NOT act immediately).
	next, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	b, ok := next.(*app)
	if !ok {
		t.Fatalf("esc returned a non-*app model")
	}
	if !b.escPending {
		t.Fatalf("esc must arm the esc+0 fallback window")
	}
	if cmd == nil {
		t.Fatalf("the pending esc needs its timeout cmd (lone-esc resolution)")
	}

	// ... and 0 within the window opens the menu bar.
	next, _ = b.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	c, _ := next.(*app)
	if !c.menuOpen {
		t.Fatalf("esc then 0 must open the menu bar (the mc fallback)")
	}

	// F9 works too (safe alias for terminals whose F10 never arrives).
	if c.menuOpen {
		c.menuActive(false)
	}
	next, _ = c.Update(tea.KeyMsg{Type: tea.KeyF9})
	d, _ := next.(*app)
	if !d.menuOpen {
		t.Fatalf("F9 must open the menu bar (alias fallback)")
	}
	d.menuActive(false)

	// LONE esc (window expired) keeps its semantics: from a screen it goes
	// back to the dashboard; from the dashboard with a downed stack it quits.
	d.screen = "logs"
	next, _ = d.Update(tea.KeyMsg{Type: tea.KeyEsc})
	e, _ := next.(*app)
	if !e.escPending {
		t.Fatalf("lone esc must arm the window before acting")
	}
	next, _ = e.Update(escTimeoutMsg{})
	f, _ := next.(*app)
	if f.screen != "dashboard" {
		t.Fatalf("lone esc from logs must land on the dashboard, got %q", f.screen)
	}

	// a double ESC must not re-pend forever (the second ESC is consumed).
	f.screen = "stack"
	next, _ = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	g, _ := next.(*app)
	if !g.escPending {
		t.Fatalf("esc must arm the window")
	}
	next, _ = g.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if h, _ := next.(*app); h.escPending {
		t.Fatalf("a second ESC must be consumed (no re-pend loop)")
	}
}

// TestUI_PaneDiscipline — on a tall terminal the panes stay compact
// (owner: the full-height padded panes read as one broken box): the LOGS
// box is capped, and the whole frame stays far below the terminal height.
func TestUI_PaneDiscipline(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)
	a.width, a.height = 100, 48 // the owner-class tall terminal
	a.screen = "logs"
	a.dcur = 3
	v := a.View()
	lines := strings.Split(strings.TrimRight(v, "\n"), "\n")

	// total frame height: title + menubar + max(panes) + status + keystrip
	if len(lines) > 30 {
		t.Fatalf("frame is %d rows on a 48-row terminal — the panes are still\nfull-height walls (they must be compact):\n%s", len(lines), v)
	}

	// the LOGS box itself is capped (~20 content rows + borders).
	top, bot := -1, -1
	for i, l := range lines {
		if top < 0 && "LOGS" == strings.TrimSpace(strings.Trim(strings.SplitN(l, " ", 2)[0], " ")) {
		}
		if strings.Contains(l, "LOGS") && strings.Contains(l, "┌") {
			top = i
		}
	}
	if top < 0 {
		for i, l := range lines {
			if strings.Contains(l, "┌") && i+1 < len(lines) && strings.Contains(lines[i+1], "LOGS") {
				top = i
				break
			}
		}
	}
	if top >= 0 {
		for i := top + 1; i < len(lines); i++ {
			if strings.Contains(lines[i], "└") && strings.Contains(lines[i], "┘") {
				bot = i
				break
			}
		}
	}
	if top < 0 || bot < top {
		t.Fatalf("could not find the LOGS box boundaries:\n%s", v)
	}
	if h := bot - top + 1; h > 24 {
		t.Fatalf("LOGS box is %d rows — must stay compact (<=24), got:\n%s", h, v)
	}
}
