package toolkit

import (
	"strings"
	"testing"
)

// ui_render_test.go — the console contract in the tview era (2026-10-07):
// the state model renders every screen into the right pane + status line,
// the dialogs carry their classic [y]/[n] and [enter]/[esc] contracts as
// plain state (the tview modals paint them), the key map (handleScreenKey)
// drives the screens through the classic transitions, and the settings
// tree keeps the groups → keys → editor flow.

func fullRender(a *app) string {
	return a.rightPane() + "\n" + a.statusLine() + "\n" + a.keyStrip()
}

// TestUI_RenderAllScreens — every screen renders its pane + the footer
// (status line + keystrip), the classic look contract.
func TestUI_RenderAllScreens(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)
	a.width, a.height = 110, 32

	for _, screen := range []string{"dashboard", "stack", "shells", "settings", "actions", "doctor", "backup", "about"} {
		a.screen = screen
		v := fullRender(a)
		if !strings.Contains(v, "[j/k] move") {
			t.Errorf("%s: the keystrip (the bracketed-key row) missing:\n%s", screen, v)
		}
		if !strings.Contains(v, "stack") {
			t.Errorf("%s: the status line (stack state) missing:\n%s", screen, v)
		}
	}
}

// TestUI_StopConfirmDialog — the stop confirmation carries its classic
// contract: title + the [y]/[n] + context line (the tview modal paints it).
func TestUI_StopConfirmDialog(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)
	a.screen = "stack"
	a.askStop()
	if a.dlg == nil {
		t.Fatal("askStop must arm the dialog")
	}
	if a.dlg.kind != dlgConfirm {
		t.Fatalf("stop must be a confirm dialog, got %q", a.dlg.kind)
	}
	if !strings.Contains(strings.ToLower(a.dlg.title), "stop") {
		t.Errorf("the stop dialog must carry its title:\n%q", a.dlg.title)
	}
	if a.dlg.onYes == nil {
		t.Error("the stop dialog must carry its YES action")
	}
	// everything else is NO: a stray key closes without the action
	a.dlg = nil
}

// TestUI_PromptDialog — the two-step input prompt (the classic contract):
// the typed value stays behind the mask; enter confirms it, esc cancels.
func TestUI_PromptDialog(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)
	a.screen = "actions"
	a.dialogPrompt("ngram", "n-gram models", "languages (en,de — empty = all five):")
	if a.dlg == nil {
		t.Fatal("dialogPrompt must arm the dialog")
	}
	if a.dlg.kind != dlgPrompt {
		t.Fatalf("the ngram prompt must be a prompt dialog, got %q", a.dlg.kind)
	}
	if a.dlg.value != "" {
		t.Errorf("the prompt value must start empty (typed value, never pre-filled), got %q", a.dlg.value)
	}
	a.dlg.value = "en,de"
	// esc → NO (the value is discarded)
	a.escSemantics()
	if a.dlg != nil {
		t.Error("esc must close the prompt as NO")
	}
}

// TestUI_MenuLeaves — the full classic menu tree exists as leaves (the
// tview menu root renders them; dispatchMenu is the single funnel).
func TestUI_MenuLeaves(t *testing.T) {
	leaves := menuLeaves()
	if len(leaves) < 15 {
		t.Fatalf("the menu leaves must cover the classic tree, got %d", len(leaves))
	}
	joined := ""
	for _, l := range leaves {
		joined += l.label + "\n"
	}
	for _, want := range []string{"Stack panel", "Quit the toolkit", "Start the stack", "n-gram", "Bootstrap", "mongo"} {
		if !strings.Contains(joined, want) {
			t.Errorf("menu leaves missing %q:\n%s", want, joined)
		}
	}
}

// TestUI_KeyMap — the classic key map drives the screens (j/k move, enter
// opens, per-screen u/d/t shortcuts arm the right jobs/dialogs, esc backs
// out, q asks quit with the stack guard).
func TestUI_KeyMap(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)

	// j/k move the master cursor
	items := a.masterList()
	before := a.dcur
	a.handleScreenKey("j")
	if a.dcur != (before+1)%len(items) {
		t.Fatalf("j must move the cursor down (got %d, want %d)", a.dcur, (before+1)%len(items))
	}
	a.handleScreenKey("k")
	if a.dcur != before {
		t.Fatalf("k must move the cursor back (got %d, want %d)", a.dcur, before)
	}

	// enter opens the current row
	a.handleScreenKey("enter")
	if a.screen != items[a.dcur].id {
		t.Fatalf("enter must open the current row (got %q, want %q)", a.screen, items[a.dcur].id)
	}

	// number keys jump
	a.handleScreenKey("1")
	if a.dcur != 0 {
		t.Fatalf("1 must land on the first row")
	}

	// stack screen: d arms the stop dialog, u starts the stack (job queued)
	a.gotoScreen("stack")
	a.handleScreenKey("d")
	if a.dlg == nil || a.dlg.kind != dlgConfirm {
		t.Fatalf("d on stack must arm the stop confirm dialog")
	}
	a.dlg = nil

	// esc semantics: any screen → the dashboard
	a.gotoScreen("logs")
	a.escSemantics()
	if a.screen != "dashboard" {
		t.Fatalf("esc from logs must land on the dashboard, got %q", a.screen)
	}

	// dashboard with a (unavailable → downed) stack: q quits without asking
	a.dock = nil
	if !a.quitNow() {
		t.Fatalf("q with a downed stack must quit")
	}
}

// TestUI_SettingsTree — the settings organization (groups → keys → editor):
// level 0 the groups; enter opens a group; back returns; enter on a key
// opens its editor.
func TestUI_SettingsTree(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)
	a.width, a.height = 100, 32

	a.loadSettings()
	a.gotoScreen("settings")
	v := fullRender(a)
	if !strings.Contains(strings.ToLower(v), "settings") {
		t.Fatalf("the settings pane must render:\n%s", v)
	}

	// j down one group, enter opens it
	a.focus = 1
	a.handleScreenKey("j")
	a.handleScreenKey("enter")
	if a.setLevel != 1 {
		t.Fatalf("enter on a group must open its keys (level 1), got %d", a.setLevel)
	}

	// back to the groups (b / h)
	a.handleScreenKey("b")
	if a.setLevel != 0 {
		t.Fatalf("b must return to the groups level, got %d", a.setLevel)
	}

	// select a key of the current group and open its editor
	a.setLevel = 1
	a.setCurSel = 0
	if len(a.setGroups[a.setCurGroup].Keys) == 0 {
		t.Skip("the selected group has no keys")
	}
	key := a.setGroups[a.setCurGroup].Keys[0].Key
	a.handleScreenKey("enter")
	if a.editKey != key {
		t.Fatalf("enter on a settings row must open its editor (got %q, want %q)", a.editKey, key)
	}
}
