package toolkit

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Palette — one coherent dark theme (owner: "make it look nice").
var (
	colText   = lipgloss.Color("252")
	colDim    = lipgloss.Color("244")
	colFg     = lipgloss.Color("255")
	colAccent = lipgloss.Color("39")  // cyan — primary actions / selected
	colOK     = lipgloss.Color("42")  // green
	colWarn   = lipgloss.Color("214") // amber
	colErr    = lipgloss.Color("203") // red
	colKey    = lipgloss.Color("250")
	colKeyBG  = lipgloss.Color("236")
)

var (
	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colFg).
			Background(colAccent).
			Padding(0, 1)

	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("51")).
			PaddingBottom(1)

	styleHint = lipgloss.NewStyle().
			Foreground(colDim).
			PaddingTop(1)

	styleBar = lipgloss.NewStyle().
			Background(colKeyBG).
			Foreground(colKey)

	styleSelected = lipgloss.NewStyle().
			Background(colAccent).
			Foreground(lipgloss.Color("16")).
			Bold(true)

	styleDim = lipgloss.NewStyle().Foreground(colDim)

	styleOK   = lipgloss.NewStyle().Bold(true).Foreground(colOK)
	styleWarn = lipgloss.NewStyle().Bold(true).Foreground(colWarn)
	styleErr  = lipgloss.NewStyle().Bold(true).Foreground(colErr)

	styleKey = lipgloss.NewStyle().
			Foreground(colAccent).
			Background(colKeyBG).
			Padding(0, 1)

	stylePanel = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1)

	stylePanelSel = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colAccent).
			Padding(0, 1)

	styleValue = lipgloss.NewStyle().Foreground(colText)
	styleKvK   = lipgloss.NewStyle().Foreground(colDim).Width(24)

	// ---- the classic console set (mc / freebsd-installer direction) --------
	// pane-frame title (the mc window title: " Name " on the top border)
	stylePaneTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colFg).
			Background(colKeyBG)
	// the right-pane section head (mc: the "(Dir)"-style header line)
	styleHead = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("51")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)
	// the classic [F5] key-chip (the bottom keystrip + the action row)
	styleChip = lipgloss.NewStyle().
			Foreground(colAccent).
			Background(colKeyBG).
			Padding(0, 1)
	// the input line inside a dialog (the freebsd-installer "text:" row)
	styleEdit = lipgloss.NewStyle().
			Foreground(colFg).
			Background(lipgloss.Color("236")).
			Padding(0, 1)
	// dialog title bar (the mc prompt-box header)
	styleDlgTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("16")).
			Background(colAccent).
			Padding(0, 1)
)

// width helper
func strwidth(s string) int { return lipgloss.Width(s) }

// okText renders a status word.
func okText(ok bool) string {
	if ok {
		return styleOK.Render("● OK")
	}
	return styleErr.Render("● FAIL")
}

func warnText(ok bool) string {
	if ok {
		return styleOK.Render("ON")
	}
	return styleDim.Render("OFF")
}

func renderListRow(idx, cursor int, label, meta string) string {
	if idx == cursor {
		return styleSelected.Render(">") + " " + styleSelected.Render(label) + styleSelected.Render("  "+meta)
	}
	return "  " + styleValue.Render(label) + styleDim.Render("  "+meta)
}

func hintBar(pairs ...string) string {
	out := ""
	for i := 0; i+1 < len(pairs); i += 2 {
		out += " " + styleKey.Render(pairs[i]) + " " + pairs[i+1]
	}
	return out
}

func titleBar(title string) string {
	return styleTitle.Render(" " + title + " ")
}

// renderWidth fits content to a width by truncating (simple + predictable).
func fit(s string, w int) string {
	if w <= 0 {
		return s
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	// rune-safe truncation with ellipsis
	runes := []rune(s)
	if len(runes) <= 1 {
		return s
	}
	visible := w - 1
	if visible < 0 {
		visible = 0
	}
	return fmt.Sprintf("%s…", string(runes[:visible]))
}
