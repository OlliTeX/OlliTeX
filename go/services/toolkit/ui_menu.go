package toolkit

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	menubar "github.com/jejacks0n/bubbletea-menubar"
)

// newMenus builds the menu bar (jejacks0n/bubbletea-menubar) — the top menu
// strip of the classic console. Every item lands in dispatchMenu, the same
// funnel the left-pane rows and the keystrip chips use, so menu / list /
// keys / clicks are behaviorally identical.
//
// The strip is the freebsd-installer "File · Edit · View" row: stable top-
// level verbs (the screens) with the classic single-letter hotkeys (Ctrl-F /
// F10 opens the bar — the mc convention).

func newMenus(a *app) menubar.Model {
	open := func(id string) func() tea.Msg {
		return func() tea.Msg { return menuActionMsg{action: "open:" + id} }
	}
	verb := func(action string) func() tea.Msg {
		return func() tea.Msg { return menuActionMsg{action: action} }
	}
	items := []menubar.MenuItem{
		{
			Label: "File", Hotkey: "F",
			SubMenu: []menubar.MenuItem{
				{Label: "Stack panel", Action: open("stack")},
				{Label: "Logs", Action: open("logs")},
				{Label: "Settings", Action: open("settings")},
				{Label: "Shells", Action: open("shells")},
				{Label: "Actions (admin)", Action: open("actions")},
				{Label: "Doctor", Action: open("doctor")},
				{Label: "Backup", Action: open("backup")},
				{Label: "About", Action: open("about")},
				menubar.Separator(),
				{Label: "Quit the toolkit", Action: verb("quit")},
			},
		},
		{
			Label: "Stack", Hotkey: "S",
			SubMenu: []menubar.MenuItem{
				{Label: "Start the stack", Action: verb("start")},
				{Label: "Stop the stack", Action: verb("stop")},
				{Label: "Restart the stack", Action: verb("restart")},
				{Label: "Pull the images", Action: verb("pull")},
				menubar.Separator(),
				{Label: "Health check (the doctor)", Action: open("doctor")},
			},
		},
		{
			Label: "Settings", Hotkey: "T",
			SubMenu: []menubar.MenuItem{
				{Label: "Open the config store", Action: open("settings")},
			},
		},
		{
			Label: "Shells", Hotkey: "H",
			SubMenu: buildShellMenu(verb),
		},
		{
			Label: "Doctor", Hotkey: "D",
			SubMenu: []menubar.MenuItem{
				{Label: "Run the health checks", Action: verb("doctor")},
			},
		},
		{
			Label: "Backup", Hotkey: "B",
			SubMenu: []menubar.MenuItem{
				{Label: "Back up the store", Action: verb("backup")},
				{Label: "Restore from the snapshot", Action: verb("restore")},
			},
		},
		{
			Label: "Actions", Hotkey: "A",
			SubMenu: []menubar.MenuItem{
				{Label: "Import the TLS cert + key", Action: verb("tls")},
				{Label: "Download the n-gram models", Action: verb("ngram")},
				{Label: "n-gram status (local)", Action: verb("ngramstatus")},
				menubar.Separator(),
				{Label: "Bootstrap the first admin", Action: verb("bootstrap")},
			},
		},
		{
			Label: "Help", Hotkey: "?", Shortcut: "F1",
			SubMenu: []menubar.MenuItem{
				{Label: "The key map + about", Action: open("about")},
				menubar.Separator(),
				{Label: "Quit", Action: verb("quit")},
			},
		},
	}
	mb := menubar.New(items)
	mb.Active = false // closed by default — F10 (or a click on a label) opens it
	mb.Styles = menubar.Styles{
		Bar:              mb.Styles.Bar,
		Item:             lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("250")).Padding(0, 1),
		SelectedItem:     lipgloss.NewStyle().Background(lipgloss.Color("39")).Foreground(lipgloss.Color("16")).Bold(true).Padding(0, 1),
		Shortcut:         lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		Dropdown:         lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("51")).Padding(0, 1),
		DropdownItem:     lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("250")),
		DropdownSelected: lipgloss.NewStyle().Background(lipgloss.Color("39")).Foreground(lipgloss.Color("16")).Padding(0, 1),
		ShortcutSelected: lipgloss.NewStyle().Foreground(lipgloss.Color("255")),
		Hotkey:           lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		Separator:        lipgloss.NewStyle().Foreground(lipgloss.Color("238")),
		Disabled:         lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	}
	return mb
}

func buildShellMenu(verb func(string) func() tea.Msg) []menubar.MenuItem {
	out := []menubar.MenuItem{}
	for _, s := range Shells {
		out = append(out, menubar.MenuItem{Label: s.Label, Action: verb("shell:" + s.Label)})
	}
	return out
}

// dispatchMenu is the single funnel for the menu bar actions.
func (a *app) dispatchMenu(action string) tea.Cmd {
	a.menuActive(false)
	switch action {
	case "open:stack":
		a.dcur, _ = a.findItem("stack")
		a.screen = "stack"
		return a.refreshStatus()
	case "open:logs":
		a.gotoScreen("logs")
		return a.refreshLogs(a.logName)
	case "open:settings":
		a.gotoScreen("settings")
		a.loadSettings()
		return nil
	case "open:shells":
		a.gotoScreen("shells")
		return nil
	case "open:actions":
		a.gotoScreen("actions")
		return nil
	case "open:doctor":
		a.gotoScreen("doctor")
		return a.runDoctor()
	case "open:backup":
		a.gotoScreen("backup")
		return nil
	case "open:about":
		a.gotoScreen("about")
		return nil
	case "start":
		a.gotoScreen("stack")
		return a.runJob("start", a.tk.StackUp)
	case "stop":
		a.gotoScreen("stack")
		a.askStop()
		return nil
	case "restart":
		a.gotoScreen("stack")
		a.askRestart()
		return nil
	case "pull":
		a.gotoScreen("stack")
		return a.runJob("pull", a.tk.PullImages)
	case "doctor":
		a.gotoScreen("doctor")
		return a.runDoctor()
	case "backup":
		a.gotoScreen("backup")
		return a.runJob("backup", func(ctx context.Context) (string, error) {
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
		return nil
	case "tls":
		a.gotoScreen("actions")
		a.dialogPrompt("tls-cert", "TLS import — certificate", "host path of the TLS certificate (a file):")
		return nil
	case "ngram":
		a.gotoScreen("actions")
		a.dialogPrompt("ngram", "n-gram models", "languages (en,de — empty = all five):")
		return nil
	case "ngramstatus":
		a.gotoScreen("actions")
		a.actResult = a.ngramStatusText()
		return nil
	case "bootstrap":
		a.gotoScreen("actions")
		a.dialogPrompt("boot-email", "first-admin bootstrap", "admin email:")
		return nil
	case "quit":
		if a.stackUpNow() {
			a.askQuit()
			return nil
		}
		return tea.Quit
	}
	if b, ok := cutPrefix(action, "shell:"); ok {
		return a.startShell(b)
	}
	return nil
}

func cutPrefix(s, pre string) (string, bool) {
	if len(s) > len(pre) && s[:len(pre)] == pre {
		return s[len(pre):], true
	}
	return "", false
}

// gotoScreen moves the master cursor to the named row (the left pane) and opens it.
func (a *app) gotoScreen(id string) {
	if i, ok := a.findItem(id); ok {
		a.dcur = i
	}
	a.screen = id
	a.editKey, a.editVal, a.editMask = "", "", false
	if id == "settings" {
		a.setLevel, a.setCurGroup, a.setCurSel = 0, 0, 0
		a.focus = 1 // the settings tree owns the keys (two-pane focus)
	} else {
		a.focus = 0
	}
}

func (a *app) findItem(id string) (int, bool) {
	for i, it := range a.masterList() {
		if it.id == id {
			return i, true
		}
	}
	return 0, false
}

var _ = fmt.Sprintf
