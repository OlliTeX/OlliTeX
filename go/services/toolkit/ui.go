package toolkit

// ui.go — the classic-console TUI core (the model, jobs, shell, key routing).
//
// Layout (the mc / ncurses / freebsd-installer structure):
//
//	line 1    title bar        "OlliTeX Toolkit" · project · rev
//	line 2    menu bar         Stack · Settings · Shells · Logs · Doctor ·
//	                           Backup · Actions · Help   ← jejacks0n/bubbletea-menubar
//	middle    LEFT pane        the master list (screens) — the mc left panel
//	            RIGHT pane     the detail of the selection (table, keys, log,
//	                           doctor rows, action output) — bordered panes
//	last 2    status line      docker/store/stack state · job + spinner
//	            keystorep      [j/k] move [enter] open [u] start [d] stop [F10] menu
//	                           ← bracketed-key row (mc keystrip), clickable
//
// Dialogs (stop/restart/restore/quit confirmations + the two-step input
// prompts) are the mc prompt boxes: a bordered box centered over the panes
// (rmhubbert/bubbletea-overlay compositor), with click-targeted [y]/[n] and
// [enter]/[esc] buttons (lrstanley/bubblezone hit-testing).
//
// All three libraries are MIT, credited in CREDITS.md.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ollitex/go/libraries/configschema"

	tea "github.com/charmbracelet/bubbletea"
	menubar "github.com/jejacks0n/bubbletea-menubar"
	zone "github.com/lrstanley/bubblezone"
)

// ---- messages --------------------------------------------------------------

type jobMsg struct {
	job  string
	out  string
	err  error
	done bool
}

// menuActionMsg is delivered by the menubar when a menu item is chosen.
type menuActionMsg struct{ action string }

// ---- app -------------------------------------------------------------------

type doctorRow struct {
	label  string
	detail string
	ok     bool
}

const maxLines = 400 // the log tail cap (the classic: the last 400 lines)

type app struct {
	tk   *Toolkit
	dock *Docker
	set  *Settings

	screen string // dashboard | stack | shells | logs | settings | actions | doctor | backup | about | shell
	dcur   int    // the master-list cursor (the left pane)
	width  int
	height int

	menu     menubar.Model
	menuOpen bool
	zoneID   string // this session's zone namespace (bubblezone prefix)

	statusMsg string
	errMsg    string
	loading   bool
	jobName   string

	// logs
	logLines []string
	logName  string

	// esc fallback state (the terminal-friendly alt-0 / esc+0 menu opener:
	// a lone ESC still gets its semantics after the 300ms window)
	escPending bool

	// hub views (the site.general.projects.* + users.* inside the TUI)
	hub       *HubStats
	hubSample int // 10 or 20 ("t" toggles)

	// settings
	setGroups []Group
	setCurSel int
	editKey   string
	editVal   string
	editMask  bool
	// two-level settings navigation: level 0 = the groups, level 1 = the
	// selected group's keys (owner: "organize the settings into sub-groups")
	setLevel    int
	setCurGroup int
	// two-pane focus: 0 = the left master list, 1 = the settings tree
	// (the right pane); starts on the settings side when settings opens
	focus int

	// doctor
	doctorRows []doctorRow

	// shell (full-screen mode — the classic attach)
	shell       *ShellSession
	shellLabel  string
	shellBuf    []byte
	shellExited bool

	// actions (admin: TLS import + n-gram models + first-admin)
	actResult string
	actCert   string
	actKey    string

	// the active mc prompt box (nil = none)
	dlg *dialog
}

// validScreen reports whether id is a master-list screen (the boot targets
// for `ssh host <screen>`).
func validScreen(a *app, id string) bool {
	for _, it := range a.masterList() {
		if it.id == id {
			return true
		}
	}
	return false
}

func newApp(t *Toolkit, startScreens ...string) *app {
	start := ""
	if len(startScreens) > 0 {
		start = startScreens[0]
	}
	d, derr := NewDocker(t.DockerSocket)
	screen := "dashboard"
	a0 := &app{
		tk:     t,
		set:    newSettings(t),
		width:  80,
		height: 24,
	}
	if start != "" && validScreen(a0, start) {
		screen = start
	}
	a := &app{
		tk:     t,
		set:    newSettings(t),
		screen: screen,
		width:  80,
		height: 24,
	}
	if derr != nil {
		a.dock = nil
		a.errMsg = "docker: " + derr.Error()
	} else {
		a.dock = d
	}
	zone.NewGlobal() // idempotent: the one-per-process zone manager
	a.zoneID = zone.NewPrefix()
	a.menu = newMenus(a)
	zone.SetEnabled(true)
	if a.tk.Store != nil {
		a.loadSettings()
	}
	return a
}

// Init (tea.Model)
func (a *app) Init() tea.Cmd {
	if a.screen == "hub" {
		if a.hubSample == 0 {
			a.hubSample = 10
		}
		return tea.Sequence(a.refreshStatus(), a.refreshHub())
	}
	return a.refreshStatus()
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

// runJobLong is for store-heavy jobs (the data-backup round tars the full
// SeaweedFS volume — minutes, not seconds). Same message contract.
func (a *app) runJobLong(job string, f func(ctx context.Context) (string, error)) tea.Cmd {
	a.loading = true
	a.jobName = job
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
		defer cancel()
		out, err := f(ctx)
		return jobMsg{job: job, out: out, err: err, done: true}
	}
}

// refreshHub — one read pass over the content DB (the exact /hub view
// predicates; see hubdata.go). The sample width is what "t" toggles.
func (a *app) refreshHub() tea.Cmd {
	return a.runJob("hub-refresh", func(ctx context.Context) (string, error) {
		s, err := a.tk.HubCollect(ctx, a.hubSample)
		if err != nil {
			return "", err
		}
		a.hub = s
		return fmt.Sprintf("%s: %d projects (all) · %d users (all)", s.DB,
			s.Projects.All, s.Users.All), nil
	})
}

// runBackupScript runs one of the toolkit/backup/*.sh suites (baked into the
// image at /opt/ollitex/backup; OLLITEX_TOOLKIT_BACKUP_DIR points at the repo
// copy during host-side dev) and returns its tail — the [backup]/[drill]
// status lines the TUI renders in the result pane.
func runBackupScript(dir, script string, ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", filepath.Join(dir, script))
	if v := os.Getenv("OLLITEX_TOOLKIT_DATA_DIR"); v != "" {
		cmd.Env = append(cmd.Env, "BACKUP_ROOT="+filepath.Join(v, "backups"))
	}
	var b bytes.Buffer
	cmd.Stdout = &b
	cmd.Stderr = &b
	err := cmd.Run()
	out := b.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > 40 {
		out = "… " + strconv.Itoa(len(lines)-40) + " earlier lines…\n" + strings.Join(lines[len(lines)-40:], "\n")
	}
	if err != nil {
		return out + "\n[exit " + err.Error() + "]", nil
	}
	return out, nil
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

// ---- shell (the classic full-screen exec attach — unchanged contract) ------

type shellChunkMsg struct {
	text string
	eof  bool
}

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
		// some front-end paths draw frame 0 before the size arrives — a zero
		// size must never reach the pane math (the old [:-6] slice panic)
		if a.width < 40 {
			a.width = 80
		}
		if a.height < 12 {
			a.height = 24
		}
		return a, nil

	case shellStartMsg:
		a.loading = false
		if m.err != nil {
			a.setErr("shell %s: %v", m.label, m.err)
			return a, nil
		}
		a.screen = "shell"
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

	case menuActionMsg:
		return a, a.dispatchMenu(m.action)

	case escTimeoutMsg:
		// the esc+0 window closed without a 0 → the lone ESC gets its
		// semantics now (no delayed back/close/quit surprise).
		a.escPending = false
		next, cmd := a.escSemantics()
		return next, cmd

	case logTickMsg:
		// auto-refresh chain: while the logs screen is focused and idle, keep
		// the tail live (the classic: the log window refreshes itself — no
		// manual [f] needed to see the box fill).
		if a.screen == "logs" && !a.loading && a.logName != "" && a.dock != nil {
			return a, a.refreshLogs(a.logName)
		}
		return a, nil

	case jobMsg:
		a.loading = false
		if m.err != nil {
			a.setErr("%s: %v%s", m.job, m.err, tail(m.out))
			return a, nil
		}
		a.errMsg = ""
		switch m.job {
		case "ping":
			a.statusMsg = ""
			return a, nil
		case "logs":
			a.statusMsg = ""
			// re-arm the 2s tick (the chain stops of its own accord once the
			// user leaves the logs screen or the dock goes away).
			return a, logTickCmd()
		case "start", "stop", "restart":
			a.statusMsg = m.job + " done" + tail(m.out)
			return a, a.refreshStatus()
		case "pull":
			a.statusMsg = "images pulled" + tail(m.out)
			return a, a.refreshStatus()
		case "hub-refresh":
			a.statusMsg = "hub: " + m.out
			return a, nil
		case "backup":
			a.statusMsg = "backup saved → " + a.tk.BackupPath() + tail(m.out)
			return a, nil
		case "restore":
			a.statusMsg = "restore done" + tail(m.out)
			a.loadSettings()
			return a, nil
		case "set", "unset":
			a.statusMsg = m.job + " " + m.out
			a.loadSettings()
			return a, nil
		case "tls", "ngram", "bootstrap":
			a.statusMsg = m.job + " done" + tail(m.out)
			a.actResult = m.out
			return a, nil
		default:
			a.statusMsg = m.job + " done" + tail(m.out)
			return a, nil
		}

	case tea.MouseMsg:
		return a.handleMouse(m)

	case tea.KeyMsg:
		if a.screen == "shell" && a.shell != nil {
			return a.shellKey(m)
		}
		if a.dlg != nil {
			return a, a.dialogKey(m)
		}
		// MENU-OPEN DELEGATION (owner: "It can be open but nothing can be
		// selected") — MUST run before the global/nav/screen switches: the
		// menubar library owns up/down/left/right/enter/esc while active, and
		// those are exactly the keys our navigation + screen handlers used to
		// eat first (k/j = down/up, enter = openCmd, l/h = cycle), so no menu
		// item could ever be reached. Items fire as their Action cmd
		// (menuActionMsg → dispatchMenu, which closes the bar itself).
		// Explicit toggles (F10/F9) close it; anything else stays inert.
		if a.menuOpen {
			switch k2 := m.String(); k2 {
			case "up", "down", "left", "right", "enter", "esc", "tab", "shift+tab":
				next, cmd := a.menu.Update(m)
				a.menu = next
				a.syncMenu()
				return a, cmd
			case "f10", "f9", "ctrl+m":
				a.menuActive(false)
				return a, nil
			}
			return a, nil // other keys: no hidden navigation while the menu is open
		}
		return a.handleKey(m)
	}
	return a, nil
}

// syncMenu mirrors the menubar's active state onto our routing flag.
func (a *app) syncMenu() { a.menuOpen = a.menu.Active }

// shellKey forwards operator input to the exec stream.
//
//	ctrl-c → to the shell (SIGINT) · ctrl-z → detach & close (back to the panes)
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
		a.screen = "dashboard"
		return a, a.refreshStatus()
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

// ---- master-list keys (the left pane) ---------------------------------------
//
// mc muscle memory: j/k roam the panel, enter opens, digits jump, F10 the
// menu bar, and the screen keys (u/d/e/h/l/f/t/n/b/r) act on the selection.

func (a *app) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	// pending-ESC resolution runs BEFORE the global switch (a second ESC
	// must be consumed, not re-armed): ESC+0 → the menu; anything else →
	// the ESC semantics run first, then this key is processed normally.
	if a.escPending {
		a.escPending = false
		if k == "0" {
			a.menuActive(true)
			return a, nil
		}
		if k == "esc" {
			next, cmd := a.escSemantics() // the double ESC is the first ESC
			return next, cmd
		}
		next, cmd := a.escSemantics()
		ap, ok := next.(*app)
		if !ok {
			return next, cmd // the ESC quit: nothing left to do
		}
		a = ap
		// fall through: process this key normally below
	}

	// global
	switch k {
	case "f10", "ctrl+m", "f9":
		// f10 is primary (the mc convention); ctrl+m is the vt100-era F10
		// byte; f9 is a safe alias for terminals whose F10 never arrives.
		a.menuActive(true)
		return a, nil
	case "f1", "?":
		a.screen = "about"
		return a, nil
	case "ctrl+c":
		if a.screen != "dashboard" {
			a.screen = "dashboard"
			a.dcur = 0
			a.loading = false
			return a, nil
		}
		if a.stackUpNow() {
			a.askQuit()
			return a, nil
		}
		return a, tea.Quit
	case "q":
		// q is the unambiguous exit — no fallback-window ambiguity.
		next, cmd := a.escSemantics()
		return next, cmd
	case "esc":
		// ESC is special: it is also the prefix of the terminal-friendly
		// menu fallback (ESC then 0 — the mc alt-F habit, for terminals
		// where the F-keys do not arrive). A 300ms window (standard
		// ESCDELAY shape) decides which meaning it was; ESC while the menu
		// is open stays immediate (close the menu — mc behavior).
		if a.menuOpen {
			next, cmd := a.escSemantics()
			return next, cmd
		}
		a.escPending = true
		return a, tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return escTimeoutMsg{} })
	}

	items := a.masterList()

	// TWO-PANE FOCUS (owner's two-pane contract): when the settings screen
	// is open, focus starts on the RIGHT pane (the settings tree); tab or
	// left/right hops between panes; j/k/enter act on the pane in focus.
	rightActive := a.screen == "settings"
	if rightActive {
		// tab or an arrow hops between the two panes
		if k == "tab" || k == "left" || k == "right" {
			if k == "tab" {
				a.focus = 1 - a.focus
			}
			if k == "left" {
				a.focus = 0
			}
			if k == "right" {
				a.focus = 1
			}
			return a, nil
		}
		if a.focus == 1 {
			return a.settingsKey(msg)
		}
	}

	// master-list navigation — the LEFT pane (live on every screen)
	switch k {
	case "j", "down":
		a.dcur = (a.dcur + 1) % len(items)
		a.openItem(a.dcur)
		return a, nil
	case "k", "up":
		a.dcur = (a.dcur - 1 + len(items)) % len(items)
		a.openItem(a.dcur)
		return a, nil
	case "1":
		a.dcur, _ = 0, items[0]
		return a, a.openCmd(0)
	case "2":
		a.dcur = 1
		return a, a.openCmd(1)
	case "3":
		a.dcur = 2
		return a, a.openCmd(2)
	case "4":
		a.dcur = 3
		return a, a.openCmd(3)
	case "5":
		a.dcur = 4
		return a, a.openCmd(4)
	case "6":
		a.dcur = 5
		return a, a.openCmd(5)
	case "7":
		a.dcur = 6
		return a, a.openCmd(6)
	case "8":
		a.dcur = 7
		return a, a.openCmd(7)
	case "enter", "space":
		return a, a.openCmd(a.dcur)
	}

	// screen keys (mc: the keys act on the pane in focus)
	switch a.screen {
	case "dashboard":
		switch k {
		case "u", "s":
			return a, a.runJob("start", a.tk.StackUp)
		case "d", "t":
			a.askStop()
			return a, nil
		}
	case "stack":
		switch k {
		case "u", "s":
			return a, a.runJob("start", a.tk.StackUp)
		case "d", "t":
			a.askStop()
			return a, nil
		case "r":
			a.askRestart()
			return a, nil
		case "p":
			return a, a.runJob("pull", a.tk.PullImages)
		case "m":
			return a, a.startShell(Shells[0].Label)
		case "g":
			return a, a.startShell(Shells[1].Label)
		case "e":
			return a, a.startShell(Shells[2].Label)
		}
	case "shells":
		for i, s := range Shells {
			if k == "enter" || k == "x"+digit(i) {
				return a, a.startShell(s.Label)
			}
		}
	case "logs":
		return a.logsKey(msg)
	case "settings":
		return a.settingsKey(msg)
	case "actions":
		return a, a.actionsKey(k)
	case "doctor":
		if k == "r" {
			return a, a.runDoctor()
		}
	case "hub":
		switch k {
		case "r", "enter":
			return a, a.refreshHub()
		case "t":
			if a.hubSample == 10 {
				a.hubSample = 20
			} else {
				a.hubSample = 10
			}
			return a, a.refreshHub()
		}
	case "backup":
		switch k {
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
			a.askRestore()
			return a, nil
		case "d":
			return a, a.runJobLong("data-backup", func(ctx context.Context) (string, error) {
				return runBackupScript(a.tk.BackupDir(), "backup-data.sh", ctx)
			})
		case "v":
			return a, a.runJobLong("data-drill", func(ctx context.Context) (string, error) {
				return runBackupScript(a.tk.BackupDir(), "restore-drill.sh", ctx)
			})
		}
	}
	return a, nil
}

func digit(i int) string { return string(rune('1' + i)) }

// menuActive opens/closes the menu bar (the menubar library drives its own
// keys + mouse once active).
func (a *app) menuActive(on bool) {
	a.menu.Active = on
	if !on {
		a.menu.OpenSubMenu = -1
		a.menu.SubMenuState = nil
	}
	a.menuOpen = on
}

// escSemantics — what a lone ESC does (mc order: close dialog → close menu →
// back to the dashboard → quit confirm — quit only when the stack is up).
func (a *app) escSemantics() (tea.Model, tea.Cmd) {
	a.dlg = nil
	a.editKey, a.editVal, a.editMask = "", "", false
	if a.menuOpen {
		a.menuActive(false)
		return a, nil
	}
	if a.screen != "dashboard" {
		a.screen = "dashboard"
		a.dcur = 0
		return a, nil
	}
	if a.stackUpNow() {
		a.askQuit()
		return a, nil
	}
	return a, tea.Quit
}

// escTimeoutMsg — fired when the ESC fallback window expires without a 0.
type escTimeoutMsg struct{}

// logTickMsg — the logs auto-refresh heartbeat (2s chain while focused).
type logTickMsg struct{}

func logTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return logTickMsg{} })
}

// openItem sets the screen for the master row (the right pane re-renders).
func (a *app) openItem(i int) {
	items := a.masterList()
	if i < 0 || i >= len(items) {
		return
	}
	a.screen = items[i].id
	a.editKey, a.editVal, a.editMask = "", "", false
	if items[i].id == "settings" {
		a.setLevel, a.setCurGroup, a.setCurSel = 0, 0, 0 // fresh navigation
		a.focus = 1                                      // the settings tree owns j/k
	} else {
		a.focus = 0 // every other screen: the left list owns j/k
	}
	a.actResult = ""
}

// openCmd = openItem + the row's initial job (logs refresh, doctor run…).
func (a *app) openCmd(i int) tea.Cmd {
	a.openItem(i)
	switch a.masterList()[i].id {
	case "logs":
		return a.refreshLogs(a.logName)
	case "doctor":
		return a.runDoctor()
	case "settings":
		a.loadSettings()
	case "hub":
		if a.hubSample == 0 {
			a.hubSample = 10
		}
		if a.hub == nil || time.Since(a.hub.Now) > 15*time.Second {
			return a.refreshHub()
		}
	}
	return nil
}

// ---- mouse (bubblezone hit-testing + the menubar's own) --------------------

func (a *app) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// the dialog owns the screen when open (its [y]/[n]/[enter]/[esc] zones)
	if a.dlg != nil {
		for _, zid := range []string{a.dlg.id + ".y", a.dlg.id + ".ok"} {
			if z := zone.Get(zid); z != nil && z.InBounds(msg) {
				return a, a.dialogConfirm()
			}
		}
		for _, zid := range []string{a.dlg.id + ".n"} {
			if z := zone.Get(zid); z != nil && z.InBounds(msg) {
				a.dlg = nil
				return a, nil
			}
		}
		return a, nil
	}
	if a.screen == "shell" {
		return a, nil
	}
	// the menu bar: its own bounds (top rows) or when open
	if a.menuOpen || msg.Y <= 2 {
		next, cmd := a.menu.Update(msg)
		a.menu = next
		a.syncMenu()
		return a, cmd
	}
	// the left-pane rows
	for i := range a.masterList() {
		if z := zone.Get(a.zoneID + ".row." + fmt.Sprint(i)); z != nil && z.InBounds(msg) {
			a.dcur = i
			return a, a.openCmd(i)
		}
	}
	// the action chips (right pane)
	for _, c := range []chip{
		{"start", func() tea.Cmd { return a.runJob("start", a.tk.StackUp) }},
		{"stop", func() tea.Cmd { a.askStop(); return nil }},
		{"restart", func() tea.Cmd { a.askRestart(); return nil }},
		{"pull", func() tea.Cmd { return a.runJob("pull", a.tk.PullImages) }},
		{"backup", func() tea.Cmd {
			return a.runJob("backup", func(ctx context.Context) (string, error) {
				m, err := a.tk.Store.Dump(a.tk.BackupPath())
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%d keys", len(m)), nil
			})
		}},
		{"data-backup", func() tea.Cmd {
			return a.runJobLong("data-backup", func(ctx context.Context) (string, error) {
				return runBackupScript(a.tk.BackupDir(), "backup-data.sh", ctx)
			})
		}},
		{"data-drill", func() tea.Cmd {
			return a.runJobLong("data-drill", func(ctx context.Context) (string, error) {
				return runBackupScript(a.tk.BackupDir(), "restore-drill.sh", ctx)
			})
		}},
		{"restore", func() tea.Cmd { a.askRestore(); return nil }},
	} {
		if z := zone.Get(a.zoneID + ".chip." + c.id); z != nil && z.InBounds(msg) {
			return a, c.fn()
		}
	}
	return a, nil
}

type chip struct {
	id string
	fn func() tea.Cmd
}

// ---- status / stack ---------------------------------------------------------

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

func (a *app) stackUpNow() bool {
	up, _ := a.stackCount()
	return up > 0
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
				next = idx
			}
		}
		return a, a.refreshLogs(lst[next].Name)
	}
	return a, nil
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

// ---- settings ----------------------------------------------------------------

func (a *app) loadSettings() {
	if a.tk.Store == nil {
		return
	}
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
	k := msg.String()
	// EDIT MODE (unchanged behavior from the flat era): the box consumes typing
	if a.editKey != "" {
		switch k {
		case "backspace", "delete":
			if len(a.editVal) > 0 {
				a.editVal = a.editVal[:len(a.editVal)-1]
			}
			return a, nil
		case "esc", "q":
			a.editKey, a.editVal, a.editMask = "", "", false
			return a, nil
		case "enter", "x", "ctrl+s":
			key := a.editKey
			val := strings.TrimSpace(a.editVal)
			a.editKey, a.editVal, a.editMask = "", "", false
			if val == "" {
				return a, a.runJob("unset", func(ctx context.Context) (string, error) {
					if err := a.set.Delete(key, "toolkit-tui"); err != nil {
						return "", err
					}
					return key, nil
				})
			}
			return a, a.runJob("set", func(ctx context.Context) (string, error) {
				if err := a.set.Set(key, val, "toolkit-tui"); err != nil {
					return "", err
				}
				return key, nil
			})
		}
		if len(msg.Runes) > 0 {
			a.editVal += string(msg.Runes)
		}
		return a, nil
	}
	// LEVEL 0 — navigate the groups (owner: the settings tree)
	if a.setLevel == 0 || len(a.setGroups) == 0 {
		n := max2(1, len(a.setGroups))
		switch k {
		case "down", "j":
			if len(a.setGroups) > 0 {
				a.setCurGroup = (a.setCurGroup + 1) % n
			}
			return a, nil
		case "up", "k":
			if len(a.setGroups) > 0 {
				a.setCurGroup = (a.setCurGroup - 1 + n) % n
			}
			return a, nil
		case "enter", "l":
			if len(a.setGroups) == 0 {
				return a, nil
			}
			a.setLevel = 1
			a.setCurSel = 0
			return a, nil
		}
		return a, nil
	}
	// LEVEL 1 — the keys of the selected group
	if a.setCurGroup >= len(a.setGroups) {
		a.setCurGroup = 0
	}
	gkeys := a.setGroups[a.setCurGroup].Keys
	n := max2(1, len(gkeys))
	switch k {
	case "down", "j":
		if len(gkeys) > 0 {
			a.setCurSel = (a.setCurSel + 1) % n
		}
		return a, nil
	case "up", "k":
		if len(gkeys) > 0 {
			a.setCurSel = (a.setCurSel - 1 + n) % n
		}
		return a, nil
	case "b":
		// back a level (to the groups); tab/arrow-left hops panes instead
		a.setLevel = 0
		return a, nil
	case "enter", "e":
		if len(gkeys) == 0 {
			return a, nil
		}
		e := gkeys[a.setCurSel%len(gkeys)]
		v := e.Value
		if e.Secret && e.Present {
			if real, present, _ := a.set.Reveal(e.Key); present {
				v = real
			}
		}
		a.editKey = e.Key
		a.editVal = v
		a.editMask = e.Secret
		return a, nil
	}
	return a, nil
}

// ---- actions (admin): TLS import + n-gram models + first-admin -------------

func (a *app) ngramStatusText() string {
	d := a.tk.DataDir
	var b strings.Builder
	b.WriteString("  lang   archive                state          size")
	for _, lang := range NgramCodes() {
		arch := NgramArchives[lang]
		z := filepath.Join(d, "ngrams", fmt.Sprintf("ngrams-%s.zip", lang))
		dir := filepath.Join(d, "ngrams", lang)
		_, zerr := os.Stat(z)
		_, derr := os.Stat(dir)
		state := "missing"
		var size int64
		if zerr == nil && derr == nil {
			state, size = "present", fileSize(z)
		} else if zerr == nil {
			state, size = "archive only", fileSize(z)
		} else if derr == nil {
			state = "extracted only"
		}
		b.WriteString("\n  " + fmt.Sprintf("%-4s   %-20s   %-12s   %s", lang, arch, state, HumanBytes(size)))
	}
	b.WriteString(fmt.Sprintf("\n\n  dir: %s/ngrams (mounted read-only into the languagetool service)", d))
	return b.String()
}

// actionsKey drives the Actions prompts through the mc prompt boxes (dlg):
// the classic two-step flows (TLS cert→key, bootstrap email→password) are
// each an overlay dialog; the jobs run unchanged.
func (a *app) actionsKey(k string) tea.Cmd {
	switch k {
	case "t":
		a.actCert, a.actKey = "", ""
		a.dialogPrompt("tls-cert", "TLS import — certificate", "host path of the TLS certificate (a file):")
	case "n":
		a.dialogPrompt("ngram", "n-gram models", "languages (en,de — empty = all five):")
	case "l":
		a.actResult = a.ngramStatusText()
	case "b":
		a.actCert, a.actKey = "", ""
		a.dialogPrompt("boot-email", "first-admin bootstrap", "admin email:")
	}
	return nil
}

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
		b.WriteString(fmt.Sprintf("\n  (dir: %s/ngrams — restart the languagetool service to pick up new models)", t.DataDir))
		return b.String(), nil
	})
}

func (a *app) runBootstrap(email, password string) tea.Cmd {
	t := a.tk
	var b strings.Builder
	if err := t.BootstrapFreshInstance(email, password, &b); err != nil {
		a.actResult = "bootstrap FAILED: " + err.Error()
		return nil
	}
	a.actResult = b.String() + "\n  first admin created: " + email + " — sign in with email + password; the instance is bootable."
	return nil
}

func (a *app) runTLS(certSrc, keySrc string) tea.Cmd {
	t, dk := a.tk, a.dock
	return a.runJob("tls", func(ctx context.Context) (string, error) {
		info, err := ImportCert(certSrc, keySrc, certSrc, keySrc)
		_ = info
		if err != nil {
			return "", err
		}
		certDst := filepath.Join(t.DataDir, "nginx", "tls", "nginx_certificate.pem")
		keyDst := filepath.Join(t.DataDir, "nginx", "tls", "nginx_key.pem")
		if serr := t.Store.Set("TLS_CERTIFICATE_PATH", certDst, "toolkit:tls-import"); serr != nil {
			return "", serr
		}
		if serr := t.Store.Set("TLS_PRIVATE_KEY_PATH", keyDst, "toolkit:tls-import"); serr != nil {
			return "", serr
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("  cert  → %s\n  key   → %s\n  store: TLS_CERTIFICATE_PATH / TLS_PRIVATE_KEY_PATH updated\n", certDst, keyDst))
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

// (the View + menu + dialog surfaces live in ui_view.go / ui_menu.go /
//  ui_dialog.go — this file is the model + jobs + keys + mouse.)
