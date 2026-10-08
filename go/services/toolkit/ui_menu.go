package toolkit

import (
	"context"
	"fmt"
	"time"
)

// ui_menu.go — the menu, ported 2026-10-07 to the tview era: the classic
// File/Stack/Settings/… tree is now a plain leaf list (menuLeaves) rendered
// in the tview menu root (F10/F9/ctrl+m or the MENU strip button), and
// dispatchMenu stays the single funnel — list rows, strip chips, menu
// leaves and keys all land in the same handler, so menu / list / keys are
// behaviorally identical (the old menubarkit widget is retired with the
// tea runtime it drew into).

// menuLeaf — one actionable menu line (label + the dispatch action).
type menuLeaf struct {
	label  string
	action string
}

// menuLeaves — the full classic tree flattened (File first, the same order
// the owner has used since the mc days).
func menuLeaves() []menuLeaf {
	out := []menuLeaf{
		{"File — Stack panel", "open:stack"},
		{"File — Logs", "open:logs"},
		{"File — Settings", "open:settings"},
		{"File — Shells", "open:shells"},
		{"File — Actions (admin)", "open:actions"},
		{"File — Doctor", "open:doctor"},
		{"File — Hub (instance views)", "open:hub"},
		{"File — Backup", "open:backup"},
		{"File — About", "open:about"},
		{"Quit the toolkit", "quit"},
		{"Stack — Start the stack", "start"},
		{"Stack — Stop the stack", "stop"},
		{"Stack — Restart the stack", "restart"},
		{"Stack — Pull the images", "pull"},
		{"Doctor — Run the health checks", "doctor"},
		{"Backup — Back up the store", "backup"},
		{"Backup — Restore from the snapshot", "restore"},
	}
	for _, s := range Shells {
		out = append(out, menuLeaf{"Shells — " + s.Label, "shell:" + s.Label})
	}
	out = append(out,
		menuLeaf{"Actions — Import the TLS cert + key", "tls"},
		menuLeaf{"Actions — Download the n-gram models", "ngram"},
		menuLeaf{"Actions — n-gram status (local)", "ngramstatus"},
		menuLeaf{"Actions — Bootstrap the first admin", "bootstrap"},
	)
	return out
}

// dispatchMenu is the single funnel for the menu actions.
func (a *app) dispatchMenu(action string) {
	switch action {
	case "open:stack":
		a.dcur, _ = a.findItem("stack")
		a.screen = "stack"
	case "open:logs":
		a.gotoScreen("logs")
		a.refreshLogs(a.logName)
	case "open:settings":
		a.gotoScreen("settings")
		a.loadSettings()
	case "open:shells":
		a.gotoScreen("shells")
	case "open:actions":
		a.gotoScreen("actions")
	case "open:doctor":
		a.gotoScreen("doctor")
		a.runDoctor()
	case "open:hub":
		a.gotoScreen("hub")
		if a.hub == nil || time.Since(a.hub.Now) > 15*time.Second {
			a.refreshHub()
		}
	case "open:backup":
		a.gotoScreen("backup")
	case "open:about":
		a.gotoScreen("about")
	case "start":
		a.gotoScreen("stack")
		a.runJob("start", a.tk.StackUp)
	case "stop":
		a.gotoScreen("stack")
		a.askStop()
		if a.tview != nil {
			a.tview.showDialog(a.dlg)
		}
	case "restart":
		a.gotoScreen("stack")
		a.askRestart()
		if a.tview != nil {
			a.tview.showDialog(a.dlg)
		}
	case "pull":
		a.gotoScreen("stack")
		a.runJob("pull", a.tk.PullImages)
	case "doctor":
		a.gotoScreen("doctor")
		a.runDoctor()
	case "backup":
		a.gotoScreen("backup")
		a.runJob("backup", func(ctx context.Context) (string, error) {
			if a.tk.Store == nil {
				return "", context.Canceled
			}
			m, err := a.tk.Store.Dump(a.tk.BackupPath())
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%d keys", len(m)), nil
		})
	case "restore":
		a.gotoScreen("backup")
		a.askRestore()
		if a.tview != nil {
			a.tview.showDialog(a.dlg)
		}
	case "tls":
		a.gotoScreen("actions")
		a.dialogPrompt("tls-cert", "TLS import — certificate", "host path of the TLS certificate (a file):")
		if a.tview != nil {
			a.tview.showDialog(a.dlg)
		}
	case "ngram":
		a.gotoScreen("actions")
		a.dialogPrompt("ngram", "n-gram models", "languages (en,de — empty = all five):")
		if a.tview != nil {
			a.tview.showDialog(a.dlg)
		}
	case "ngramstatus":
		a.gotoScreen("actions")
		a.actResult = a.ngramStatusText()
		a.redraw()
	case "bootstrap":
		a.gotoScreen("actions")
		a.dialogPrompt("boot-email", "first-admin bootstrap", "admin email:")
		if a.tview != nil {
			a.tview.showDialog(a.dlg)
		}
	case "quit":
		a.quitFromMenu()
	}
	if b, ok := cutPrefix(action, "shell:"); ok {
		a.startShell(b)
	}
}

// quitFromMenu — menu "Quit" (the owner's contract: the quit guard, then
// the exit).
func (a *app) quitFromMenu() {
	if a.stackUpNow() {
		a.askQuit()
		if a.tview != nil {
			a.tview.showDialog(a.dlg)
		}
		return
	}
	if a.tview != nil {
		a.tview.tearDown() // app.Stop() deadlocks: see tearDown
	}
}
