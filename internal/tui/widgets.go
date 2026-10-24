package tui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Общие детали экранов: полоса, панель в рамке, плитка.

// PaneSection: часть панели под перегородкой с подписью.
type PaneSection struct {
	Label string
	Rows  []string
}

// Pane: панель в рамке; заголовок в верхней кромке, sub - серым рядом, sections - под строками
// через перегородки. Строки обрезаются по ширине.
func Pane(title, sub string, rows []string, width int, sections ...PaneSection) []string {
	inner := width - 4
	head := styleLine.Render("\u256d\u2500 ") + stylePaneT.Render(title)
	if sub != "" {
		head += styleLine.Render(" \u2500 ") + styleDim.Render(sub)
	}
	n := lipgloss.Width(head)
	head += styleLine.Render(" " + strings.Repeat("\u2500", max(0, width-n-2)) + "\u256e")
	out := []string{head}
	line := func(r string) string {
		return styleLine.Render("\u2502 ") + ui.Fit(r, inner) + styleLine.Render(" \u2502")
	}
	for _, r := range rows {
		out = append(out, line(r))
	}
	for _, sec := range sections {
		fill := strings.Repeat("\u2500", max(0, width-lipgloss.Width(sec.Label)-5))
		out = append(out, styleLine.Render("\u251c\u2500 ")+styleDim.Render(sec.Label)+styleLine.Render(" "+fill+"\u2524"))
		for _, r := range sec.Rows {
			out = append(out, line(r))
		}
	}
	out = append(out, styleLine.Render("\u2570"+strings.Repeat("\u2500", width-2)+"\u256f"))
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

// Notice: заметка той же геометрии, что лок-ап, - для загрузки, пустоты и ошибки экрана. Маскот в
// настроении сессии слева, справа заголовок жирным и две строки: ход или числа, факт и последствие.
// Клавиш и команд в заметке нет: они внизу экрана.
func Notice(title, line2, line3 string, width int) []string {
	lines := [][]string{{styleBold.Render(title)}}
	if line2 != "" {
		lines = append(lines, []string{line2})
	}
	if line3 != "" {
		lines = append(lines, []string{styleDim.Render(line3)})
	}
	look := brand.Look{Mood: sessionMood(brand.Calm), Light: ui.Light()}
	if width < 70 {
		out := []string{""}
		for _, l := range lines {
			out = append(out, "   "+ui.TrimSegments(l, width-4))
		}
		return out
	}
	return append([]string{""}, ui.Header(lines, ui.HeaderOpts{Look: look, Indent: 2, Width: width, Sprite: true, Keep: true})...)
}

// progressLine: полоса хода 40 клеток и счётчик с идущими секундами; frac < 0 - доля неизвестна.
func progressLine(frac float64, text string, started time.Time) string {
	line := ui.Progress(frac, 40, animFrame) + "  " + styleDim.Render(text)
	if d := time.Since(started); d >= time.Second {
		line += styleDim.Render(" " + symSep + " " + ui.Short(d))
	}
	return line
}

// WaitNotice: заметка загрузки с полосой хода; frac < 0 - доля неизвестна, бежит бегунок.
func WaitNotice(title string, frac float64, count string, started time.Time, fact string, width int) []string {
	return Notice(title, progressLine(frac, count, started), fact, width)
}
