package toolkit

import (
	"context"
	"fmt"
	"strings"

	ob "github.com/rmhubbert/bubbletea-overlay"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

// dialog — the mc prompt box (rmhubbert/bubbletea-overlay compositor +
// lrstanley/bubblezone click targets).
//
// Two shapes, one type:
//   - confirm: a yes/no question  (stop · restart · restore · quit)
//   - prompt:  a typed input line (TLS cert/key paths · n-gram langs ·
//     bootstrap email/password) — the classic overlay text field
//
// Security contract: the value is echoed only in the prompt line (never
// stored in the message log, never in the view shell), [y]/[n]/[enter]/[esc]
// are the only exits, and a click outside the box is a NO (the mc default) —
// so a stray click can never run a destructive job.

type dialog struct {
	id      string // zone id prefix (this session's marker namespace)
	kind    string // "confirm" | "prompt"
	title   string
	prompt  string // the question / the input label
	value   string // the typed value (prompt only)
	action  string // the job to run on YES / ENTER
	context string // second line of context shown under the title
	step1   string // for two-step flows: captured step-1 value
}

// askConfirm opens a yes/no box. YES runs `action`.
func (a *app) askConfirm(title, context, action string) {
	a.dlg = &dialog{
		id:      a.zoneID + ".dlg." + strings.ReplaceAll(action, ":", "-"),
		kind:    "confirm",
		title:   title,
		prompt:  "Are you sure?   [y] yes   [n] no (default)",
		action:  action,
		context: context,
	}
}

func (a *app) askStop() {
	a.askConfirm("stop the stack", "every container in project '"+a.tk.Project+"' stops; the data dir is untouched.", "stop")
}
func (a *app) askRestart() {
	a.askConfirm("restart the stack", "every container in project '"+a.tk.Project+"' restarts (config unchanged).", "restart")
}
func (a *app) askRestore() {
	a.askConfirm("restore the config store", "the backup snapshot ("+a.tk.BackupPath()+") is loaded back — current stored values are replaced.", "restore")
}
func (a *app) askQuit() {
	a.askConfirm("quit the toolkit", "quitting leaves the stack running"+a.quitTail(), "quit")
}

func (a *app) quitTail() string {
	if a.stackUpNow() {
		return " (it is UP — stop it first with d if you want it down)"
	}
	return ""
}

// dialogPrompt opens a typed-input box; ENTER collects the value and runs
// the two-step chain (the Actions flow) or the single action.
func (a *app) dialogPrompt(kind, title, prompt string) {
	a.dlg = &dialog{
		id:     a.zoneID + ".dlg." + kind,
		kind:   "prompt",
		title:  title,
		prompt: prompt,
		action: kind,
	}
}

func (a *app) dialogConfirm() tea.Cmd {
	d := a.dlg
	if d == nil {
		return nil
	}
	if d.kind == "confirm" {
		act := d.action
		a.dlg = nil
		return a.confirmAction(act)
	}
	// prompt: the "ok" button = enter with the typed value
	v := d.value
	a.dlg = nil
	return a.promptConfirm(d, v)
}

// confirmAction runs the YES path of a confirm box.
func (a *app) confirmAction(action string) tea.Cmd {
	switch action {
	case "stop":
		return a.runJob("stop", func(ctx context.Context) (string, error) { return a.tk.StackDown(ctx, false) })
	case "restart":
		return a.runJob("restart", a.tk.StackRestart)
	case "restore":
		return a.runJob("restore", func(ctx context.Context) (string, error) {
			if a.tk.Store == nil {
				return "", context.Canceled
			}
			n, err := a.tk.Store.Restore(a.tk.BackupPath())
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%d keys", n), nil
		})
	case "quit":
		return tea.Quit
	}
	return nil
}

// ---- keys (dialog is modal: it owns every key while open) -------------------

func (a *app) dialogKey(msg tea.KeyMsg) tea.Cmd {
	d := a.dlg
	if d == nil {
		return nil
	}
	k := msg.String()
	if d.kind == "confirm" {
		if k == "y" || k == "yes" || k == "enter" {
			act := d.action
			a.dlg = nil
			return a.confirmAction(act)
		}
		a.dlg = nil // n, esc, q, anything else → NO (the mc default)
		return nil
	}
	// prompt
	switch k {
	case "enter":
		a.dlg = nil
		return a.promptConfirm(d, strings.TrimSpace(d.value))
	case "backspace", "delete":
		if len(d.value) > 0 {
			d.value = d.value[:len(d.value)-1]
		}
		return nil
	case "esc", "q":
		a.dlg = nil
		return nil
	}
	if len(msg.Runes) == 1 && msg.Runes[0] >= 32 && msg.Runes[0] != 127 {
		d.value += string(msg.Runes[0])
	}
	return nil
}

// promptConfirm runs the ENTER path of a prompt box (the Actions two-step
// chain resolves here).
func (a *app) promptConfirm(d *dialog, v string) tea.Cmd {
	switch d.action {
	case "tls-cert":
		a.actCert = v
		a.dialogPrompt("tls-key", "TLS import — private key", "host path of the TLS private key (a file):")
		return nil
	case "tls-key":
		a.actKey = v
		cert, key := a.actCert, v
		a.actCert, a.actKey = "", ""
		a.screen = "actions"
		return a.runTLS(cert, key)
	case "ngram":
		a.screen = "actions"
		return a.runNgram(v)
	case "boot-email":
		a.actCert = v
		a.dialogPrompt("boot-pass", "first-admin bootstrap", "admin password:")
		return nil
	case "boot-pass":
		email := a.actCert
		a.actCert = ""
		a.screen = "actions"
		return a.runBootstrap(email, v)
	}
	return nil
}

// (the dialog chain resolves via the live a.dlg before it is cleared)

//
// We snapshot it in dialogKey before clearing.

// ---- the box (View) — the overlay compositor --------------------------------

func (d *dialog) render(a *app, back string) string {
	fore := a.dlgBox(d)
	m := ob.New(viewStr(fore), viewStr(back), ob.Center, ob.Center, 2, 2)
	return m.View()
}

// viewStr adapts a string to the overlay's Viewable interface.
type viewStr string

func (v viewStr) View() string { return string(v) }

// dlgBox draws the classic mc prompt box: a bordered panel with a titled
// header, the question, the (optional) typed line, and the [y]/[n] or
// [enter]/[esc] buttons — each button a bubblezone click target.
func (a *app) dlgBox(d *dialog) string {
	id := d.id
	// the box fits the terminal (the classic prompt is ~2/3 of the width, but
	// never wider than the screen — the composite would overflow otherwise)
	boxW := a.width * 2 / 3
	if boxW < 46 {
		boxW = 46
	}
	if boxW > 86 {
		boxW = 86
	}
	wrapW := boxW - 8
	var b strings.Builder
	b.WriteString("  " + a.dlgTitle(fit(d.title, wrapW)) + "\n")
	if d.context != "" {
		for _, l := range wrap(d.context, wrapW) {
			b.WriteString("  " + fit(l, wrapW) + "\n")
		}
	}
	b.WriteString("  " + fit(d.prompt, wrapW) + "\n")
	if d.kind == "prompt" {
		b.WriteString("  " + a.dlgInput(d.value) + styleDim.Render(" |") + "\n")
	}
	b.WriteString("\n")
	if d.kind == "confirm" {
		b.WriteString("  " + zone.Mark(id+".n", a.dlgBtn("n", " no ")) + "   " + zone.Mark(id+".y", a.dlgBtn("y", " yes ")) + "   " + styleDim.Render("[esc] cancel") + "\n")
	} else {
		b.WriteString("  " + zone.Mark(id+".ok", a.dlgBtn("enter", " ")) + "  " + zone.Mark(id+".n", a.dlgBtn("esc", " cancel ")) + "\n")
	}
	return a.dlgPanel(strings.TrimRight(b.String(), "\n"), boxW)
}

// wrap folds a paragraph at w display columns (words, no mid-word breaks).
func wrap(s string, w int) []string {
	if w <= 0 {
		w = 40
	}
	var out []string
	for _, raw := range strings.Split(s, "\n") {
		words := strings.Fields(raw)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		cur := ""
		for _, wd := range words {
			if cur == "" {
				cur = wd
			} else if lipgloss.Width(cur+" "+wd) <= w {
				cur += " " + wd
			} else {
				out = append(out, cur)
				cur = wd
			}
		}
		if cur != "" {
			out = append(out, cur)
		}
	}
	return out
}

// small style helpers (kept here so the dialog is self-contained).
func (a *app) dlgTitle(t string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("16")).
		Background(lipgloss.Color("39")).
		Padding(0, 1).
		Render(" " + strings.ToUpper(t) + " ")
}

func (a *app) dlgInput(v string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("255")).
		Background(lipgloss.Color("236")).
		Padding(0, 1).
		Render(" " + v)
}

func (a *app) dlgBtn(k, label string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Background(lipgloss.Color("236")).
		Padding(0, 0).
		Render(" [" + k + "]" + label)
}

func (a *app) dlgPanel(body string, boxW int) string {
	w := boxW
	if w < 46 {
		w = 46
	}
	if w > a.width-2 {
		w = a.width - 2
	}
	if w < 46 {
		w = 46
	}
	pad := w - 2
	corner := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	var out []string
	out = append(out, corner.Render("╔"+strings.Repeat("═", w-2)+"╗"))
	for _, l := range strings.Split(body, "\n") {
		if lipgloss.Width(l) < pad {
			l += strings.Repeat(" ", pad-lipgloss.Width(l))
		} else {
			l = fit(l, pad)
		}
		out = append(out, corner.Render("│")+" "+l+" "+corner.Render("│"))
	}
	out = append(out, corner.Render("╚"+strings.Repeat("═", w-2)+"╝"))
	return strings.Join(out, "\n")
}
