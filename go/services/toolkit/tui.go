package toolkit

// tui.go — the rivo/tview retained-mode UI for the OlliTeX Toolkit
// (AI item, 2026-10-07: the bubbletea/lipgloss full-frame redraw was the
// "ultra slow over SSH" wall — tview/tcell is a retained-mode screen that
// repaints only changed cells, and QueueUpdateDraw coalesces programmatic
// updates).
//
// Built against this environment's tview build (github.com/rivo/tview
// v0.42.0 + github.com/gdamore/tcell/v2 v2.8.1 — the exact pair the Go
// proxy serves here) — classic widget API: List.AddItem(text, secondary,
// shortcut, cb), Modal AddButtons + SetDoneFunc(index,label), Grid with
// Flexibility rows, Application.SetInputCapture(func(*EventKey) *EventKey).
//
// Layout contract (the classic mc console, kept identical):
//
//	row 0   menu strip — one button per screen + [MENU] (the leaf list)
//	middle  LEFT: the master list   RIGHT: the screen's detail view
//	bottom  the status line (job + result, errors, key hints)
//
// Dialogs (stop/restart/restore/quit confirm + the two-step input prompts)
// swap to a centered modal root — the [y]/[n] [enter]/[esc] contract and
// the "everything else is NO" security default are unchanged.

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ---- palette (tcell colors; this tcell build composes RGB via ColorIsRGB) ----

const (
	colBg     = tcell.Color(tcell.ColorIsRGB|tcell.ColorValid | 0x101014)
	colPane   = tcell.Color(tcell.ColorIsRGB|tcell.ColorValid | 0x181820)
	colBar    = tcell.Color(tcell.ColorIsRGB|tcell.ColorValid | 0x1C1C24)
	colDim    = tcell.Color(tcell.ColorIsRGB|tcell.ColorValid | 0x8A8A9A)
	colMain   = tcell.Color(tcell.ColorIsRGB|tcell.ColorValid | 0xD4D4DC)
	colAccent = tcell.Color(tcell.ColorIsRGB|tcell.ColorValid | 0x464688)
	colSel    = tcell.Color(tcell.ColorIsRGB|tcell.ColorValid | 0x303058)
	colOK     = tcell.Color(tcell.ColorIsRGB|tcell.ColorValid | 0x50C050)
	colErr    = tcell.Color(tcell.ColorIsRGB|tcell.ColorValid | 0xD05050)
	colShell  = tcell.ColorBlack
)

// sty — the tcell style helper (tcell.Style is a value type with
// Foreground/Background/Bold methods; Style{} is the default).
func sty(fg, bg tcell.Color) tcell.Style {
	return tcell.Style{}.Foreground(fg).Background(bg)
}

type tviewApp struct {
	app     *tview.Application
	a       *app
	rio     io.ReadWriteCloser // the ssh session io (for teardown + closing)
	root    *tview.Grid
	strip   *tview.Flex
	master  *tview.List
	right   *tview.TextView
	status  *tview.TextView
	shellView *tview.TextView
	shellRoot *tview.Grid
	dlgModal *tview.Modal
	dlgInput *tview.InputField
	dlgRoot  *tview.Grid
	menuList *tview.List
	menuRoot *tview.Grid
	curRoot tview.Primitive
	// inLoop: set while the tview event loop is inside our capture (so
	// redraw/render can run inline instead of going through the updates
	// channel — see app.redraw).
	inLoop bool
}

// newTUI builds the widget tree + key routing over the app state model.
func newTUI(a *app, screen tcell.Screen) *tviewApp {
	tv := &tviewApp{a: a}
	tv.app = tview.NewApplication()
	// mouse support (owner 2026-10-07: "the controls don't work at all" — the
	// TUI was keyboard-only; with EnableMouse the master list, menu strip
	// buttons, modal [y]/[n] and input fields all respond to clicks too).
	tv.app.EnableMouse(true)
	if screen != nil {
		tv.app.SetScreen(screen)
	}
	a.tview = tv
	tv.app.SetTitle(" OlliTeX Toolkit · " + a.tk.Project)

	// ---- master list (LEFT pane) -------------------------------------------
	tv.master = tview.NewList()
	tv.master.SetBorder(true)
	tv.master.SetTitle(" S C R E E N S ")
	tv.master.SetTitleColor(colDim)
	tv.master.SetBackgroundColor(colPane)
	tv.master.SetMainTextStyle(sty(colMain, colPane))
	tv.master.SetSecondaryTextStyle(sty(colDim, colPane))
	tv.master.SetSelectedStyle(sty(tcell.ColorWhite, colSel))
	tv.master.SetSelectedFocusOnly(false)
	tv.master.SetHighlightFullLine(true)
	tv.master.SetWrapAround(true)
	tv.master.SetSelectedFunc(func(index int, _ string, _ string, _ rune) {
		if index < 0 || index >= len(a.masterList()) {
			return
		}
		a.dcur = index
		a.openScreen(a.masterList()[index].id)
	})

	// ---- right detail pane ---------------------------------------------------
	tv.right = tview.NewTextView()
	tv.right.SetWordWrap(true)
	tv.right.SetTextColor(colMain)
	tv.right.SetBackgroundColor(colPane)

	// ---- status line ---------------------------------------------------------
	tv.status = tview.NewTextView()
	tv.status.SetTextColor(colMain)
	tv.status.SetBackgroundColor(colBar)

	// ---- menu strip (row 0) ---------------------------------------------------
	tv.strip = tview.NewFlex()
	tv.strip.SetDirection(tview.FlexRow)
	tv.strip.SetBackgroundColor(colBar)
	for _, it := range a.masterList() {
		label := " " + upperFirst(it.id[:1]) + it.id[1:] + " "
		b := tview.NewButton(label)
		b.SetLabelColor(colMain)
		b.SetBackgroundColor(colBar)
		b.SetSelectedFunc(func(id string) func() {
			return func() { a.openScreen(id) }
		}(it.id))
		tv.strip.AddItem(b, 0, 0, false)
	}
	menuBtn := tview.NewButton(" MENU ")
	menuBtn.SetLabelColor(colMain)
	menuBtn.SetBackgroundColor(colBar)
	menuBtn.SetSelectedFunc(func() { tv.showMenu() })
	tv.strip.AddItem(menuBtn, 0, 0, false)
	title := tview.NewTextView().SetText("  OlliTeX Toolkit · " + a.tk.Project + tv.rev())
	title.SetTextColor(colDim)
	title.SetBackgroundColor(colBar)
	tv.strip.AddItem(title, 0, 1, false)

	// ---- the grid root ---------------------------------------------------------
	tv.root = tview.NewGrid()
	tv.root.SetBackgroundColor(colBg)
	tv.root.SetBorder(true)
	tv.root.SetColumns(26, -4)
	tv.root.SetRows(1, -5, 1)
	tv.root.AddItem(tv.strip, 0, 0, 1, 2, 0, 0, false)
	tv.root.AddItem(tv.master, 1, 0, 1, 1, 4, 3, true)
	tv.root.AddItem(tv.right, 1, 1, 1, 1, 4, 3, false)
	tv.root.AddItem(tv.status, 2, 0, 1, 2, 0, 0, false)

	// ---- the shell root (full screen while attached) ---------------------------
	tv.shellView = tview.NewTextView()
	tv.shellView.SetWordWrap(false)
	tv.shellView.SetTextColor(colMain)
	tv.shellView.SetBackgroundColor(colShell)
	tv.shellView.SetTitle(" shell — ctrl-z / esc detach · ctrl+c → the shell ")
	tv.shellView.SetTitleColor(colDim)
	tv.shellRoot = tview.NewGrid()
	tv.shellRoot.SetBackgroundColor(colShell)
	tv.shellRoot.SetColumns(-3)
	tv.shellRoot.AddItem(tv.shellView, 0, 0, 1, 1, 2, 2, true)

	// ---- the dialog root --------------------------------------------------------
	tv.dlgModal = tview.NewModal()
	tv.dlgModal.SetBackgroundColor(tcell.Color(tcell.ColorIsRGB|tcell.ColorValid | 0x14141C))
	tv.dlgModal.SetTextColor(colMain)
	tv.dlgModal.SetButtonStyle(sty(colMain, colPane))
	tv.dlgModal.SetDoneFunc(func(index int, label string) {
		traceFile("DLMODAL done idx=" + fmt.Sprint(index) + " label=" + label + "\n")
		tv.dialogClosed(index == 0)
	})
	tv.dlgInput = tview.NewInputField()
	tv.dlgInput.SetLabel("")
	tv.dlgInput.SetLabelWidth(0)
	tv.dlgInput.SetFieldBackgroundColor(tcell.ColorWhite)
	tv.dlgInput.SetFieldTextColor(tcell.ColorBlack)

	tv.dlgRoot = tview.NewGrid()
	tv.dlgRoot.SetBackgroundColor(colBg)
	tv.dlgRoot.SetRows(1)
	tv.dlgRoot.AddItem(tv.dlgModal, 0, 0, 1, 1, 4, 3, false)

	// ---- the menu root (the classic File/Stack/… tree as the leaf list) --------
	tv.menuList = tview.NewList()
	tv.menuList.SetTitle(" MENU — 1..n, enter/esc ")
	tv.menuList.SetTitleColor(colDim)
	tv.menuList.SetBackgroundColor(colPane)
	tv.menuList.SetMainTextStyle(sty(colMain, colPane))
	tv.menuList.SetSelectedStyle(sty(tcell.ColorWhite, colSel))
	tv.menuList.SetHighlightFullLine(true)
	tv.menuList.SetSelectedFocusOnly(false)
	tv.menuList.SetWrapAround(true)
	for _, mi := range menuLeaves() {
		tv.menuList.AddItem(mi.label, "", 0, func(action string) func() {
			return func() { tv.menuPicked(action) }
		}(mi.action))
	}
	tv.menuRoot = tview.NewGrid()
	tv.menuRoot.SetBackgroundColor(colBg)
	tv.menuRoot.SetColumns(-3)
	tv.menuRoot.AddItem(tv.menuList, 0, 0, 1, 1, 4, 3, true)

	// ---- roots ------------------------------------------------------------------
	// Mouse routing is installed on every root (v0.42 dispatches mouse to
	// the root primitive only — see mouseRoot above):
	//   main   -> strip buttons + master list + right pane
	//   dialog -> the modal's [y]/[n] buttons (tview's own Modal/Form handler)
	//   menu   -> the menu list rows
	tv.app.SetRoot(tv.wrapRoot(tv.root, tv.status, tv.master, tv.right, tv.strip), true)
	tv.app.SetFocus(tv.master)

	// ---- keyboard: the global capture (the mc key map) --------------------------
	tv.app.SetInputCapture(tv.capture)

	// the first frame: initial render (list + panel + status) before the
	// event loop starts — a stale empty frame is exactly the "blank TUI"
	// symptom the owner saw with the legacy runtime
	tv.render(a)
	return tv
}

// mouseRoot — a Primitive wrapper that gives a tview root a MouseHandler
// (v0.42 routes mouse actions to the ROOT primitive only; Grid/Flex have no
// handler of their own, so without this every click dies and the TUI is
// keyboard-only — the owner's "the controls don't work at all"). The route
// dispatches the click to the child element under the cursor; tview's own
// List/Button/Form/Modal/InputField handlers then do the right thing
// (select list rows, press strip buttons, hit dialog [y]/[n]).
type tuiMouseHandler func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive)

type mouseRoot struct {
	tview.Primitive
	route tuiMouseHandler
}

func (m *mouseRoot) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return m.route
}

// containsPoint reports whether the point lies inside the primitive's rect.
func containsPoint(p tview.Primitive, x, y int) bool {
	if p == nil {
		return false
	}
	px, py, pw, ph := p.GetRect()
	if pw <= 0 || ph <= 0 {
		return false
	}
	return x >= px && x < px+pw && y >= py && y < py+ph
}

// mouseRoute builds the root's MouseHandler: the topmost child containing
// the click point receives the event (its native tview handler runs);
// children without a handler of their own (Flex) dispatch into their
// children in turn.
func (tv *tviewApp) mouseRoute(children ...tview.Primitive) tuiMouseHandler {
	return func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		x, y := event.Position()
		traceFile(fmt.Sprintf("MOUSE act=%d at %d,%d children=%d", action, x, y, len(children)))
		for i, ch := range children {
			px, py, pw, ph := ch.GetRect()
			traceFile(fmt.Sprintf("MOUSE child[%d]=%T rect=%d,%d %dx%d", i, ch, px, py, pw, ph))
		}
		// two-step declare+assign: Go forbids self-reference inside a
		// variable's own initializer (even from a nested closure), so the
		// recursion target must pre-exist.
		var deliver func(c tview.Primitive) (bool, tview.Primitive)
		deliver = func(c tview.Primitive) (bool, tview.Primitive) {
			if h := c.MouseHandler(); h != nil {
				cons, cap2 := h(action, event, setFocus)
				traceFile(fmt.Sprintf("MOUSE deliver %T -> consumed=%v capture=%v", c, cons, cap2 != nil))
				return cons, cap2
			}
			if flex, ok := c.(*tview.Flex); ok {
				count := flex.GetItemCount()
				for i := 0; i < count; i++ {
					fc := flex.GetItem(i)
					if fc == nil || !containsPoint(fc, x, y) {
						continue
					}
					if cc, cp := deliver(fc); cc || cp != nil {
						return cc, cp
					}
				}
			}
			return false, nil
		}
		if os.Getenv("TK_TRACE") != "" {
			for _, ch := range children {
				bx, by, bw, bh := ch.GetRect()
				traceFile(fmt.Sprintf("MOUSE childrect %T %d,%d %dx%d", ch, bx, by, bw, bh))
			}
		}
		for i := len(children) - 1; i >= 0; i-- {
			if !containsPoint(children[i], x, y) {
				continue
			}
			if consumed, capture = deliver(children[i]); consumed || capture != nil {
				return consumed, capture
			}
		}
		return false, nil
	}
}

// wrapRoot installs the mouse routing on a root primitive.
func (tv *tviewApp) wrapRoot(p tview.Primitive, children ...tview.Primitive) tview.Primitive {
	return &mouseRoot{Primitive: p, route: tv.mouseRoute(children...)}
}

// upperFirst — "stop"→"Stop" (no strings.Title: deprecated/removed in recent Go).
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// rev — the version suffix for the strip (empty when not ldflagged).
func (tv *tviewApp) rev() string {
	if tv.a != nil && tv.a.tk != nil && tv.a.tk.Ver != "" {
		return " · " + tv.a.tk.Ver
	}
	return ""
}

// tearDown — end the TUI session WITHOUT tcell's Fini path: tcell's own
// Fini does disengage() → t.wg.Wait() while the inputLoop is blocked in
// tty.Read on the ssh session — a guaranteed deadlock (the read only ends
// when the other side closes/errors). So we write the terminal teardown
// ourselves (alt-screen out, cursor show, mouse + paste protocols off,
// style reset) and close the stream; tview's inputLoop then sees the read
// error and the Run loop exits on its own (clean, no leak). This is why
// `q→y` used to hang the session forever (TK T10).
func (tv *tviewApp) tearDown() {
	rio := tv.rio
	if rio == nil {
		if tv.a != nil && tv.a.tview != nil {
			tv.a.tview.app.Stop()
		}
		return
	}
	_, _ = rio.Write([]byte("[?1006l" + // exit SGR mouse
		"[?1000l" + // exit X10 mouse
		"[?2004l" + // exit bracketed paste
		"[?25h" +   // show cursor
		"(B" +         // ascii charset
		"[0m" +        // reset style
		"[?1049l"))    // leave alternate screen LAST
	_ = rio.Close()
}

// SetRIO — bind the ssh session io (server.go, after SessionScreen).
func (tv *tviewApp) SetRIO(rio io.ReadWriteCloser) { tv.rio = rio }

// ---- root switching -----------------------------------------------------------

func (tv *tviewApp) setRoot(p tview.Primitive) {
	// install (idempotent) mouse routing per root flavour
	switch p {
	case tv.dlgRoot:
		p = tv.wrapRoot(p, tv.dlgModal)
	case tv.menuRoot:
		p = tv.wrapRoot(p, tv.menuList)
	case tv.shellRoot:
		p = tv.wrapRoot(p, tv.shellView)
	case tv.root:
		p = tv.wrapRoot(p, tv.status, tv.master, tv.right, tv.strip)
	}
	tv.app.SetRoot(p, true)
	tv.curRoot = p
}

func (tv *tviewApp) gridRoot() tview.Primitive    { return tv.root }
func (tv *tviewApp) shellRootRef() tview.Primitive { return tv.shellRoot }
func (tv *tviewApp) dlgRootRef() tview.Primitive   { return tv.dlgRoot }
func (tv *tviewApp) menuRootRef() tview.Primitive  { return tv.menuRoot }

// requestPaint — ask the event loop to redraw (from inside the loop this
// is a no-op: tview already draws after every captured key).
func (tv *tviewApp) requestPaint() {
	if tv.inLoop {
		return
	}
	tv.schedule(func() {})
}

// schedule — run fn on the tview event loop (coalesced redraw).
func (tv *tviewApp) schedule(fn func()) {
	// NON-BLOCKING on purpose: capture runs INSIDE the event loop, and
	// QueueUpdateDraw blocks the caller until the loop runs f — a
	// caller-in-the-loop deadlock that kills every control (the owner's
	// "controls don't work at all", 2026-10-07 e2e-caught).
	go tv.app.QueueUpdateDraw(fn)
}

// render — publish the app state into the widgets (one retained-mode frame).
func (tv *tviewApp) render(a *app) {
	if os.Getenv("TK_TRACE") != "" {
		traceFile("RENDER painting=" + a.screen + "\n")
	}
	// master list (rebuild only when the count moved; refresh text in place)
	items := a.masterList()
	if tv.master.GetItemCount() != len(items) {
		tv.master.Clear()
		for _, it := range items {
			tv.master.AddItem(it.label, it.detail, 0, func() {})
		}
	} else {
		for i, it := range items {
			tv.master.RemoveItem(i)
			tv.master.InsertItem(i, it.label, it.detail, 0, func() {})
		}
	}
	if a.dcur >= 0 && a.dcur < len(items) {
		tv.master.SetCurrentItem(a.dcur)
	}

	// the status line (job/result; the key hints when idle)
	st := ""
	if a.loading {
		st = " " + a.jobName + " …   "
	} else if a.errMsg != "" {
		st = " " + a.errMsg
	} else if a.statusMsg != "" {
		st = " " + a.statusMsg
	} else {
		st = " " + a.keyStrip()
	}
	tv.status.SetText(st)

	// the live shell stream (the tail TextView)
	if a.screen == "shell" || a.shell != nil {
		text := string(a.shellBuf)
		if a.shellExited {
			text += "\n\n  — connection closed (esc returns) —"
		}
		tv.shellView.SetText(text)
		if tv.curRoot == tview.Primitive(tv.shellRoot) {
			tv.requestPaint()
		}
		return
	}

	// the right pane
	tv.right.SetText(a.rightPane())
}

// ---- dialogs -------------------------------------------------------------------

// showDialog — open the confirm/prompt box as the active root (the classic
// overlay, tview-edition: the modal draws itself centered).
func (tv *tviewApp) showDialog(d *dialog) {
	a := tv.a
	if os.Getenv("TK_TRACE") != "" {
		traceFile("SHOWDIALOG " + d.id + "\n")
	}
	body := " " + strings.ToUpper(d.title) + "\n"
	if d.context != "" {
		body += " " + d.context + "\n"
	}
	body += " " + d.prompt + "\n"
	if d.kind == dlgConfirm {
		body += "\n are you sure?"
	}
	tv.dlgModal.SetText(body)
	tv.dlgModal.ClearButtons()

	// the prompt line (the classic input field under the prompt)
	if d.kind == dlgPrompt {
		tv.dlgInput.SetText(d.value)
		tv.dlgRoot.SetRows(0, 0)
		tv.dlgRoot.AddItem(tv.dlgModal, 0, 0, 1, 1, 3, 3, false)
		tv.dlgRoot.AddItem(tv.dlgInput, 1, 0, 1, 1, 0, 0, true)
		tv.dlgModal.AddButtons([]string{"enter  [enter]", "cancel  [esc]"})
		tv.app.SetFocus(tv.dlgInput)
	} else {
		tv.dlgRoot.SetRows(1)
		tv.dlgRoot.AddItem(tv.dlgModal, 0, 0, 1, 1, 4, 3, false)
		tv.dlgModal.AddButtons([]string{"yes  [y]", "no  [n]"})
		tv.app.SetFocus(tv.dlgModal)
	}

	a.errMsg = ""
	tv.setRoot(tv.dlgRootRef())
	tv.requestPaint()
}

// dialogClosed — the modal buttons + direct keys funnel here (confirmed
// bool = the first-button path: [y]/[enter]).
func (tv *tviewApp) dialogClosed(confirmed bool) {
	a := tv.a
	d := a.dlg
	traceFile("DLCLOSED confirmed=" + fmt.Sprint(confirmed) + " dlgid=" + func() string { if d == nil { return "nil" }; return d.id }() + "\n")
	a.dlg = nil
	tv.setRoot(tv.gridRoot())
	tv.app.SetFocus(tv.master)
	if d == nil {
		return
	}
	if !confirmed {
		a.editKey, a.editVal, a.editMask = "", "", false // the mc default is NO
		if d.onNo != nil {
			d.onNo()
		}
		a.redraw()
		return
	}
	if d.kind == dlgConfirm {
		if d.onYes != nil {
			d.onYes()
		}
	} else {
		v := tv.dlgInput.GetText()
		if d.onYesText != nil {
			d.onYesText(strings.TrimSpace(v))
		}
	}
	a.redraw()
}

// ---- the full menu ---------------------------------------------------------------

func (tv *tviewApp) showMenu() {
	tv.menuList.SetCurrentItem(0)
	tv.setRoot(tv.menuRootRef())
	tv.app.SetFocus(tv.menuList)
}

func (tv *tviewApp) menuPicked(action string) {
	a := tv.a
	a.dlg = nil
	tv.setRoot(tv.gridRoot())
	tv.app.SetFocus(tv.master)
	a.dispatchMenu(action)
	a.redraw()
}

// ---- key capture (the mc key map, the bubbletea handleKey port) --------------------

func (tv *tviewApp) capture(e *tcell.EventKey) *tcell.EventKey {
	if os.Getenv("TK_TRACE") != "" {
		traceFile("CAPTURE key=" + evKey(e) + "\n")
	}
	a := tv.a
	k := evKey(e)
	tv.inLoop = true
	defer func() { tv.inLoop = false }()

	// 1) the dialog owns the keyboard while open (the mc security contract:
	//    [y]/[n] for confirm, [enter]/[esc] for the prompt, everything
	//    else is NO).
	if a.dlg != nil {
		switch k {
		case "enter":
			tv.dialogClosed(true)
			return nil
		case "y":
			if a.dlg.kind == dlgConfirm {
				tv.dialogClosed(true)
				return nil
			}
		case "n", "esc", "q", "ctrl+c", "space":
			tv.dialogClosed(false)
			return nil
		}
		if a.dlg.kind == dlgPrompt {
			if r, ok := evRune(e); ok {
				tv.dlgInput.SetText(tv.dlgInput.GetText() + string(r))
				return nil
			}
			if k == "backspace" {
				v := tv.dlgInput.GetText()
				if len(v) > 0 {
					tv.dlgInput.SetText(v[:len(v)-1])
				}
				return nil
			}
			return nil
		}
		if k == "enter" && tv.dlgModal.HasFocus() {
			// the modal's own [enter] = the first button (y)
			tv.dialogClosed(true)
			return nil
		}
		return nil // swallow: NO (the default)
	}

	// 2) the shell owns the keyboard while attached
	if a.screen == "shell" && a.shell != nil {
		switch k {
		case "ctrl+z", "esc":
			a.endShell()
			a.shellExited = true
			a.screen = "dashboard"
			a.statusMsg = "shell detached"
			tv.setRoot(tv.gridRoot())
			tv.app.SetFocus(tv.master)
			a.redraw()
			return nil
		case "ctrl+c":
			a.shellForward("\x03")
			return nil
		}
		if r, ok := evRune(e); ok {
			a.shellForward(string(r))
			return nil
		}
		if seq := arrowSeq(k); seq != "" {
			a.shellForward(seq)
			return nil
		}
		return nil // swallow the rest inside the shell (no global nav)
	}

	// 3) the menu list root: esc/q/enter handled here, nav by the list
	if tv.curRoot == tview.Primitive(tv.menuRoot) {
		switch k {
		case "esc", "q", "ctrl+c":
			tv.setRoot(tv.gridRoot())
		(tv.app).SetFocus(tv.master)
			return nil
		case "enter":
			return e // the List fires the current item's selected func
		}
		return e // arrows: the List handles them
	}

	// 4) global keys (the classic mc order)
	switch k {
	case "f10", "f9":
		tv.showMenu()
		return nil
	case "f1", "?":
		a.gotoScreen("about")
		a.errMsg = ""
		a.redraw()
		return nil
	case "ctrl+c":
		if a.screen != "dashboard" {
			a.gotoScreen("dashboard")
			a.errMsg = ""
			a.redraw()
			return nil
		}
		if a.stackUpNow() {
			a.askQuit()
			tv.showDialog(a.dlg)
			return nil
		}
		tv.tearDown() // app.Stop() deadlocks: see tearDown
		return nil
	case "q":
		if a.quitNow() {
			tv.tearDown() // app.Stop() deadlocks: see tearDown
			return nil
		}
		return nil
	case "esc":
		a.escSemantics()
		if a.dlg != nil {
			tv.showDialog(a.dlg)
		} else {
			if tv.curRoot != tview.Primitive(tv.root) {
				tv.setRoot(tv.gridRoot())
			}
			tv.app.SetFocus(tv.master)
		}
		a.redraw()
		return nil
	}

	// 5) the per-screen action keys
	if a.handleScreenKey(k) {
		a.redraw()
		return nil
	}
	return e // hand back to the focused widget (List arrows, etc.)
}

// handleScreenKey — the per-screen one-letter actions (the mc key map),
// now callable from both the key capture and the tests.
func (a *app) handleScreenKey(k string) bool {
	items := a.masterList()
	if len(items) == 0 {
		return false
	}

	// TWO-PANE FOCUS (the owner's two-pane contract): on settings the right
	// pane owns j/k; tab or an arrow hops between the panes.
	if a.screen == "settings" {
		if k == "tab" || k == "left" || k == "right" {
			if k == "tab" {
				a.focus = 1 - a.focus
			} else if k == "left" {
				a.focus = 0
			} else {
				a.focus = 1
			}
			return true
		}
		if a.focus == 1 {
			if a.settingsNav(k, nil) {
				return true
			}
		}
	}

	// master-list navigation — the LEFT pane (j/k/up/down/1-8/enter/space)
	switch k {
	case "j", "down":
		a.dcur = (a.dcur + 1) % len(items)
		a.openScreen(items[a.dcur].id)
		return true
	case "k", "up":
		a.dcur = (a.dcur - 1 + len(items)) % len(items)
		a.openScreen(items[a.dcur].id)
		return true
	case "1", "2", "3", "4", "5", "6", "7", "8", "9", "0":
		// 1-based screen digits across ALL ten screens: 1=Dashboard …
		// 9=Backup, 0=About (the 8-screen era map is extended — the list
		// has ten rows now, and "6 jumps to Doctor" would have been a lie).
		idx := map[string]int{
			"1": 0, "2": 1, "3": 2, "4": 3, "5": 4,
			"6": 5, "7": 6, "8": 7, "9": 8, "0": 9,
		}[k]
		if idx < len(items) {
			a.dcur = idx
			a.openScreen(items[idx].id)
			return true
		}
	}
	switch k {
	case "enter", "space":
		a.openScreen(items[a.dcur].id)
		return true
	}

	// the per-screen actions (the classic key map, ported)
	switch a.screen {
	case "dashboard":
		switch k {
		case "u", "s":
			a.gotoScreen("stack")
			a.runJob("start", a.tk.StackUp)
			return true
		case "d", "t":
			a.askStop()
			if a.tview != nil {
				a.tview.showDialog(a.dlg)
			}
			return true
		}
	case "stack":
		switch k {
		case "u", "s":
			a.runJob("start", a.tk.StackUp)
			return true
		case "d", "t":
			a.askStop()
			if os.Getenv("TK_TRACE") != "" {
				traceFile("ASKSTOP dlg=" + fmt.Sprint(a.dlg != nil) + "\n")
			}
			if a.tview != nil && a.dlg != nil {
				a.tview.showDialog(a.dlg)
			}
			return true
		case "r":
			a.askRestart()
			if a.tview != nil {
				a.tview.showDialog(a.dlg)
			}
			return true
		case "p":
			a.runJob("pull", a.tk.PullImages)
			return true
		case "m":
			a.startShell(Shells[0].Label)
			return true
		case "g":
			a.startShell(Shells[1].Label)
			return true
		case "e":
			a.startShell(Shells[2].Label)
			return true
		}
	case "shells":
		for i, s := range Shells {
			if k == "enter" || k == "x"+digit(i) {
				a.startShell(s.Label)
				return true
			}
		}
	case "logs":
		switch k {
		case "f", "enter":
			a.refreshLogs(a.logName)
			return true
		case "left", "h":
			a.logCycle(-1)
			return true
		case "right", "l":
			a.logCycle(1)
			return true
		}
	case "actions":
		a.actionsKey(k)
		return true
	case "doctor":
		if k == "r" {
			a.runDoctor()
			return true
		}
	case "hub":
		switch k {
		case "r", "enter":
			a.refreshHub()
			return true
		case "t":
			if a.hubSample == 10 {
				a.hubSample = 20
			} else {
				a.hubSample = 10
			}
			a.refreshHub()
			return true
		}
	case "backup":
		switch k {
		case "b", "enter":
			a.runJob("backup", func(ctx context.Context) (string, error) {
				if a.tk.Store == nil {
					return "", ctx.Err()
				}
				m, err := a.tk.Store.Dump(a.tk.BackupPath())
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%d keys", len(m)), nil
			})
			return true
		case "r":
			a.askRestore()
			if a.tview != nil {
				a.tview.showDialog(a.dlg)
			}
			return true
		case "d":
			a.runJobLong("data-backup", func(ctx context.Context) (string, error) {
				return runBackupScript(a.tk.BackupDir(), "backup-data.sh", ctx)
			})
			return true
		case "v":
			a.runJobLong("data-drill", func(ctx context.Context) (string, error) {
				return runBackupScript(a.tk.BackupDir(), "restore-drill.sh", ctx)
			})
			return true
		}
	}
	return false
}

// evKey — the tcell key → the classic mc key string.
func evKey(e *tcell.EventKey) string {
	switch e.Key() {
	case tcell.KeyRune:
		if r := e.Rune(); r != 0 {
			if r == ' ' {
				return "space"
			}
			return string(r)
		}
		return ""
	case tcell.KeyEnter:
		return "enter"
	case tcell.KeyEsc:
		return "esc"
	case tcell.KeyBackspace2, tcell.KeyBackspace:
		return "backspace"
	case tcell.KeyDelete:
		return "delete"
	case tcell.KeyUp:
		return "up"
	case tcell.KeyDown:
		return "down"
	case tcell.KeyLeft:
		return "left"
	case tcell.KeyRight:
		return "right"
	case tcell.KeyTab:
		return "tab"
	case tcell.KeyCtrlC:
		return "ctrl+c"
	case tcell.KeyCtrlZ:
		return "ctrl+z"
	case tcell.KeyF1:
		return "f1"
	case tcell.KeyF9:
		return "f9"
	case tcell.KeyF10:
		return "f10"
	}
	return ""
}

// evRune — the printable rune (for the shell forwarding + the prompt field).
func evRune(e *tcell.EventKey) (rune, bool) {
	r := e.Rune()
	if e.Key() == tcell.KeyRune && r != 0 {
		return r, true
	}
	return 0, false
}

// arrowSeq — the terminal escape sequences the shells expect.
func arrowSeq(k string) string {
	switch k {
	case "up":
		return "\x1b[A"
	case "down":
		return "\x1b[B"
	case "right":
		return "\x1b[C"
	case "left":
		return "\x1b[D"
	}
	return ""
}

// quitNow — "q" is the unambiguous exit (the mc contract, no fallback window).
func (a *app) quitNow() bool {
	if a.screen != "dashboard" {
		a.gotoScreen("dashboard")
		return a.quitNow()
	}
	if a.stackUpNow() {
		a.askQuit()
		if a.tview != nil {
			a.tview.showDialog(a.dlg)
		}
		return false
	}
	a.endShell()
	return true
}

// NewTUI — export the TUI builder (main.go's `ui`/`tui` path). The screen
// is optional: an SSH session injects the session-backed screen there (the
// wish session pipe is not a /dev/tty — the stdio screen cannot Start),
// while the local path runs on the default terminal screen.
func NewTUI(a *app, screen ...tcell.Screen) *tviewApp {
	if a.tview != nil {
		return a.tview
	}
	var sc tcell.Screen
	if len(screen) > 0 && screen[0] != nil {
		sc = screen[0]
	}
	return newTUI(a, sc)
}

// traceFile — TK_TRACE=1 diagnostic spew to /tmp/tktrace.log
func traceFile(line string) {
	if os.Getenv("TK_TRACE") == "" {
		return
	}
	f, err := os.OpenFile("/tmp/tktrace.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.WriteString(line + "\n")
		f.Close()
	}
}

// Run — start the tview event loop (blocks until quit).
func (tv *tviewApp) Run() error {
	if os.Getenv("TK_TRACE") != "" {
		traceFile("RUN start (local path)")
	}
	err := tv.app.Run()
	if os.Getenv("TK_TRACE") != "" {
		traceFile("RUN end err=" + fmt.Sprint(err))
	}
	return err
}

// ---- the SSH-session screen (no /dev/tty on the server side) ---------------

// sessTty implements tcell.Tty over any io.ReadWriteCloser (the wish ssh
// session). Start/Stop are no-ops: the ssh pty is already a terminal as
// seen by the client, and the server side is a plain pipe — raw mode is
// not negotiable here and not needed (the client does line discipline).
type sessTty struct {
	rio   io.ReadWriteCloser
	mu    sync.Mutex
	cols  int
	rows  int
	resCb func()
}

// SetSize — the client's live window size (the ssh window-change channel);
// updates the reported size and re-fires the screen's resize callback so
// tcell re-reads WindowSize and redraws.
func (t *sessTty) SetSize(cols, rows int) {
	t.mu.Lock()
	if cols > 0 {
		t.cols = cols
	}
	if rows > 0 {
		t.rows = rows
	}
	cb := t.resCb
	t.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func (t *sessTty) Start() error  { return nil }
func (t *sessTty) Stop() error   { return nil }
func (t *sessTty) Drain() error  { return nil }
func (t *sessTty) Close() error  { return t.rio.Close() }
func (t *sessTty) Read(p []byte) (int, error)  { return t.rio.Read(p) }
func (t *sessTty) Write(p []byte) (int, error) { return t.rio.Write(p) }
func (t *sessTty) NotifyResize(cb func()) { t.resCb = cb }
func (t *sessTty) WindowSize() (tcell.WindowSize, error) {
	t.mu.Lock()
	c, r := t.cols, t.rows
	t.mu.Unlock()
	if c <= 0 {
		c = 80
	}
	if r <= 0 {
		r = 24
	}
	return tcell.WindowSize{Width: c, Height: r}, nil
}

// SessionScreen builds the tcell screen for an io.ReadWriteCloser (the ssh
// session). envCols/envRows are the negotiated size when known (0 = 80x24).
// SessionScreen builds the tcell screen for a io.ReadWriteCloser (the ssh
// session). envCols/envRows are the negotiated size when known (0 = 80x24).
// The returned tty exposes SetSize for the client's live window changes.
func SessionScreen(rio io.ReadWriteCloser, cols, rows int) (tcell.Screen, *sessTty, error) {
	if os.Getenv("TK_TRACE") != "" {
		traceFile("SCREEN TERM=" + os.Getenv("TERM") + " COLORTERM=" + os.Getenv("COLORTERM") + " TCELL_TRUECOLOR=" + os.Getenv("TCELL_TRUECOLOR") + "\n")
	}
	// 24-bit color: tcell only fabricates the RGB sequences for a 256-color
	// terminfo when COLORTERM=truecolor/24bit/24-bit or TCELL_TRUECOLOR is
	// set; neither arrives over the SSH channel (env requests are session
	// scoped, and most clients do not send COLORTERM), so a plain
	// xterm-256color lookup resolves to indexed-only output and the whole
	// palette collapses to ~16 basic colors (the "monocolor TUI" the
	// owner saw). Force the knob for this process before the terminfo
	// lookup so the dark-navy palette renders as designed. Modern
	// terminals (xterm, tmux, kitty, iTerm2, foot) all speak truecolor.
	if v := os.Getenv("TCELL_TRUECOLOR"); v == "" || v == "disable" {
		os.Setenv("TCELL_TRUECOLOR", "true")
	}
	if os.Getenv("COLORTERM") == "" {
		os.Setenv("COLORTERM", "truecolor")
	}
	tty := &sessTty{rio: rio, cols: cols, rows: rows}
	screen, err := tcell.NewTerminfoScreenFromTty(tty)
	if err != nil {
		traceFile("SCREEN first attempt failed: " + err.Error() + " -> forcing TERM=xterm-256color\n")
		if serr := os.Setenv("TERM", "xterm-256color"); serr != nil {
			return nil, nil, err
		}
		traceFile("SCREEN TERM=" + os.Getenv("TERM") + "\n")
		screen, err = tcell.NewTerminfoScreenFromTty(tty)
		if err != nil {
			return nil, nil, err
		}
	} else {
		traceFile("SCREEN first attempt OK with TERM=" + os.Getenv("TERM") + "\n")
	}
	// init-once: tcell v2.8.1's Init is NOT idempotent — a second Init
	// re-runs t.cells.Resize(terminfo-default 80x24) and then t.resize()
	// early-returns (t.w/t.h already equal the tty size), leaving a screen
	// wider than 80 with an 80-wide cell buffer. The first draw then spins
	// forever in drawCell — GetContent returns width 0 for the out-of-buffer
	// cells and `x += width-1` never advances (TK H3: every size except
	// 80x24 died at boot; 80x24 survived because the shrink no-ops there).
	// Both our server's explicit Init and tview's SetScreen call Init, so
	// the second one must be skipped.
	return &onceInitScreen{Screen: screen}, tty, nil
}

// onceInitScreen — delegate everything, Init only once (see SessionScreen).
type onceInitScreen struct {
	tcell.Screen
	inited bool
}

func (s *onceInitScreen) Init() error {
	if s.inited {
		return nil
	}
	s.inited = true
	return s.Screen.Init()
}
