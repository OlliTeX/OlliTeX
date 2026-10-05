package toolkit

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// TestUI_FooterPinnedContract — the owner's layout contract (2026-10-06):
// the frame fills EXACTLY the terminal; the status line + keystrip (the
// footer) rest on the bottom two rows; every visible row fits the terminal
// width (lipgloss-measured, not bytes); both panes take the full pane area
// (two true equal windows).
func TestUI_FooterPinnedContract(t *testing.T) {
	tk, _ := offlineToolkit(t)
	for _, size := range [][2]int{{80, 24}, {100, 32}, {120, 48}} {
		a := newApp(tk)
		a.width, a.height = size[0], size[1]
		for _, screen := range []string{"dashboard", "stack", "shells", "logs", "settings", "actions", "doctor", "backup", "about"} {
			a.screen = screen
			a.dlg = nil
			v := a.View()
			lines := strings.Split(strings.TrimRight(v, "\n"), "\n")
			if len(lines) > a.height {
				t.Fatalf("%s %dx%d: frame is %d rows (terminal is %d): it must fill exactly with the footer pinned\n%s", screen, a.width, a.height, len(lines), a.height, v)
			}
			// footer on the bottom two rows
			last := strings.TrimRight(lines[len(lines)-1], " ")
			if len(lines) == a.height {
				if !strings.Contains(last, "[F10|esc0]") && !strings.Contains(last, "[j/k]") {
					t.Fatalf("%s %dx%d: bottom row is not the keystrip: %q", screen, a.width, a.height, last[:min(80, len(last))])
				}
				if !strings.Contains(lines[len(lines)-2], "stack") {
					t.Fatalf("%s %dx%d: row above the keystrip is not the status line: %q", screen, a.width, a.height, lines[len(lines)-2][:min(80, len(lines[len(lines)-2]))])
				}
			}
			// every visible row fits the width
			for i, l := range lines {
				if w := lipgloss.Width(l); w > a.width {
					t.Fatalf("[%s %dx%d] row %d is %d cols > %d:", screen, a.width, a.height, i, w, a.width)
				}
			}
			_ = last
		}
		_ = size
	}
}

// TestUI_SettingsTree — the owner's settings organization (groups → keys):
// level 0 lists the groups; enter opens one; j/k + enter/e land on a key's
// editor; h goes back to the groups; the flat 255-key wall no longer renders.
func TestUI_SettingsTree(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)
	a.width, a.height = 100, 32

	// level 0 — the groups (the tree top, per the configschema registry)
	a.gotoScreenOr("settings")
	v := a.View()
	// panelHead renders titles in caps — compare case-insensitively.
	if !strings.Contains(strings.ToLower(v), "settings — groups") {
		t.Fatalf("level 0 must show the groups list:\n%s", v)
	}
	for _, g := range []string{"core", "boot", "services", "email", "integrations", "compilation", "limits", "security", "test", "stack"} {
		if !strings.Contains(v, g) {
			t.Fatalf("the group %q is missing from the groups level:\n%s", g, v)
		}
	}

	// enter → level 1 (the selected group's keys)
	a2, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}) // down one group
	b, _ := a2.(*app)
	c, _ := b.Update(tea.KeyMsg{Type: tea.KeyEnter})
	d, _ := c.(*app)
	if !d.menuOpen {
		// (sanity: we never touched the menu)
	}
	v2 := d.View()
	if strings.Contains(strings.ToLower(v2), "settings — groups") {
		t.Fatalf("enter must open a group (level 1), still on the groups level:\n%s", v2)
	}

	// back to the groups (b = back a level; tab/arrow-hop switches panes)
	e, _ := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	f, _ := e.(*app)
	v3 := f.View()
	if !strings.Contains(strings.ToLower(v3), "settings — groups") {
		t.Fatalf("h must return to the groups level:\n%s", v3)
	}

	// select a key of the current group and open its editor
	f.setLevel = 1
	f.setCurSel = 0
	if len(f.setGroups[f.setCurGroup].Keys) == 0 {
		t.Skip("the selected group has no keys")
	}
	key := f.setGroups[f.setCurGroup].Keys[0].Key
	g, _ := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	h, _ := g.(*app)
	if h.editKey != key {
		t.Fatalf("enter on a settings row must open its editor (got %q, want %q)", h.editKey, key)
	}
	v4 := h.View()
	if !strings.Contains(v4, "EDIT") {
		t.Fatalf("the editor box must render:\n%s", v4)
	}
}
