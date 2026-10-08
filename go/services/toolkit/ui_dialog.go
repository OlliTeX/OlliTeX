package toolkit

import (
	"context"
	"fmt"
)

// ui_dialog.go — the confirm/prompt state, ported 2026-10-07 off the
// overlay+tea runtime: the dialog is plain app state (a.dlg) and the tview
// app swaps it in as a centered modal root (tui.go showDialog/dialogClosed).
// The security-critical default is unchanged: EVERY key that is not an
// explicit confirmation closes the dialog as NO; nothing destructive ever
// fires on a stray keypress.

type dialogKind string

const (
	dlgConfirm dialogKind = "confirm" // the classic contract: [y]=yes, everything else NO
	dlgPrompt  dialogKind = "prompt"  // the action prompt: enter = confirm the typed value, esc = cancel
)

type dialog struct {
	kind    dialogKind
	id      string
	title   string
	prompt  string
	context string
	value   string

	onYes     func()       // confirm (d.kind == dlgConfirm)
	onYesText func(string) // prompt: confirm with the typed value
	onNo      func()       // cancel (both kinds)
}

// askStop — the classic confirm (the guard is the same as the overlay era).
func (a *app) askStop() {
	if a.dlg != nil {
		return
	}
	a.dlg = &dialog{
		kind:   dlgConfirm,
		id:     "stop",
		title:  "Stop the OlliTeX stack?",
		prompt: "This halts the web, mongo, redis + compile services.",
		context: a.stopContext(),
		onYes:  func() { a.runJob("stop", func(ctx context.Context) (string, error) { return a.tk.StackDown(ctx, false) }) },
	}
	a.redraw()
}

func (a *app) askRestart() {
	if a.dlg != nil {
		return
	}
	a.dlg = &dialog{
		kind:   dlgConfirm,
		id:     "restart",
		title:  "Restart the OlliTeX stack?",
		prompt: "Services come down and back up.",
		onYes:  func() { a.runJobLong("restart", a.tk.StackRestart) },
	}
	a.redraw()
}

func (a *app) askRestore() {
	if a.dlg != nil {
		return
	}
	a.dlg = &dialog{
		kind:   dlgConfirm,
		id:     "restore",
		title:  "Restore the config store?",
		prompt: "Restores the encrypted config snapshot to the config store.",
		onYes: func() {
			a.runJob("restore", func(ctx context.Context) (string, error) {
				if a.tk.Store == nil {
					return "", fmt.Errorf("config store not configured")
				}
				n, err := a.tk.Store.Restore(a.tk.BackupPath())
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%d keys restored", n), nil
			})
		},
	}
	a.redraw()
}

func (a *app) askQuit() {
	if a.dlg != nil {
		return
	}
	a.dlg = &dialog{
		kind:   dlgConfirm,
		id:     "quit",
		title:  "Quit the toolkit?",
		prompt: "The stack keeps running (it is docker-managed).",
		onYes:  func() { a.quitConfirmed() },
	}
	a.redraw()
}

// dialogPrompt — the two-step prompts (the classic input field): the value
// stays behind a mask; esc = NO; enter confirms the typed value (nothing is
// ever auto-confirmed by a stray key).
func (a *app) dialogPrompt(id, title, prompt string) {
	a.dlg = &dialog{kind: dlgPrompt, id: id, title: title, prompt: prompt, value: ""}
	a.editVal, a.editMask = a.dlg.value, true
	a.redraw()
}

// quitConfirmed — the quit dialog's YES (the tview app stops).
func (a *app) quitConfirmed() {
	traceFile("QUITCONFIRMED start")
	a.endShell()
	if a.tview != nil {
		a.tview.tearDown() // app.Stop() deadlocks: see tearDown
	}
	traceFile("QUITCONFIRMED teardown returned")
}

func dKindName(d *dialog) string {
	if d == nil {
		return "nil"
	}
	switch d.kind {
	case dlgConfirm:
		return "confirm"
	case dlgPrompt:
		return "prompt"
	}
	return "?"
}

// stopContext — the live stack line for the stop dialog.
func (a *app) stopContext() string {
	up, total := a.stackCount()
	if up == 0 {
		return "stack: no service containers up"
	}
	var names []string
	for _, c := range a.containerList() {
		if c.Up {
			names = append(names, c.Name)
		}
	}
	if len(names) == 0 {
		return fmt.Sprintf("stack: %d/%d up", up, total)
	}
	return fmt.Sprintf("up: %s", joinFirst(names, 6))
}

// joinFirst — "a b c … (n more)" style join (bounded for TUI width).
func joinFirst(xs []string, n int) string {
	if len(xs) <= n {
		out := ""
		for i, x := range xs {
			if i > 0 {
				out += " · "
			}
			out += x
		}
		return out
	}
	out := ""
	for i, x := range xs[:n-1] {
		if i > 0 {
			out += " · "
		}
		out += x
	}
	return out + " · +" + itoa(len(xs)-n+1)
}


// escSemantics — the unified esc/q/ctrl+c owner (ported from the overlay era
// unchanged): dialog → cancel; settings → back to the dashboard; any other
// screen → the dashboard. The quit guard is NOT behind esc (only q/ctrl+c +
// the menu offer quit, with the stack-state confirm when the stack is up).
func (a *app) escSemantics() {
	if a.dlg != nil {
		a.dlg = nil
		a.editKey, a.editVal, a.editMask = "", "", false
		return
	}
	a.screen = "dashboard"
	a.focus = 0
	a.dcur, _ = a.findItem("dashboard")
}
