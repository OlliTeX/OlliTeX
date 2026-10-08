package toolkit

// ui.go — the classic-console TUI core (the state model, jobs, shell,
// screen transitions).
//
// Layout contract (the mc / ncurses / freebsd-installer structure — kept
// identical to the bubbletea era):
//
//	row 0    menu strip       Dashboard Stack Shells Logs Hub Settings
//	                         Actions Doctor Backup About  [Menu ▾]
//	middle   LEFT pane        the master list (screens) — the mc left panel
//	           RIGHT pane     the detail of the selection (dashboard, stack,
//	                         keys, log tail, doctor rows, action output)
//	bottom   status line      docker/store/stack state · job + result
//
// Ported 2026-10-07 from bubbletea/lipgloss to rivo/tview (owner AI item):
// tview is retained-mode on tcell — only changed cells repaint, which is the
// fix for the "ultra slow over SSH" jank (the old full-frame redraw every
// keystroke + the 2 s log tick repainting ~400 lines).
//
// Dialogs (stop/restart/restore/quit confirmations + the two-step input
// prompts) are tview modals; the security contract is unchanged: [y]/[n]
// (y is confirm, n is the default) or [enter]/[esc] are the only exits.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ollitex/go/libraries/configschema"
)

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

	statusMsg string
	errMsg    string
	loading   bool
	jobName   string

	// logs
	logLines []string
	logName  string

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

	// shell (the classic attach)
	shell       *ShellSession
	shellLabel  string
	shellBuf    []byte
	shellExited bool

	// actions (admin: TLS import + n-gram models + first-admin)
	actResult string
	actCert   string
	actKey    string

	// the active prompt box (nil = none)
	dlg *dialog

	// the tview application (set by newTUI)
	tview *tviewApp
}

// boot — the initial auto-load for the screen the app booted on
// (the old tea Init command, now a direct job start).
func (a *app) boot() {
	if a.screen == "hub" && a.hub == nil {
		a.refreshHub()
	}
	a.redraw()
}

// newApp builds the state model for a Toolkit. startScreens: `ssh host
// <screen>` boot targets.
func newApp(t *Toolkit, startScreens ...string) *app {
	// the ssh session io (and a bare `ssh host tui` CLI invocation)
	// carries no TERM; tcell needs one that resolves in the image
	// terminfo DB (the alpine image ships ncurses-libs).
	if term := os.Getenv("TERM"); term == "" {
		os.Setenv("TERM", "xterm-256color")
	}

	a := &app{tk: t, width: 100, height: 40}
	if d, derr := NewDocker(t.DockerSocket); derr == nil {
		a.dock = d
	}
	if t.Store != nil {
		a.set = newSettings(t)
	}
	screen := "dashboard"
	if len(startScreens) > 0 {
		screen = startScreens[0]
	}
	if a.screenExists(screen) {
		a.screen = screen
	}
	if i, ok := a.findItem(a.screen); ok {
		a.dcur = i
	}
	if a.screen != "dashboard" {
		a.openItem(a.dcur)
	}
	a.boot()
	return a
}

// screenExists reports whether id is a master-list screen.
func (a *app) screenExists(id string) bool {
	_, ok := a.findItem(id)
	return ok
}

// validScreen reports whether id is a master-list screen (the boot targets
// for `ssh host <screen>`).
func validScreen(a *app, id string) bool {
	return a.screenExists(id)
}

// openItem sets the screen for the master row (the right pane re-renders).
func (a *app) openItem(i int) {
	items := a.masterList()
	if i < 0 || i >= len(items) {
		return
	}
	if os.Getenv("TK_TRACE") != "" {
		traceFile("NAV openItem->" + items[i].id + "\n")
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

// openScreen opens the named row and runs its initial job (logs refresh,
// doctor run, hub collect).
func (a *app) openScreen(id string) {
	if i, ok := a.findItem(id); ok {
		a.dcur = i
		a.openItem(i)
	}
	// FIRST publish the screen switch and the frame (synchronously, via the
	// app's update channel so it is processed before any later event — no
	// visible one-behind lag under key bursts); THEN start the screen's
	// async job. Starting the job first let its settle-redraw (a separate
	// queued draw) race the navigation redraw and render a stale a.screen —
	// the off-by-one "digit lands on the previous screen" the owner's e2e
	// caught (TK S14: each digit one behind).
	a.redraw()
	switch id {
	case "logs":
		a.refreshLogs(a.logName)
	case "doctor":
		a.runDoctor()
	case "settings":
		a.loadSettings()
	case "hub":
		if a.hubSample == 0 {
			a.hubSample = 10
		}
		if a.hub == nil || time.Since(a.hub.Now) > 15*time.Second {
			a.refreshHub()
		}
	}
}

// small helpers ---------------------------------------------------------------

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

func digit(i int) string { return string(rune('1' + i)) }

func cutPrefix(s, pre string) (string, bool) {
	if len(s) > len(pre) && s[:len(pre)] == pre {
		return s[len(pre):], true
	}
	return "", false
}

// ---- jobs (goroutines — the retained-mode replacement for tea.Cmd) ---------
//
// Every job: kick off a goroutine, mark loading, run the work, publish the
// result onto the app state, then schedule ONE tview redraw. tview
// coalesces redraws (QueueUpdateDraw), so the classic full-frame repaint
// storm over slow SSH links is gone: the frame is drawn only when state
// actually changed, and tcell repaints only the cells that differ.

// schedule redraws (safe from any goroutine — no-op when the app finished).
func (a *app) redraw() {
	if a.tview != nil {
		if a.tview.inLoop {
			// inside the event loop (key capture): render inline — tview
			// paints (a.draw()) right after the capture returns, so the new
			// state is on the screen BEFORE the next key reaches us (no
			// burst lag).
			// NOTE: state publishers (jobs settling, dialogs) call this
			// BEFORE their setRoot switch; a tview repaint in between is
			// harmless (old root, new content — replaced by the follow-up
			// paint) and the async settle-redraw still lands the final
			// frame.
			a.tview.render(a)
			a.tview.requestPaint()
			return
		}
		a.tview.schedule(func() { a.tview.render(a) })
	}
}

func (a *app) job(name string, timeout time.Duration, f func(ctx context.Context) (string, error), settle func(job, out string, err error)) {
	a.loading = true
	a.jobName = name
	a.redraw()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		out, err := f(ctx)
		a.loading = false
		if settle != nil {
			settle(name, out, err)
		} else {
			if err != nil {
				a.setErr("%s: %v", name, err)
			} else {
				a.statusMsg = name + " done" + tail(out)
			}
		}
		a.redraw()
	}()
}

func (a *app) runJob(job string, f func(ctx context.Context) (string, error)) {
	a.job(job, 180*time.Second, f, nil)
}

// runJobLong is for store-heavy jobs (the data-backup round tars the full
// SeaweedFS volume — minutes, not seconds).
func (a *app) runJobLong(job string, f func(ctx context.Context) (string, error)) {
	a.job(job, 25*time.Minute, f, nil)
}

func (a *app) silentJob(job string, f func(ctx context.Context) (string, error)) {
	a.job(job, 60*time.Second, f, func(name, out string, err error) {
		if err != nil {
			a.setErr("%s: %v", name, err)
			return
		}
		// silent jobs set their own state (logLines, doctorRows, hub…);
		// keep the status line calm.
		if a.statusMsg == "" || strings.HasPrefix(a.statusMsg, name+" ") {
			a.statusMsg = name + " ok" + tail(out)
		}
	})
}

// refreshHub — one read pass over the content DB (the exact /hub view
// predicates; see hubdata.go). The sample width is what "t" toggles.
func (a *app) refreshHub() {
	a.silentJob("hub-refresh", func(ctx context.Context) (string, error) {
		s, err := a.tk.HubCollect(ctx, a.hubSample)
		if err != nil {
			return "", err
		}
		a.hub = s
		return "hub refreshed", nil
	})
}

func runBackupScript(dir, script string, ctx context.Context) (string, error) {
	p := filepath.Join(dir, script)
	if fi, err := os.Stat(p); err != nil || fi.IsDir() {
		return "", fmt.Errorf("backup script missing: %s", p)
	}
	cmd := exec.CommandContext(ctx, p)
	cmd.Dir = dir
	var b strings.Builder
	cmd.Stdout = &b
	cmd.Stderr = &b
	err := cmd.Run()
	out := b.String()
	// tail-truncation (the TUI renders a bounded tail, not a 1000-line
	// dump) + the exit note INLINED (the TUI renders job results, never
	// Go errors — a failing script is a normal observation)
	const maxLines = 40
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > maxLines {
		out = "  (earlier lines…)\n" + strings.Join(lines[len(lines)-maxLines:], "\n")
	}
	if err != nil {
		return out + "\n  [exit: " + err.Error() + "]", nil
	}
	return out, nil
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

func (a *app) refreshStatus() {
	if a.dock == nil {
		return
	}
	a.silentJob("ping", func(ctx context.Context) (string, error) {
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

func (a *app) refreshLogs(name string) {
	if name == "" {
		name = a.firstLoggable()
	}
	a.logName = name
	if a.dock == nil || name == "" {
		return
	}
	a.silentJob("logs", func(ctx context.Context) (string, error) {
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

// logCycle steps to the next/prev container (h/l = left/right, classic).
func (a *app) logCycle(dir int) {
	lst := a.containerList()
	if len(lst) == 0 {
		return
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
	}
	a.refreshLogs(lst[next].Name)
}

// ---- doctor ----------------------------------------------------------------

func (a *app) runDoctor() {
	a.silentJob("doctor", func(ctx context.Context) (string, error) {
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
	if a.tk == nil || a.tk.Store == nil {
		a.setGroups = nil
		return
	}
	if a.set == nil {
		a.set = newSettings(a.tk)
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

// settingsNav handles the two-pane settings navigation + edit mode for the
// key `k` (port of the old settingsKey state machine — tea types removed).
func (a *app) settingsNav(k string, runes []rune) (handled bool) {
	// EDIT MODE (unchanged behavior from the flat era): the box consumes typing
	if a.editKey != "" {
		switch k {
		case "backspace", "delete":
			if len(a.editVal) > 0 {
				a.editVal = a.editVal[:len(a.editVal)-1]
			}
			return true
		case "esc", "q":
			a.editKey, a.editVal, a.editMask = "", "", false
			return true
		case "enter", "x", "ctrl+s":
			key := a.editKey
			val := strings.TrimSpace(a.editVal)
			a.editKey, a.editVal, a.editMask = "", "", false
			if val == "" {
				a.runJob("unset", func(ctx context.Context) (string, error) {
					if err := a.set.Delete(key, "toolkit-tui"); err != nil {
						return "", err
					}
					return key, nil
				})
			} else {
				a.runJob("set", func(ctx context.Context) (string, error) {
					if err := a.set.Set(key, val, "toolkit-tui"); err != nil {
						return "", err
					}
					return key, nil
				})
			}
			return true
		}
		if len(runes) > 0 {
			a.editVal += string(runes)
		}
		return true
	}
	// LEVEL 0 — navigate the groups (owner: the settings tree)
	if a.setLevel == 0 || len(a.setGroups) == 0 {
		n := max2(1, len(a.setGroups))
		switch k {
		case "down", "j":
			if len(a.setGroups) > 0 {
				a.setCurGroup = (a.setCurGroup + 1) % n
			}
			return true
		case "up", "k":
			if len(a.setGroups) > 0 {
				a.setCurGroup = (a.setCurGroup - 1 + n) % n
			}
			return true
		case "enter", "l":
			if len(a.setGroups) == 0 {
				return true
			}
			a.setLevel = 1
			a.setCurSel = 0
			return true
		}
		return false // level 0 only eats j/k/enter/l; the rest fall through
		// (was `return !handled` = always true — swallowed digit keys from
		// the settings screen, breaking 1-9/0 screen jumps after `4`.)
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
		return true
	case "up", "k":
		if len(gkeys) > 0 {
			a.setCurSel = (a.setCurSel - 1 + n) % n
		}
		return true
	case "b":
		// back a level (to the groups); tab/arrow-left hops panes instead
		a.setLevel = 0
		return true
	case "enter", "e":
		if len(gkeys) == 0 {
			return true
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
		return true
	}
	return false
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

// actionsKey drives the Actions prompts through the prompt boxes (dlg):
// the classic two-step flows (TLS cert→key, bootstrap email→password) are
// each a dialog; the jobs run unchanged.
func (a *app) actionsKey(k string) {
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
	a.redraw()
}

func (a *app) runNgram(langs string) {
	t := a.tk
	a.runJob("ngram", func(ctx context.Context) (string, error) {
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

func (a *app) runBootstrap(email, password string) {
	t := a.tk
	var b strings.Builder
	if err := t.BootstrapFreshInstance(email, password, &b); err != nil {
		a.actResult = "bootstrap FAILED: " + err.Error()
		a.redraw()
		return
	}
	a.actResult = b.String() + "\n  first admin created: " + email + " — sign in with email + password; the instance is bootable."
	a.redraw()
}

func (a *app) runTLS(certSrc, keySrc string) {
	t, dk := a.tk, a.dock
	a.runJob("tls", func(ctx context.Context) (string, error) {
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

// ---- shell (the classic attach) ---------------------------------------------

func (a *app) startShell(label string) {
	dk, tk := a.dock, a.tk
	a.loading = true
	a.jobName = "shell " + label
	a.redraw()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		sess, err := NewShell(ctx, dk, tk.Project, label)
		a.loading = false
		if err != nil {
			a.setErr("shell %s: %v", label, err)
			a.redraw()
			return
		}
		a.shell = sess
		a.shellLabel = label
		a.shellBuf = nil
		a.shellExited = false
		a.screen = "shell"
		a.redraw()
		go a.pumpShellLoop()
	}()
}

// pumpShellLoop reads the exec stream until EOF, appending decoded chunks
// (tview redraws are coalesced — no full-frame flood over SSH).
func (a *app) pumpShellLoop() {
	sess := a.shell
	if sess == nil {
		return
	}
	buf := make([]byte, 256*1024)
	for {
		n, err := sess.ReadOne(buf)
		if n > 0 {
			text := DecodeShellChunk(buf[:n])
			a.shellBuf = append(a.shellBuf, []byte(text)...)
			if len(a.shellBuf) > 1<<20 { // 1 MiB cap: keep the tail
				a.shellBuf = a.shellBuf[len(a.shellBuf)-(1<<20):]
			}
			a.redraw()
		}
		if err != nil {
			a.shellExited = true
			a.endShell()
			a.screen = "dashboard"
			a.refreshStatus()
			a.redraw()
			return
		}
	}
}

func (a *app) endShell() {
	if a.shell != nil {
		a.shell.Close()
		a.shell = nil
	}
}

// shellForward sends operator input to the exec stream (the shellKey port:
// ctrl-z → detach & close · ctrl+c → to the shell · everything else verbatim).
func (a *app) shellForward(b string) {
	sess := a.shell
	if sess == nil {
		return
	}
	if _, err := sess.Write([]byte(b)); err != nil {
		a.setErr("shell write: %v", err)
		a.shellExited = true
		a.endShell()
		a.screen = "dashboard"
		a.redraw()
	}
}

var _ = strconv.Itoa // (kept for the settings parsers that live in config.go)
