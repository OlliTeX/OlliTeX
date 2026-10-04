package toolkit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	scrShell
	scrActions
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

	// shell
	shell       *ShellSession
	shellLabel  string
	shellBuf    []byte
	shellExited bool

	// actions (admin: TLS import + n-gram models)
	actPrompt string // active prompt text ("" = no prompt)
	actBuffer string // typed input for the active prompt
	actResult string // last action result (shown on the Actions screen)
	actCert   string // captured cert path (TLS import, 2-step)
	actKey    string
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

// shellChunkMsg carries one decoded exec chunk into the model.
type shellChunkMsg struct {
	text string
	eof  bool
}

// pumpShell blocks on the exec stream and delivers one decoded chunk.
func (a *app) pumpShell() tea.Cmd {
	sess := a.shell
	if sess == nil {
		return nil
	}
	return func() tea.Msg {
		buf := make([]byte, 256*1024)
		n, err := sess.ReadOne(buf)
		if n > 0 {
			return shellChunkMsg{text: DecodeShellChunk(buf[:n]), eof: err != nil}
		}
		return shellChunkMsg{eof: err != nil}
	}
}

func (a *app) endShell() {
	if a.shell != nil {
		a.shell.Close()
		a.shell = nil
	}
}

// shellStartMsg is delivered when a shell action completes.
type shellStartMsg struct {
	label string
	err   error
}

func (a *app) startShell(label string) tea.Cmd {
	dk, tk := a.dock, a.tk
	a.loading = true
	a.jobName = "shell " + label
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		sess, err := NewShell(ctx, dk, tk.Project, label)
		if err == nil {
			a.shell = sess
			a.shellLabel = label
			a.shellBuf = nil
			a.shellExited = false
		}
		return shellStartMsg{label: label, err: err}
	}
}

// ---- Update ----------------------------------------------------------------

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
		return a, nil

	case shellStartMsg:
		a.loading = false
		if m.err != nil {
			a.setErr("shell %s: %v", m.label, m.err)
			return a, nil
		}
		a.kind = scrShell
		return a, a.pumpShell()

	case shellChunkMsg:
		a.shellBuf = append(a.shellBuf, []byte(m.text)...)
		if len(a.shellBuf) > 1<<20 { // 1 MiB cap: keep the tail
			a.shellBuf = a.shellBuf[len(a.shellBuf)-(1<<20):]
		}
		if m.eof {
			a.shellExited = true
			a.endShell()
			return a, nil
		}
		return a, a.pumpShell()

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
		if a.kind == scrShell && a.shell != nil {
			return a.shellKey(m)
		}
		return a.handleKey(m)
	}
	return a, nil
}

// shellKey forwards operator input to the exec stream.
//
//	ctrl-c → to the shell (SIGINT) · ctrl-z → detach & close (back to menu)
//	q      → to the shell (type `exit` in the app shell)
func (a *app) shellKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	w := func(s string) tea.Cmd {
		_, err := a.shell.Write([]byte(s))
		if err != nil {
			a.setErr("shell write: %v", err)
			a.shellExited = true
			a.endShell()
		}
		return nil
	}
	code := func(c byte) string { return string(c) }
	switch msg.String() {
	case "ctrl+z":
		a.endShell()
		a.kind = scrDashboard
		return a, a.refreshActive()
	case "ctrl+c":
		return a, w(code(3))
	case "ctrl+d":
		return a, w(code(4))
	case "ctrl+u":
		return a, w(code(21))
	case "ctrl+l":
		return a, w(code(12))
	case "enter":
		return a, w(code(13))
	case "backspace", "delete":
		return a, w(code(127))
	case "left":
		return a, w("\x1b[D")
	case "right":
		return a, w("\x1b[C")
	case "up":
		return a, w("\x1b[A")
	case "down":
		return a, w("\x1b[B")
	case "tab":
		return a, w(code(9))
	case "shift+tab":
		return a, w("\x1b[Z")
	case "esc":
		return a, w(code(27))
	case "home":
		return a, w("\x1b[H")
	case "end":
		return a, w("\x1b[F")
	case "pgup":
		return a, w("\x1b[5~")
	case "pgdown":
		return a, w("\x1b[6~")
	default:
		if len(msg.Runes) > 0 {
			return a, w(string(msg.Runes))
		}
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
	case scrActions:
		return a.actionsKey(msg)
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
		{"Shells", "mongo / postgres / app exec", func(a *app) (tea.Model, tea.Cmd) {
			return a, a.startShell(Shells[0].Label)
		}},
		{"Actions", "TLS import · n-gram models", func(a *app) (tea.Model, tea.Cmd) {
			a.kind = scrActions
			a.actResult = ""
			a.actPrompt = ""
			return a, nil
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

// viewShell renders the live exec feed.
func (a *app) viewShell() string {
	var b strings.Builder
	b.WriteString("\n")
	bar := titleBar("Shell — " + a.shellLabel)
	b.WriteString(bar)
	if a.shellExited {
		b.WriteString("\n")
		b.WriteString(styleErr.Render("  session ended"))
	}
	tail := a.shellBuf
	if len(tail) > 6000 {
		tail = tail[len(tail)-6000:]
	}
	for _, ln := range strings.SplitAfter(string(tail), "\n") {
		b.WriteString("  " + ln)
	}
	b.WriteString("\n")
	b.WriteString(hintBar("ctrl-c", "to shell", "ctrl-z", "detach & close", "type", "exit / q"))
	return b.String()
}

func (a *app) View() string {
	switch a.kind {
	case scrSettings:
		return a.viewSettings()
	case scrStack:
		return a.viewStack()
	case scrLogs:
		return a.viewLogs()
	case scrShell:
		return a.viewShell()
	case scrActions:
		return a.viewActions()
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
	case "m":
		return a, a.startShell(Shells[0].Label)
	case "g":
		return a, a.startShell(Shells[1].Label)
	case "e":
		return a, a.startShell(Shells[2].Label)
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

// ---- actions (admin): TLS import + n-gram models ---------------------------

// ngramStatusText lists the current models on disk (no network).
func (a *app) ngramStatusText() string {
	d := a.tk.DataDir
	var b strings.Builder
	b.WriteString("  lang   archive                state          size\n")
	for _, lang := range NgramCodes() {
		arch := NgramArchives[lang]
		z := filepath.Join(d, "ngrams", fmt.Sprintf("ngrams-%s.zip", lang))
		dir := filepath.Join(d, "ngrams", lang)
		_, zerr := os.Stat(z)
		_, derr := os.Stat(dir)
		state := "missing"
		var size int64
		if zerr == nil && derr == nil {
			state = "present"
			size = fileSize(z)
		} else if zerr == nil {
			state = "archive only"
			size = fileSize(z)
		} else if derr == nil {
			state = "extracted only"
		}
		b.WriteString(fmt.Sprintf("  %-4s   %-20s   %-12s   %s\n", lang, arch, state, HumanBytes(size)))
	}
	b.WriteString(fmt.Sprintf("\n  dir: %s/ngrams (mounted read-only into the languagetool service)\n", d))
	return b.String()
}

// runNgram downloads/verifies the requested languages into the DATA DIR
// (owner layout: <data>/ngrams/<lang> + stable ngrams-<lang>.zip). Job
// output = one line per language.
func (a *app) runNgram(langs string) tea.Cmd {
	t := a.tk
	return a.runJob("ngram", func(ctx context.Context) (string, error) {
		parts := []string{}
		for _, p := range strings.Split(langs, ",") {
			if s2 := strings.TrimSpace(p); s2 != "" {
				parts = append(parts, s2)
			}
		}
		if len(parts) == 0 {
			parts = NgramCodes()
		}
		sts, err := NgramDownload(ctx, t.DataDir, parts)
		var b strings.Builder
		for _, st := range sts {
			b.WriteString(fmt.Sprintf("  %-4s  %-16s  %s\n", st.Language, st.Action, st.Path))
		}
		if err != nil {
			return b.String(), err
		}
		b.WriteString(fmt.Sprintf("\n  (dir: %s/ngrams — restart the languagetool service to pick up new models)\n", t.DataDir))
		return b.String(), nil
	})
}

// runTLS copies the owner's cert+key into <data>/nginx/tls, points the store
// keys at them, and restarts nginx once (when the service is present).
func (a *app) runTLS(certSrc, keySrc string) tea.Cmd {
	t, dk := a.tk, a.dock
	return a.runJob("tls", func(ctx context.Context) (string, error) {
		certDst := filepath.Join(t.DataDir, "nginx", "tls", "nginx_certificate.pem")
		keyDst := filepath.Join(t.DataDir, "nginx", "tls", "nginx_key.pem")
		info, err := ImportCert(certDst, keyDst, certSrc, keySrc)
		if err != nil {
			return "", err
		}
		if serr := t.Store.Set("TLS_CERTIFICATE_PATH", certDst, "toolkit:tls-import"); serr != nil {
			return "", serr
		}
		if serr := t.Store.Set("TLS_PRIVATE_KEY_PATH", keyDst, "toolkit:tls-import"); serr != nil {
			return "", serr
		}
		_ = info
		var b strings.Builder
		b.WriteString(fmt.Sprintf("  cert  \u2192 %s\n  key   \u2192 %s\n  store: TLS_CERTIFICATE_PATH / TLS_PRIVATE_KEY_PATH updated\n", certDst, keyDst))
		if dk != nil {
			if rerr := dk.RestartOne(ctx, t.Project, "nginx"); rerr != nil {
				b.WriteString("  nginx restart: " + rerr.Error() + " (restart it manually when up)\n")
			} else {
				b.WriteString("  nginx: restarted with the new pair\n")
			}
		}
		return b.String(), nil
	})
}

// actionsKey — the Actions screen flow:
//
//	t  → TLS import (prompt: certificate path, then key path)
//	n  → n-gram models (prompt: "en,de" or empty = all five)
//	l  → n-gram status (instant, no network)
//	esc/q → back to the dashboard
//
// Prompt mode: type the path, enter confirms (TLS collects two prompts).
func (a *app) actionsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.actPrompt == "" {
		switch msg.String() {
		case "t":
			a.actCert, a.actKey = "", ""
			a.actPrompt = "enter the HOST path of the TLS certificate (file), then enter:"
			return a, nil
		case "n":
			a.actPrompt = `languages (en,de — empty = all five), then enter:`
			return a, nil
		case "l":
			a.actResult = a.ngramStatusText()
			return a, nil
		case "esc", "q":
			a.kind = scrDashboard
			return a, a.refreshActive()
		}
		return a, nil
	}
	p := a.actPrompt
	switch msg.String() {
	case "enter":
		v := strings.TrimSpace(a.actBuffer)
		a.actBuffer = ""
		if strings.HasPrefix(p, "enter the HOST path") {
			a.actCert = v
			a.actPrompt = "now the HOST path of the TLS private key (file), then enter:"
			return a, nil
		}
		if strings.HasPrefix(p, "now the HOST path") {
			a.actKey = v
			a.actPrompt = ""
			return a, a.runTLS(a.actCert, a.actKey)
		}
		if strings.HasPrefix(p, "languages") {
			a.actPrompt = ""
			return a, a.runNgram(v)
		}
	case "backspace":
		if len(a.actBuffer) > 0 {
			a.actBuffer = a.actBuffer[:len(a.actBuffer)-1]
		}
		return a, nil
	case "esc":
		a.actPrompt = ""
		a.actBuffer = ""
		return a, nil
	default:
		if len(msg.Runes) > 0 && msg.Runes[0] >= 32 && msg.Runes[0] != 127 {
			a.actBuffer += string(msg.Runes[0])
		}
	}
	return a, nil
}

// viewActions renders the Actions screen.
func (a *app) viewActions() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(titleBar("Actions — TLS import \u00b7 n-gram models"))
	b.WriteString("\n")
	b.WriteString("  " + styleDim.Render("t") + "  Import TLS cert + key  (from owner files \u2192 " + a.tk.DataDir + "/nginx/tls, store keys updated, nginx restarted)")
	b.WriteString("\n")
	b.WriteString("  " + styleDim.Render("n") + "  Download n-gram models  (official dated archives \u2192 " + a.tk.DataDir + "/ngrams)")
	b.WriteString("\n")
	b.WriteString("  " + styleDim.Render("l") + "  n-gram status (local, no network)")
	b.WriteString("\n")
	b.WriteString("  " + styleDim.Render("esc") + "  back")
	b.WriteString("\n")
	if a.actPrompt != "" {
		b.WriteString("\n")
		b.WriteString(styleWarn.Render("  \u25ac " + a.actPrompt))
		b.WriteString("\n")
		b.WriteString("  " + a.actBuffer + "\u258c")
		b.WriteString("\n")
	}
	if a.actResult != "" {
		b.WriteString("\n")
		b.WriteString(a.actResult)
		b.WriteString("\n")
	}
	if a.errMsg != "" {
		b.WriteString("\n")
		b.WriteString(styleErr.Render("  " + a.errMsg))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(hintBar("t/n/l", "actions", "esc", "back"))
	b.WriteString("\n")
	return b.String()
}

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
