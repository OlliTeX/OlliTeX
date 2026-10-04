package toolkit

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"ollitex/go/libraries/configschema"

	tea "github.com/charmbracelet/bubbletea"
)

// ---- messages --------------------------------------------------------------

type jobMsg struct {
	job  string
	out  string
	err  error
	done bool
}

// ---- app -------------------------------------------------------------------

type screenKind int

const (
	scrDashboard screenKind = iota
	scrSettings
	scrStack
	scrLogs
	scrDoctor
	scrBackup
	scrAbout
)

const maxLines = 400

type doctorRow struct {
	label  string
	detail string
	ok     bool
}

type app struct {
	tk   *Toolkit
	dock *Docker
	set  *Settings

	kind   screenKind
	dcur   int
	width  int
	height int

	statusMsg string
	errMsg    string

	loading bool
	jobName string

	// logs
	logLines []string
	logName  string

	// settings
	setGroups []Group
	setCurSel int
	editKey   string
	editVal   string

	// doctor
	doctorRows []doctorRow
}

func newApp(t *Toolkit) *app {
	d, derr := NewDocker(t.DockerSocket)
	a := &app{
		tk:     t,
		set:    newSettings(t),
		kind:   scrDashboard,
		width:  80,
		height: 24,
	}
	if derr != nil {
		a.dock = nil
		a.errMsg = "docker: " + derr.Error()
	} else {
		a.dock = d
	}
	return a
}

// Init (tea.Model)
func (a *app) Init() tea.Cmd {
	return a.refreshActive()
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (a *app) setErr(f string, args ...any) { a.errMsg = fmt.Sprintf(f, args...) }

func tail(out string) string {
	out = strings.TrimSpace(out)
	const n = 200
	if out == "" {
		return ""
	}
	if len(out) <= n {
		return " — " + out
	}
	return " — …" + out[len(out)-n:]
}

func (a *app) runJob(job string, f func(ctx context.Context) (string, error)) tea.Cmd {
	a.loading = true
	a.jobName = job
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		out, err := f(ctx)
		return jobMsg{job: job, out: out, err: err, done: true}
	}
}

func (a *app) silentJob(job string, f func(ctx context.Context) (string, error)) tea.Cmd {
	a.loading = true
	a.jobName = job
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		out, err := f(ctx)
		return jobMsg{job: job, out: out, err: err, done: true}
	}
}

// refreshActive runs the refresh cmd for the current screen.
func (a *app) refreshActive() tea.Cmd {
	switch a.kind {
	case scrDashboard, scrStack:
		return a.refreshStatus()
	case scrLogs:
		return a.refreshLogs("")
	case scrDoctor:
		return a.runDoctor()
	case scrSettings:
		a.loadSettings()
		return nil
	}
	return nil
}

// ---- Update ----------------------------------------------------------------

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
		return a, nil

	case jobMsg:
		a.loading = false
		if m.err != nil {
			a.setErr("%s: %v%s", m.job, m.err, tail(m.out))
			return a, nil
		}
		switch m.job {
		case "ping":
			a.errMsg = ""
			a.statusMsg = ""
			return a, nil
		case "start", "stop", "restart":
			a.statusMsg = m.job + " done" + tail(m.out)
			return a, a.refreshStatus()
		case "pull":
			a.statusMsg = "images pulled" + tail(m.out)
			return a, a.refreshStatus()
		case "backup":
			a.statusMsg = "backup saved → " + a.tk.BackupPath() + tail(m.out)
			return a, nil
		case "restore":
			a.statusMsg = "restore done" + tail(m.out)
			return a, nil
		case "logs":
			a.errMsg = ""
			a.statusMsg = ""
			return a, nil
		default:
			a.statusMsg = m.job + " done" + tail(m.out)
			return a, nil
		}

	case tea.KeyMsg:
		return a.handleKey(m)
	}
	return a, nil
}

func (a *app) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if a.kind != scrDashboard {
			a.kind = scrDashboard
			a.loading = false
			a.editKey = ""
			return a, a.refreshActive()
		}
		return a, tea.Quit
	case "esc":
		if a.editKey != "" {
			a.editKey = ""
			return a, nil
		}
		if a.kind != scrDashboard {
			a.kind = scrDashboard
			a.loading = false
			return a, a.refreshActive()
		}
	case "q":
		if a.editKey != "" {
			a.editKey = ""
			return a, nil
		}
		if a.kind != scrDashboard {
			a.kind = scrDashboard
			a.loading = false
			return a, a.refreshActive()
		}
		return a, tea.Quit
	case ".":
		a.editKey = ""
		return a, a.refreshActive()
	case "?":
		a.kind = scrAbout
		a.editKey = ""
		return a, nil
	}
	switch a.kind {
	case scrDashboard:
		return a.dashKey(msg)
	case scrSettings:
		return a.settingsKey(msg)
	case scrStack:
		return a.stackKey(msg)
	case scrLogs:
		return a.logsKey(msg)
	case scrDoctor:
		if msg.String() == "r" {
			return a, a.runDoctor()
		}
	case scrBackup:
		return a.backupKey(msg)
	}
	return a, nil
}

// ---- status / dashboard ----------------------------------------------------

type dashItem struct {
	label string
	meta  string
	cmd   func(*app) (tea.Model, tea.Cmd)
}

func (a *app) stackCount() (up, total int) {
	if a.dock == nil {
		return 0, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	lst, err := a.dock.ListProject(ctx, a.tk.Project)
	if err != nil {
		return 0, 0
	}
	for _, c := range lst {
		total++
		if c.Up {
			up++
		}
	}
	return up, total
}

func (a *app) containerList() []Status {
	if a.dock == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	lst, err := a.dock.ListProject(ctx, a.tk.Project)
	if err != nil {
		return nil
	}
	return lst
}

func (a *app) firstLoggable() string {
	for _, c := range a.containerList() {
		if c.Up {
			return c.Name
		}
	}
	if l := a.containerList(); len(l) > 0 {
		return l[0].Name
	}
	return ""
}

func (a *app) refreshStatus() tea.Cmd {
	if a.dock == nil {
		return nil
	}
	return a.silentJob("ping", func(ctx context.Context) (string, error) {
		if err := a.dock.Ping(ctx); err != nil {
			return "", err
		}
		if _, err := a.dock.ListProject(ctx, a.tk.Project); err != nil {
			return "", err
		}
		return "ok", nil
	})
}

func (a *app) dashItems() []dashItem {
	up, total := a.stackCount()
	return []dashItem{
		{"Stack", fmt.Sprintf("%d/%d up", up, total), func(a *app) (tea.Model, tea.Cmd) {
			a.kind = scrStack
			return a, a.refreshStatus()
		}},
		{"Logs", "tail container logs", func(a *app) (tea.Model, tea.Cmd) {
			a.kind = scrLogs
			return a, a.refreshLogs("")
		}},
		{"Settings", "configstore — one true source", func(a *app) (tea.Model, tea.Cmd) {
			a.kind = scrSettings
			a.loadSettings()
			return a, nil
		}},
		{"Doctor", "docker / store / stack health", func(a *app) (tea.Model, tea.Cmd) {
			a.kind = scrDoctor
			return a, a.runDoctor()
		}},
		{"Backup", "dump / restore the config store", func(a *app) (tea.Model, tea.Cmd) {
			a.kind = scrBackup
			return a, nil
		}},
		{"About", "build + planes", func(a *app) (tea.Model, tea.Cmd) {
			a.kind = scrAbout
			return a, nil
		}},
	}
}

func (a *app) dashKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := a.dashItems()
	n := len(items)
	switch msg.String() {
	case "down", "j":
		a.dcur = (a.dcur + 1) % n
	case "up", "k":
		a.dcur = (a.dcur - 1 + n) % n
	case "enter", " ":
		return items[a.dcur].cmd(a)
	case "1":
		return items[0].cmd(a)
	case "2":
		return items[1].cmd(a)
	case "3":
		return items[2].cmd(a)
	case "4":
		return items[3].cmd(a)
	case "5":
		return items[4].cmd(a)
	case "6":
		return items[5].cmd(a)
	case "s":
		a.kind = scrStack
		return a, a.runJob("start", a.tk.StackUp)
	case "t":
		a.kind = scrStack
		return a, a.runJob("stop", func(ctx context.Context) (string, error) { return a.tk.StackDown(ctx, false) })
	}
	return a, nil
}

func (a *app) View() string {
	switch a.kind {
	case scrSettings:
		return a.viewSettings()
	case scrStack:
		return a.viewStack()
	case scrLogs:
		return a.viewLogs()
	case scrDoctor:
		return a.viewDoctor()
	case scrBackup:
		return a.viewBackup()
	case scrAbout:
		return a.viewAbout()
	}
	return a.viewDashboard()
}

func (a *app) footer() string {
	out := ""
	if a.errMsg != "" {
		out += "\n" + styleErr.Render("✗ ") + a.errMsg
	}
	if a.statusMsg != "" {
		out += "\n" + a.statusMsg
	}
	if a.loading {
		out += "\n" + styleWarn.Render("⠋ ") + a.jobName + "…"
	}
	if a.kind != scrDashboard {
		out += "\n" + hintBar("q/esc", "back", ".", "refresh", "?", "about")
	} else {
		out += "\n" + hintBar("j/k", "move", "enter", "open", "1-5", "screens", "s", "start", "t", "stop", "q", "quit")
	}
	return out
}

func (a *app) viewDashboard() string {
	up, total := a.stackCount()
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(titleBar("OlliTeX Toolkit"))
	b.WriteString("\n")
	b.WriteString("\n" + styleHeader.Render("  Stack"))
	if a.dock == nil {
		b.WriteString("\n  " + styleErr.Render("docker socket unavailable"))
	} else {
		state, scol := "down", styleErr
		if up > 0 {
			state, scol = "up", styleOK
		}
		b.WriteString("\n  " + scol.Render(state) + "  " + a.tk.Project +
			styleDim.Render(fmt.Sprintf("  %d/%d containers up", up, total)))
	}
	b.WriteString("\n" + styleHeader.Render("  Config store"))
	if a.tk.Store != nil {
		b.WriteString("\n  " + okText(true) + "  " + a.tk.StoreDescribe)
	} else {
		b.WriteString("\n  " + styleErr.Render("not configured"))
	}
	b.WriteString("\n\n")
	for i, it := range a.dashItems() {
		b.WriteString(renderListRow(i, a.dcur, it.label, it.meta) + "\n")
	}
	b.WriteString(a.footer())
	return b.String()
}

// ---- stack -----------------------------------------------------------------

func (a *app) viewStack() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(titleBar("Stack — " + a.tk.Project))
	b.WriteString("\n")
	a.renderPlan(&b)
	lst := a.containerList()
	if a.dock == nil {
		b.WriteString("\n  " + styleErr.Render("docker socket unavailable — check the mount"))
	} else if len(lst) == 0 {
		b.WriteString("\n  " + styleDim.Render("no containers for this project (stack not created?)"))
	} else {
		b.WriteString("\n  " + styleKvK.Render("NAME") + " " +
			styleKvK.Render("STATE") + " " +
			styleKvK.Render("HEALTH") + " " +
			styleKvK.Render("PORTS") + "\n")
		for _, c := range lst {
			state, col := "down", styleErr
			if c.Up {
				state, col = "up ", styleOK
			}
			b.WriteString("  " + fit(c.Name, 28) + " " +
				col.Render(fit(state, 5)) + " " +
				styleDim.Render(fit(healthWord(c.Health), 9)) + " " +
				styleDim.Render(fit(c.Ports, 18)) + " " +
				styleDim.Render(fit(c.Image, 18)) + "\n")
		}
	}
	b.WriteString("\n  " + styleKey.Render("u") + " start   " +
		styleKey.Render("d") + " stop   " +
		styleKey.Render("r") + " restart   " +
		styleKey.Render("p") + " pull images" + a.footer())
	return b.String()
}

// renderPlan shows the store-rendered overlay/env plan (the exact thing the
// stack actions will run).
func (a *app) renderPlan(b *strings.Builder) {
	plan, err := a.tk.Plan()
	if err != nil {
		b.WriteString("\n  " + styleErr.Render("plan: ") + err.Error())
		return
	}
	names := []string{}
	for _, f := range plan.Files {
		names = append(names, shortName(f))
	}
	b.WriteString("\n  " + styleHeader.Render("plan") + "  " +
		fit(strings.Join(names, " + "), a.width-10))
	b.WriteString("\n  " + styleKvK.Render("image") + " " + styleDim.Render(fit(plan.Env["IMAGE"], a.width-40)))
	b.WriteString("\n  " + styleKvK.Render("env file") + " " + styleDim.Render(plan.EnvFile))
	for _, n := range plan.Notes {
		b.WriteString("\n  " + styleWarn.Render("note") + "  " + fit(n, a.width-12))
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

func (a *app) stackKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "u", "s":
		return a, a.runJob("start", a.tk.StackUp)
	case "d", "t":
		return a, a.runJob("stop", func(ctx context.Context) (string, error) { return a.tk.StackDown(ctx, false) })
	case "r":
		return a, a.runJob("restart", a.tk.StackRestart)
	case "p":
		return a, a.runJob("pull", a.tk.PullImages)
	}
	return a, nil
}

// ---- settings --------------------------------------------------------------

func (a *app) loadSettings() {
	a.setGroups, _ = a.set.Groups()
	if a.setCurSel >= len(a.flatKeys()) {
		a.setCurSel = 0
	}
}

func (a *app) flatKeys() []Entry {
	var out []Entry
	for _, g := range a.setGroups {
		out = append(out, g.Keys...)
	}
	return out
}

func isSecret(key string) bool {
	p, ok := configschema.Find(key)
	return ok && p.Secret
}

func (a *app) settingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.editKey != "" {
		switch msg.String() {
		case "backspace", "delete":
			if len(a.editVal) > 0 {
				a.editVal = a.editVal[:len(a.editVal)-1]
			}
			return a, nil
		case "enter", "x", "ctrl+s":
			key := a.editKey
			val := strings.TrimSpace(a.editVal)
			a.editKey = ""
			if val == "" {
				return a, a.runJob("unset", func(ctx context.Context) (string, error) {
					if err := a.set.Delete(key, "toolkit-tui"); err != nil {
						return "", err
					}
					a.loadSettings()
					return key, nil
				})
			}
			return a, a.runJob("set", func(ctx context.Context) (string, error) {
				if err := a.set.Set(key, val, "toolkit-tui"); err != nil {
					return "", err
				}
				a.loadSettings()
				return key, nil
			})
		}
		if len(msg.Runes) > 0 {
			a.editVal += string(msg.Runes)
		}
		return a, nil
	}
	keys := a.flatKeys()
	n := max2(1, len(keys))
	switch msg.String() {
	case "down", "j":
		if len(keys) > 0 {
			a.setCurSel = (a.setCurSel + 1) % len(keys)
		}
	case "up", "k":
		if len(keys) > 0 {
			a.setCurSel = (a.setCurSel - 1 + n) % len(keys)
		}
	case "enter", "e":
		if len(keys) == 0 {
			return a, nil
		}
		e := keys[a.setCurSel]
		v := e.Value
		if e.Secret && e.Present {
			if real, present, _ := a.set.Reveal(e.Key); present {
				v = real
			}
		}
		a.editKey = e.Key
		a.editVal = v
		return a, nil
	}
	return a, nil
}

func (a *app) viewSettings() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(titleBar("Settings — configstore (one true source)"))
	keys := a.flatKeys()
	if a.editKey != "" {
		b.WriteString("\n  " + styleHeader.Render("edit") + " " + a.editKey)
		if isSecret(a.editKey) {
			b.WriteString("  " + styleWarn.Render("[secret]"))
		}
		b.WriteString("\n  > " + a.editVal + "|")
		if strings.TrimSpace(a.editVal) == "" {
			b.WriteString(" " + styleDim.Render("(empty = unset → registry default)"))
		}
		b.WriteString("\n\n" + hintBar("enter/x", "save", "backspace", "edit", "esc/q", "cancel"))
		return b.String()
	}
	groupSeen := map[string]bool{}
	for i, e := range keys {
		if !groupSeen[e.Group] {
			groupSeen[e.Group] = true
			b.WriteString("\n  " + styleHeader.Render(strings.ToUpper(e.Group)))
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
		row := fit(e.Key, 38) + "  " + fit(val, max2(10, a.width-60)) + "  " + mark
		if i == a.setCurSel {
			b.WriteString(styleSelected.Render(">") + styleSelected.Render(" "+row) + "\n")
		} else {
			b.WriteString("   " + row + "\n")
		}
	}
	if len(keys) == 0 {
		b.WriteString("\n  " + styleDim.Render("settings registry empty"))
	}
	b.WriteString("\n" + hintBar("j/k", "move", "enter", "edit", "e", "reveal+edit", "q", "back"))
	return b.String()
}

// ---- logs ------------------------------------------------------------------

func (a *app) refreshLogs(name string) tea.Cmd {
	if name == "" {
		name = a.firstLoggable()
	}
	a.logName = name
	if a.dock == nil || name == "" {
		return nil
	}
	return a.silentJob("logs", func(ctx context.Context) (string, error) {
		st, err := a.dock.TailLog(ctx, name, maxLines)
		if err != nil {
			return "", err
		}
		defer st.Close()
		var lines []string
		for len(lines) < maxLines {
			ok, line := st.ScanLog()
			if !ok {
				break
			}
			lines = append(lines, line)
		}
		a.logLines = lines
		return fmt.Sprintf("%d lines from %s", len(lines), name), nil
	})
}

func (a *app) logsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "f", "enter":
		return a, a.refreshLogs(a.logName)
	case "left", "h", "right", "l":
		dir := 1
		if msg.String() == "left" || msg.String() == "h" {
			dir = -1
		}
		lst := a.containerList()
		if len(lst) == 0 {
			return a, nil
		}
		idx := -1
		for i, c := range lst {
			if c.Name == a.logName {
				idx = i
				break
			}
		}
		if idx < 0 {
			idx = 0
		}
		next := idx
		for step := 0; step < len(lst); step++ {
			next = (idx + dir*(step+1)) % len(lst)
			if next < 0 {
				next += len(lst)
			}
			if lst[next].Up {
				break
			}
			if step == len(lst)-1 {
				next = idx // none up — keep current
			}
		}
		return a, a.refreshLogs(lst[next].Name)
	case "pgdown":
		return a, a.refreshLogs(a.logName)
	}
	return a, nil
}

func (a *app) viewLogs() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(titleBar("Logs") + styleDim.Render("  "+a.logName))
	if len(a.logLines) == 0 {
		b.WriteString("\n  " + styleDim.Render("(no lines — press f to refresh)"))
	} else {
		lines := a.logLines
		h := max2(4, a.height-8)
		if len(lines) > h {
			lines = lines[len(lines)-h:]
		}
		for _, l := range lines {
			b.WriteString("\n  " + fit(strings.TrimRight(l, "\r"), a.width-4))
		}
	}
	b.WriteString("\n" + hintBar("h/l", "switch container", "f", "refresh", "q", "back"))
	return b.String()
}

// ---- doctor ----------------------------------------------------------------

func (a *app) runDoctor() tea.Cmd {
	return a.silentJob("doctor", func(ctx context.Context) (string, error) {
		var rows []doctorRow
		if a.dock != nil {
			if err := a.dock.Ping(ctx); err == nil {
				rows = append(rows, doctorRow{label: "docker socket", detail: "daemon reachable", ok: true})
			} else {
				rows = append(rows, doctorRow{label: "docker socket", detail: "daemon unreachable: " + err.Error(), ok: false})
			}
		} else {
			rows = append(rows, doctorRow{label: "docker socket", detail: "client failed to init (check mount)", ok: false})
		}
		if a.tk.Store != nil {
			if _, err := a.tk.Store.Keys(); err == nil {
				rows = append(rows, doctorRow{label: "config store", detail: a.tk.StoreDescribe, ok: true})
			} else {
				rows = append(rows, doctorRow{label: "config store", detail: "open/read failed", ok: false})
			}
		} else {
			rows = append(rows, doctorRow{label: "config store", detail: "not configured (set CONFIG_DB_DSN)", ok: false})
		}
		if fi, err := os.Stat(a.tk.ComposeFile); err == nil && !fi.IsDir() {
			rows = append(rows, doctorRow{label: "compose file", detail: a.tk.ComposeFile, ok: true})
		} else {
			rows = append(rows, doctorRow{label: "compose file", detail: a.tk.ComposeFile + " (missing)", ok: false})
		}
		if fi, err := os.Stat(a.tk.DataDir); err == nil && fi.Mode().Perm()&0o200 != 0 {
			rows = append(rows, doctorRow{label: "data dir", detail: a.tk.DataDir + " (writable)", ok: true})
		} else {
			rows = append(rows, doctorRow{label: "data dir", detail: a.tk.DataDir + " (missing/not writable)", ok: false})
		}
		a.doctorRows = rows
		return "done", nil
	})
}

func (a *app) viewDoctor() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(titleBar("Doctor"))
	for _, r := range a.doctorRows {
		b.WriteString("\n  " + okText(r.ok) + "  " + fit(r.label, 16) + "  " + styleDim.Render(r.detail))
	}
	if len(a.doctorRows) == 0 {
		b.WriteString("\n\n  " + styleDim.Render("press r to run the checks"))
	}
	b.WriteString("\n\n" + hintBar("r", "re-run", "q", "back"))
	return b.String()
}

// ---- backup ----------------------------------------------------------------

func (a *app) backupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "b", "enter":
		return a, a.runJob("backup", func(ctx context.Context) (string, error) {
			if a.tk.Store == nil {
				return "", context.Canceled
			}
			m, err := a.tk.Store.Dump(a.tk.BackupPath())
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%d keys", len(m)), nil
		})
	case "r":
		return a, a.runJob("restore", func(ctx context.Context) (string, error) {
			if a.tk.Store == nil {
				return "", context.Canceled
			}
			n, err := a.tk.Store.Restore(a.tk.BackupPath())
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%d keys", n), nil
		})
	}
	return a, nil
}

func (a *app) viewBackup() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(titleBar("Backup & restore"))
	b.WriteString("\n\n  snapshot file: " + a.tk.BackupPath())
	b.WriteString("\n\n  " + styleKey.Render("b") + "  backup  — dump the whole store to JSON")
	b.WriteString("\n  " + styleKey.Render("r") + "  restore — load that JSON back (upsert)")
	b.WriteString(a.footer())
	return b.String()
}

// ---- about -----------------------------------------------------------------

func (a *app) viewAbout() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(titleBar("About"))
	b.WriteString("\n")
	rows := [][2]string{
		{"build", a.tk.Ver},
		{"project", a.tk.Project},
		{"compose", a.tk.ComposeFile},
		{"docker", a.tk.DockerSocket},
		{"data dir", a.tk.DataDir},
		{"configstore", a.tk.StoreDescribe},
	}
	for _, r := range rows {
		b.WriteString("\n  " + styleKvK.Render(r[0]) + " " + fit(r[1], a.width-30))
	}
	b.WriteString("\n\n  " + styleDim.Render("host contract: docker + one mounted folder + the docker socket."))
	b.WriteString("\n  " + styleDim.Render("settings plane: configstore (Postgres) — no docker-env fallbacks."))
	b.WriteString(a.footer())
	return b.String()
}
