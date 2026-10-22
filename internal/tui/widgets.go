package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Общие детали экранов: полоса, панель в рамке, плитка.

// Bar: полоса на n клеток; цвет по порогу - до 60% спокойный, до 85% внимание, выше плохо.
func Bar(frac float64, n int) string {
	f := max(0, min(n, int(frac*float64(n)+0.5)))
	st := styleAccent
	switch {
	case frac >= 0.85:
		st = styleBad
	case frac >= 0.6:
		st = styleAmber
	}
	return st.Render(strings.Repeat(symBlock, f)) + styleGhost.Render(strings.Repeat(symBlock, n-f))
}

// PaneSection: часть панели под перегородкой с подписью.
type PaneSection struct {
	Label string
	Rows  []string
}

// Pane: панель в рамке; заголовок в верхней кромке, sub - серым рядом, sections - под строками
// через перегородки. Строки обрезаются по ширине.
func Pane(title, sub string, rows []string, width int, sections ...PaneSection) []string {
	inner := width - 4
	head := styleDim.Render("\u256d\u2500 ") + stylePaneT.Render(title)
	if sub != "" {
		head += styleDim.Render(" \u2500 " + sub)
	}
	n := lipgloss.Width(head)
	head += styleDim.Render(" " + strings.Repeat("\u2500", max(0, width-n-2)) + "\u256e")
	out := []string{head}
	line := func(r string) string {
		return styleDim.Render("\u2502 ") + ui.Fit(r, inner) + styleDim.Render(" \u2502")
	}
	for _, r := range rows {
		out = append(out, line(r))
	}
	for _, sec := range sections {
		fill := strings.Repeat("\u2500", max(0, width-lipgloss.Width(sec.Label)-5))
		out = append(out, styleDim.Render("\u251c\u2500 "+sec.Label+" "+fill+"\u2524"))
		for _, r := range sec.Rows {
			out = append(out, line(r))
		}
	}
	out = append(out, styleDim.Render("\u2570"+strings.Repeat("\u2500", width-2)+"\u256f"))
	return out
}

// Beside ставит панель справа от строк списка, строка к строке.
func Beside(left []string, leftW int, right []string, width int) []string {
	n := max(len(left), len(right))
	out := make([]string, n)
	for i := range out {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = ui.Fit(l, leftW) + " " + ui.Fit(r, width-leftW-1)
	}
	return out
}
