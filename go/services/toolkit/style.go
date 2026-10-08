package toolkit

import (
	"strings"
)

// style.go — the classic-style palette, ported 2026-10-07 to the tview
// era: the tview retained-mode screen owns the colors (tview.Style per
// widget), so the legacy lipgloss styles are now plain passthrough
// wrappers — the panel text rendered into the tview TextViews stays clean
// (ANSI-free) and the layout primitives (fit/fitRect/wrap) are unchanged.

// textStyle is the legacy lipgloss style surface, reduced to a no-op
// renderer (tview paints the colors itself per primitive).
type textStyle struct{}

func (textStyle) Render(s string) string { return s }

var (
	styleTitle    = textStyle{}
	styleHeader   = textStyle{}
	styleHint     = textStyle{}
	styleBar      = textStyle{}
	styleSelected = textStyle{}
	styleDim      = textStyle{}
	styleOK       = textStyle{}
	styleWarn     = textStyle{}
	styleErr      = textStyle{}
	styleKey      = textStyle{}
	stylePanel    = textStyle{}
	stylePanelSel = textStyle{}
	styleValue    = textStyle{}
	styleKvK      = textStyle{}
	stylePaneTitle = textStyle{}
	styleHead     = textStyle{}
	styleChip     = textStyle{}
	styleEdit     = textStyle{}
	styleDlgTitle = textStyle{}
)

// dispW — display width (rune count; the toolkit text is ASCII/emoji-light).
func dispW(s string) int { return len([]rune(s)) }

// fit truncates each line to w columns (tail-keep is fitRect's job).
func fit(s string, w int) string {
	return fitRect(s, w, 0)
}

func fitRect(s string, w, h int) string {
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	if h > 0 && len(lines) > h {
		lines = lines[len(lines)-h:]
	}
	var out []string
	for _, l := range lines {
		if w <= 0 || dispW(l) <= w {
			out = append(out, l)
			continue
		}
		var b strings.Builder
		used := 0
		for _, r := range l {
			if used+1 > w-1 {
				break
			}
			b.WriteRune(r)
			used++
		}
		out = append(out, b.String()+"…")
	}
	return strings.Join(out, "\n")
}

// okText — the classic ✓/✗ status marks (ANSI-free for the tview TextViews).
func okText(ok bool) string {
	if ok {
		return "✓"
	}
	return "✗"
}
