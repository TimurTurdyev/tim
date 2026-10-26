package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Части доски: строка управления, строки списка, шапка с лампами.

// controlLine: строка управления доски без вкладок - поиск слева, порядок справа.
func (b *board) controlLine(width int) string {
	left := "   " + b.SearchLine()
	right := ""
	if b.opts.Order != "" {
		right = styleDim.Render(b.opts.Order) + " "
	}
	return left + strings.Repeat(" ", max(1, width-ui.Width(left)-ui.Width(right))) + right
}

// listLines: строки списка; у выбранной строки-прозы под ней строка со стрелкой.
func (b *board) listLines(w, h int) []string {
	cur := b.current()
	inline := cur != nil && cur.Card == nil && cur.Title == "" && cur.Inline != ""
	var caps []string
	if inline {
		h--
		for _, a := range b.opts.Actions {
			if !a.Free && !a.Hidden && a.Key != "enter" && a.When != nil && a.When(cur) {
				caps = append(caps, styleKeyCap.Render(" "+a.Key+" ")+" "+styleText.Render(a.Label))
			}
		}
		if len(caps) > 0 {
			h--
		}
	}
	lines := b.list.View(w, max(1, h))
	if !inline {
		return lines
	}
	sel := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, symBar) })
	if sel < 0 {
		return lines
	}
	note := []string{"     " + styleDim.Render(ui.Clip(symArrow+" "+cur.Inline, w-6))}
	if len(caps) > 0 {
		note = append(note, "     "+strings.Join(caps, "   "))
	}
	return slices.Insert(lines, sel+1, note...)
}

// head: шапка и лампы - всё, что над списком.
func (b *board) head(width int) []string {
	var out []string
	for _, h := range b.data.Head {
		out = append(out, "   "+h)
	}
	if len(b.data.Lamps) > 0 {
		for _, l := range ui.Pairs(b.data.Lamps, width-6) {
			out = append(out, "   "+l)
		}
	}
	if len(out) > 0 {
		out = append(out, "")
	}
	return out
}

// clipRows: не больше n строк; последняя говорит, сколько не влезло.
func clipRows(rows []string, n int) []string {
	if len(rows) <= n {
		return rows
	}
	if n < 1 {
		return nil
	}
	return append(slices.Clone(rows[:n-1]), styleDim.Render(fmt.Sprintf("и ещё %d", len(rows)-n+1)))
}
