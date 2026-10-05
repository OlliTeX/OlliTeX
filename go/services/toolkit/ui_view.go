package toolkit

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	zone "github.com/lrstanley/bubblezone"
)

// ---- the master list (the LEFT pane) ----------------------------------------

type masterItem struct {
	id     string
	label  string
	detail string
}

func (a *app) masterList() []masterItem {
	up, total := a.stackCount()
	state, stateOK := "down", false
	if up > 0 {
		state, stateOK = "UP", true
	}
	store := "off"
	if a.tk.Store != nil {
		store = "on"
	}
	items := []masterItem{
		{"dashboard", "Dashboard", a.tk.Project + " · " + state},
		{"stack", "Stack", fmt.Sprintf("%d/%d up", up, total)},
		{"logs", "Logs", a.logName},
		{"settings", "Settings", "configstore · " + store},
		{"shells", "Shells", "mongo · psql · app"},
		{"actions", "Actions", "tls · n-gram · admin"},
		{"doctor", "Doctor", a.doctorText()},
		{"backup", "Backup", "store snapshot"},
		{"about", "About", "rev " + a.tk.Ver},
	}
	_ = stateOK
	return items
}

func (a *app) doctorText() string {
	if len(a.doctorRows) == 0 {
		return "not run"
	}
	ok := 0
	for _, r := range a.doctorRows {
		if r.ok {
			ok++
		}
	}
	fmtS := fmt.Sprintf("%d/%d ok", ok, len(a.doctorRows))
	return fmtS
}

// ---- View ------------------------------------------------------------------

func (a *app) View() string {
	if a.screen == "shell" && a.shell != nil {
		base := a.viewShell()
		if a.dlg != nil {
			base = a.dlg.render(a, base)
		}
		return a.scan(base)
	}
	base := a.viewClassic()
	if a.dlg != nil {
		base = a.dlg.render(a, base)
	}
	return a.scan(base) // the zone markers are stripped + registered here, once
}

// scan strips the zone markers after registration (the library contract:
// Scan on the outermost view only, once — the view is rendered top-down so
// the marks exist at the time of this call).
func (a *app) scan(v string) string {
	if zone.Enabled() {
		if s := zone.Scan(v); s != "" {
			return s
		}
	}
	return v
}

// viewClassic — the mc composition: title · menu bar · two bordered panes ·
// status line · keystrip.
func (a *app) viewClassic() string {
	var out strings.Builder
	// line 1: title (the rev only when it is set — the build ldflags)
	var right string
	if a.tk.Ver != "" {
		right = styleDim.Render(fmt.Sprintf("  rev %s · project %s", a.tk.Ver, a.tk.Project))
	} else {
		right = styleDim.Render(fmt.Sprintf("  project %s", a.tk.Project))
	}
	title := " " + stylePaneTitle.Render(" OlliTeX Toolkit ")
	gap := a.width - lipgloss.Width(title) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	out.WriteString(title + strings.Repeat(" ", gap) + right + "\n")

	// line 2: the menu bar (the menubar library renders it + dropdowns)
	out.WriteString(a.menuBarLine() + "\n")

	// the panes (left = master list, right = the screen detail)
	paneH := a.height - 5
	if paneH < 4 {
		paneH = 4
	}
	var left strings.Builder
	left.WriteString(stylePaneTitle.Render("  S C R E E N S  "))
	left.WriteString("\n")
	items := a.masterList()
	for i, it := range items {
		row := a.masterRow(i, it)
		left.WriteString(row + "\n")
	}
	body := left.String()
	filled := strings.Count(body, "\n")
	if filled < paneH {
		body += strings.Repeat("\n", paneH-filled)
	}
	leftS := stylePanel.Render(body)

	rightBody := a.rightPane()
	rb := rightBody
	rfilled := strings.Count(rb, "\n")
	if rfilled < paneH {
		rb += strings.Repeat("\n", paneH-rfilled)
	}
	rightS := stylePanel.Render(rb)

	leftW := a.width / 3
	if a.width >= 100 {
		leftW = a.width * 2 / 5
	}
	if leftW < 24 {
		leftW = 24
	}
	rightW := a.width - leftW
	if rightW < 10 {
		rightW = 10
	}
	leftFit := fit(leftS, leftW)
	rightFit := fit(rightS, rightW)
	out.WriteString(leftFit + " " + rightFit + "\n")

	// status line
	out.WriteString(a.statusLine() + "\n")
	// keystorep (the classic bracketed keys, zone-targeted)
	out.WriteString(a.keyStrip())
	return out.String()
}

// menuBarLine renders the menubar (bar + any open dropdown) at full width.
func (a *app) menuBarLine() string {
	s := a.menu.View()
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	bar := lines[0]
	if lipgloss.Width(bar) < a.width {
		bar += strings.Repeat(" ", a.width-lipgloss.Width(bar))
	} else {
		bar = fit(bar, a.width)
	}
	if len(lines) > 1 {
		return bar + "\n" + strings.Join(lines[1:], "\n")
	}
	return bar
}

// masterRow — one left-pane row; the selected row is the mc cursor.
func (a *app) masterRow(i int, it masterItem) string {
	label := fit(it.label, 14)
	detail := fit(it.detail, 20)
	if i == a.dcur {
		return zone.Mark(a.zoneID+".row."+fmt.Sprint(i), styleSelected.Render("> "+label+" ")+styleDim.Render(detail))
	}
	return "   " + styleValue.Render(label) + "  " + styleDim.Render(detail)
}

// statusLine — docker / store / job state (the mc status line).
func (a *app) statusLine() string {
	sp := ""
	if a.loading {
		sp = "⠋ " + a.jobName + "… "
	}
	store := "store off"
	if a.tk.Store != nil {
		store = "store on"
	}
	up, total := a.stackCount()
	stack := "stack " + fmt.Sprintf("%d/%d", up, total)
	msg := sp + " " + stack + " · " + store
	if a.errMsg != "" {
		msg += "  ✗ " + a.errMsg
	} else if a.statusMsg != "" {
		msg += "  " + a.statusMsg
	}
	return " " + fit(strings.TrimSpace(msg), a.width)
}

// keyStrip — the bracketed-key row (the classic keystrip), zone-targeted so
// the primary actions are clickable.
func (a *app) keyStrip() string {
	zc := func(id, k, label string) string {
		return zone.Mark(a.zoneID+".strip."+id, styleChip.Render("["+k+"] "+label))
	}
	var seg []string
	seg = append(seg, zc("move", "j/k", "move"))
	seg = append(seg, zc("open", "enter", "open"))
	switch a.screen {
	case "stack":
		seg = append(seg, zc("start", "u", "start"), zc("stop", "d", "stop"), zc("restart", "r", "restart"), zc("pull", "p", "pull"))
	case "settings":
		seg = append(seg, zc("edit", "e", "edit"), zc("save", "x", "save"))
	case "logs":
		seg = append(seg, zc("cycle", "h/l", "cycle"), zc("refresh", "f", "refresh"))
	case "actions":
		seg = append(seg, zc("tls", "t", "tls"), zc("ngram", "n", "n-gram"), zc("status", "l", "status"), zc("admin", "b", "admin"))
	case "backup":
		seg = append(seg, zc("backup", "b", "backup"), zc("restore", "r", "restore"))
	case "shells":
		seg = append(seg, zc("shell", "1-4", "shell"))
	}
	seg = append(seg, zc("menu", "F10", "menu"), zc("help", "?", "help"))
	s := " " + strings.Join(seg, " ")
	if lipgloss.Width(s) > a.width {
		s = " " + strings.Join(seg[:len(seg)-2], " ")
	}
	return fit(s, a.width)
}

// ---- right pane: per-screen detail -----------------------------------------

func (a *app) rightPane() string {
	switch a.screen {
	case "dashboard":
		return a.panelDashboard()
	case "stack":
		return a.panelStack()
	case "shells":
		return a.panelShells()
	case "logs":
		return a.panelLogs()
	case "settings":
		return a.panelSettings()
	case "actions":
		return a.panelActions()
	case "doctor":
		return a.panelDoctor()
	case "backup":
		return a.panelBackup()
	case "about":
		return a.panelAbout()
	}
	return "  "
}

func panelHead(title string) string {
	return " " + styleHead.Render(" "+strings.ToUpper(title)+" ") + "\n"
}

func (a *app) panelDashboard() string {
	up, total := a.stackCount()
	var b strings.Builder
	b.WriteString(panelHead("dashboard — " + a.tk.Project))
	if a.dock == nil {
		b.WriteString("  " + styleErr.Render("docker socket unavailable") + "\n")
	} else {
		state, scol := "down", styleErr
		if up > 0 {
			state, scol = "up", styleOK
		}
		b.WriteString("  " + scol.Render(state) + "  " + styleDim.Render(fmt.Sprintf("%d/%d containers up   ", up, total)) + " " + a.tk.Project + "\n")
	}
	if a.tk.Store != nil {
		b.WriteString("  " + okText(true) + "  " + a.tk.StoreDescribe + "\n")
	} else {
		b.WriteString("  " + styleErr.Render("config store not configured") + "\n")
	}
	b.WriteString("\n")
	b.WriteString("  host contract: docker + one mounted folder + the docker socket.\n")
	b.WriteString("  the left list is the master pane — enter opens a screen.\n")
	b.WriteString("  F10 (or a click on a label) opens the menu bar.\n")
	return b.String()
}

func (a *app) panelStack() string {
	var b strings.Builder
	b.WriteString(panelHead("stack — plan + containers"))
	a.renderPlan(&b)
	lst := a.containerList()
	if a.dock != nil && len(lst) > 0 {
		b.WriteString("  " + styleKvK.Render("NAME") + " " + styleKvK.Render("STATE") + " " + styleKvK.Render("HEALTH") + " " + styleKvK.Render("PORTS") + "\n")
		for _, c := range lst {
			state, col := "down", styleErr
			if c.Up {
				state, col = "up ", styleOK
			}
			b.WriteString("  " + fit(c.Name, 28) + " " + col.Render(fit(state, 5)) + " " +
				styleDim.Render(fit(healthWord(c.Health), 9)) + " " +
				styleDim.Render(fit(c.Ports, 16)) + "\n")
		}
	}
	if a.dock == nil {
		b.WriteString("  " + styleErr.Render("docker socket unavailable — check the mount") + "\n")
	} else if len(lst) == 0 {
		b.WriteString("  " + styleDim.Render("no containers for this project (stack not created?)") + "\n")
	}
	b.WriteString("\n")
	b.WriteString(a.btnRow("start", "u start", "stop", "d stop", "restart", "r restart", "pull", "p pull"))
	return b.String()
}

// btnRow — the classic [u] start [d] stop … action row, each chip a zone
// click target (the right-pane chips share the strip ids: chip.<name>).
func (a *app) btnRow(pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		b.WriteString(zone.Mark(a.zoneID+".chip."+pairs[i], styleChip.Render("["+strings.TrimPrefix(pairs[i+1], " ")+"] "+pairs[i+1])) + " ")
	}
	return b.String()
}

func (a *app) renderPlan(b *strings.Builder) {
	plan, err := a.tk.Plan()
	if err != nil {
		b.WriteString("  " + styleErr.Render("plan: ") + err.Error() + "\n")
		return
	}
	names := []string{}
	for _, f := range plan.Files {
		names = append(names, shortName(f))
	}
	b.WriteString("  " + styleKvK.Render("files") + " " + fit(strings.Join(names, " + "), 60) + "\n")
	b.WriteString("  " + styleKvK.Render("image") + " " + styleDim.Render(fit(plan.Env["IMAGE"], 50)) + "\n")
	for _, n := range plan.Notes {
		b.WriteString("  " + styleWarn.Render("note") + "  " + fit(n, 56) + "\n")
	}
}

func healthWord(h string) string {
	switch h {
	case "healthy":
		return "healthy"
	case "unhealthy":
		return "unhealthy"
	case "starting":
		return "start…"
	case "":
		return "-"
	}
	return h
}

func (a *app) panelShells() string {
	var b strings.Builder
	b.WriteString(panelHead("shells — exec attach (full-screen)"))
	for i, s := range Shells {
		b.WriteString("  " + styleKvK.Render(digits1(i+1)) + "  " + styleValue.Render(s.Label) + "  " + styleDim.Render(s.Service) + "\n")
	}
	b.WriteString("\n  " + styleDim.Render("enter/1-4 starts the shell · ctrl-z detaches · type `exit` in the shell") + "\n")
	return b.String()
}

func digits1(n int) string {
	if n <= 9 {
		return string(rune('0' + n))
	}
	return fmt.Sprint(n)
}

func (a *app) panelLogs() string {
	var b strings.Builder
	b.WriteString(panelHead("logs — " + a.logName))
	if len(a.logLines) == 0 {
		b.WriteString("  " + styleDim.Render("(no lines — f refreshes when a container is up)") + "\n")
	} else {
		lines := a.logLines
		h := max2(4, a.height-10)
		if len(lines) > h {
			lines = lines[len(lines)-h:]
		}
		for _, l := range lines {
			b.WriteString("  " + fit(strings.TrimRight(l, "\r"), 90) + "\n")
		}
	}
	return b.String()
}

func (a *app) panelSettings() string {
	keys := a.flatKeys()
	var b strings.Builder
	b.WriteString(panelHead("settings — configstore (the one true source)"))
	if a.editKey != "" {
		b.WriteString("  " + styleHead.Render(" EDIT ") + " " + a.editKey)
		if isSecret(a.editKey) {
			b.WriteString("   " + styleWarn.Render("[secret]") + "   " + styleDim.Render("the value is never re-shown") + "\n")
		}
		if a.editMask {
			b.WriteString("  " + styleEdit.Render(" > "+strings.Repeat("•", max2(6, len(a.editVal)))) + styleDim.Render("  (masked)") + "\n")
		} else {
			b.WriteString("  " + styleEdit.Render(" > "+a.editVal) + styleDim.Render(" |") + "\n")
		}
		if strings.TrimSpace(a.editVal) == "" {
			b.WriteString("  " + styleDim.Render("(empty on save = unset → the registry default applies)") + "\n")
		}
		b.WriteString("  " + styleDim.Render("enter/x save · esc cancel") + "\n")
		return b.String()
	}
	groupSeen := map[string]bool{}
	for _, e := range keys {
		if !groupSeen[e.Group] {
			groupSeen[e.Group] = true
			b.WriteString("  " + styleHead.Render(" "+strings.ToUpper(e.Group)+" ") + "\n")
		}
		var val string
		mark := styleDim.Render("○")
		if e.Present {
			mark = styleOK.Render("●")
			val = e.Value
			if isSecret(e.Key) {
				val = "••••••"
			}
		} else if isSecret(e.Key) {
			val = styleDim.Render("(unset)")
		} else if e.Default != "" {
			val = e.Default + styleDim.Render("  (def)")
		} else {
			val = styleDim.Render("(unset)")
		}
		b.WriteString("  " + styleValue.Render(fit(e.Key, 40)) + "  " + fit(val, 40) + "  " + mark + "\n")
	}
	if len(keys) == 0 {
		b.WriteString("  " + styleDim.Render("settings registry empty (configstore not reachable?)") + "\n")
	}
	b.WriteString("\n  " + styleDim.Render("e/enter edits a key · j/k roams · tab? no tab here — the left list owns the other pane") + "\n")
	return b.String()
}

func (a *app) panelActions() string {
	var b strings.Builder
	b.WriteString(panelHead("actions — tls import · n-gram models · first-admin"))
	b.WriteString("  " + styleChip.Render("[t]") + "  Import the TLS certificate + key (two steps: cert path, key path)\n")
	b.WriteString("  " + styleChip.Render("[n]") + "  Download the n-gram models (en,de — empty = all five)\n")
	b.WriteString("  " + styleChip.Render("[l]") + "  n-gram status (local, no network)\n")
	b.WriteString("  " + styleChip.Render("[b]") + "  Bootstrap the first admin (email, then password)\n")
	if a.actResult != "" {
		b.WriteString("\n" + a.actResult + "\n")
	}
	return b.String()
}

func (a *app) panelDoctor() string {
	var b strings.Builder
	b.WriteString(panelHead("doctor — health checks"))
	for _, r := range a.doctorRows {
		b.WriteString("  " + okText(r.ok) + "  " + fit(r.label, 16) + "  " + styleDim.Render(r.detail) + "\n")
	}
	if len(a.doctorRows) == 0 {
		b.WriteString("\n  " + styleDim.Render("(press r or enter on Doctor to run the checks)") + "\n")
	}
	return b.String()
}

func (a *app) panelBackup() string {
	var b strings.Builder
	b.WriteString(panelHead("backup — the config-store snapshot"))
	b.WriteString("  " + styleKvK.Render("file") + " " + a.tk.BackupPath() + "\n")
	b.WriteString("  " + styleKvK.Render("scope") + " " + styleDim.Render("every stored key (the true source of the settings plane)") + "\n")
	if _, err := storeFileExists(a.tk.BackupPath()); err != nil {
		b.WriteString("  " + styleKvK.Render("state") + " " + styleDim.Render("(not written yet)") + "\n")
	} else {
		b.WriteString("  " + styleKvK.Render("state") + " " + styleOK.Render("present") + "\n")
	}
	b.WriteString("\n")
	b.WriteString(a.btnRow("backup", "b backup", "restore", "r restore"))
	return b.String()
}

func storeFileExists(p string) (bool, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return false, err
	}
	return !fi.IsDir(), nil
}

func (a *app) panelAbout() string {
	var b strings.Builder
	b.WriteString(panelHead("about"))
	rows := [][2]string{
		{"build", a.tk.Ver},
		{"project", a.tk.Project},
		{"compose", a.tk.ComposeFile},
		{"docker", a.tk.DockerSocket},
		{"data dir", a.tk.DataDir},
		{"configstore", a.tk.StoreDescribe},
	}
	for _, r := range rows {
		b.WriteString("  " + styleKvK.Render(r[0]) + " " + fit(r[1], 60) + "\n")
	}
	b.WriteString("\n  " + styleDim.Render("console layout: the classic two-pane (this one) + the menu bar + the keystrip.") + "\n")
	b.WriteString("  " + styleDim.Render("TUI stack: bubbletea · menubar (jejacks0n) · overlay (rmhubbert) · zone (lrstanley) — MIT, CREDITS.md") + "\n")
	return b.String()
}

// ---- the shell full-screen (unchanged contract) -----------------------------

func (a *app) viewShell() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(" " + stylePaneTitle.Render(" Shell — "+a.shellLabel) + styleDim.Render("   ctrl-z detach · ctrl+c → SIGINT"))
	if a.shellExited {
		b.WriteString("\n  " + styleErr.Render("session ended"))
	}
	tail := a.shellBuf
	if len(tail) > 6000 {
		tail = tail[len(tail)-6000:]
	}
	for _, ln := range strings.SplitAfter(string(tail), "\n") {
		b.WriteString("  " + ln)
	}
	b.WriteString("\n")
	return b.String()
}

// (dialog surface: ui_dialog.go · menus: ui_menu.go)
