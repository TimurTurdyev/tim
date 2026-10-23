package tui

import (
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Части главного экрана: группы слева, разделы справа, машина и находки поиска.

// groupsView: ВНИМАНИЕ и НЕДАВНЕЕ в левой колонке, не выше room строк. Недавнего меньше пяти -
// его нет; беды не режутся, лишнее недавнее уходит снизу.
func (h *home) groupsView(w, room int) []string {
	var att, rec []int
	for i, r := range h.list.Rows {
		if e := h.hitAt(i); e != nil && !r.Header {
			switch e.Kind {
			case KindAttention:
				att = append(att, i)
			case KindRecent:
				rec = append(rec, i)
			}
		}
	}
	var out []string
	if len(att) > 0 {
		show := att
		if over := len(att) + 1 + 1 + homeMinRecent - room; over > 0 && len(rec) > 0 {
			show = att[:max(1, len(att)-over)]
		}
		head := "   " + styleHead.Render("ВНИМАНИЕ")
		if hidden := len(att) - len(show); hidden > 0 {
			head += styleDim.Render(" " + symSep + " ещё " + itoa(hidden))
		}
		out = append(out, head)
		for _, i := range show {
			out = append(out, h.attentionRow(i, w))
		}
		for _, i := range att[len(show):] {
			h.hidden[i] = true
		}
	}
	fit := room - len(out) - 1
	switch {
	case len(rec) == 0 && h.ready:
		out = append(out, "   "+styleHead.Render("НЕДАВНЕЕ"), "   "+styleHint.Render("проекты и команды, которые открываешь, появятся здесь"))
	case len(rec) > 0 && fit >= min(homeMinRecent, len(rec)):
		out = append(out, "   "+styleHead.Render("НЕДАВНЕЕ"))
		for _, i := range rec[:min(fit, len(rec))] {
			out = append(out, h.recentRow(i, w))
		}
		rec = rec[min(fit, len(rec)):]
	}
	for _, i := range rec {
		h.hidden[i] = true
	}
	return out
}

func (h *home) attentionRow(i, w int) string {
	e := h.hitAt(i)
	sel := i == h.list.Cursor
	text := ui.Fit(e.Title, homeNameW)
	dest := ""
	if w >= homeLeftW {
		dest = styleDim.Render(e.Hint)
	}
	return SelRow(sel, w, text+dest)
}

func (h *home) recentRow(i, w int) string {
	e := h.hitAt(i)
	sel := i == h.list.Cursor
	nameW := homeNameW
	if w < homeLeftW {
		nameW = 29
	}
	edit := ""
	if e.Edit {
		edit = styleDim.Render("правка в :") + "  "
	}
	name := styleText.Render(e.Title)
	if sel {
		name = styleBold.Render(e.Title)
	}
	name = ui.Fit(name, nameW-ui.Width(edit)) + edit
	hint := ui.TrimSegments(strings.Split(e.Hint, " "+symSep+" "), w-3-nameW)
	return SelRow(sel, w, name+styleDim.Render(hint))
}

// sectionsView: разделы справа - кап буквы, название, подсказка и бейдж; на узком без подсказок.
func (h *home) sectionsView(w int, hints bool) []string {
	if len(h.data.Sections) == 0 {
		return nil
	}
	out := []string{" " + styleHead.Render("РАЗДЕЛЫ "+symSep+" буква, потом enter")}
	for _, sc := range h.data.Sections {
		line := styleKeyCap.Render(" "+sc.Key+" ") + " " + styleText.Render(sc.Title)
		if hints {
			line = styleKeyCap.Render(" "+sc.Key+" ") + " " + ui.Fit(styleText.Render(sc.Title), 13) + styleDim.Render(sc.Hint)
		}
		if badge := h.mach.Badges[sc.Key]; badge != "" {
			line += "   " + badge
		}
		out = append(out, ui.Clip(line, w))
	}
	return out
}

// machineView: группа МАШИНА - подпись, значение и куда ведёт; пока сводка собирается - скелет.
// compact - три строки без "запущено" и без назначений.
func (h *home) machineView(width int, compact bool) []string {
	if !h.machOK {
		secs := int(time.Since(h.started).Seconds())
		line := Spinner() + " собираю: машина, сеть, порты"
		if secs >= 1 {
			line += " " + symSep + " " + itoa(secs) + " с"
		}
		out := []string{"   " + styleDim.Render(line)}
		for _, l := range []string{"память", "диск", "сеть", "запущено"} {
			out = append(out, "   "+styleHead.Render(l))
		}
		return out
	}
	var rows []int
	valueW := 0
	for i := range h.list.Rows {
		if e := h.hitAt(i); e != nil && e.Kind == KindMachine {
			if compact && len(rows) == 3 {
				h.hidden[i] = true
				continue
			}
			rows = append(rows, i)
			// Колонка "куда ведёт" - по обычным строкам; длинная строка ставит его сразу за собой.
			if w := ui.Width(e.Note); w <= homeMachineW {
				valueW = max(valueW, w)
			}
		}
	}
	if len(rows) == 0 {
		return nil
	}
	out := []string{"   " + styleHead.Render("МАШИНА")}
	for _, i := range rows {
		e := h.hitAt(i)
		line := ui.Fit(styleAccent.Render(e.Title), 12) + ui.Pad(e.Note, valueW) + "  "
		if !compact && ui.Width(line)+ui.Width(e.Hint)+3 <= width {
			line += styleDim.Render(e.Hint)
		}
		out = append(out, SelRow(i == h.list.Cursor, width-2, line))
	}
	return out
}

// searchView: находки поиска во всю ширину, по группам.
func (h *home) searchView(width, height int) []string {
	if len(h.hits) == 0 {
		return []string{"   " + styleDim.Render("ничего похожего на \""+strings.TrimSpace(h.input.Value())+"\" "+symSep+" enter отдаст в палитру")}
	}
	titleW := 0
	for _, e := range h.hits {
		titleW = max(titleW, ui.Width(e.Title)+2)
	}
	titleW = min(titleW, 34)
	var out []string
	start, end := h.list.window(max(3, height-1))
	if start > 0 {
		out = append(out, styleDim.Render("   "+symUp+" ещё "+itoa(start)+" выше"))
	}
	for i := start; i < end; i++ {
		r := h.list.Rows[i]
		if r.Header {
			out = append(out, "   "+styleHead.Render(strings.ToUpper(r.Text)))
			continue
		}
		e := h.hitAt(i)
		sel := i == h.list.Cursor
		name := styleText.Render(e.Title)
		if sel {
			name = styleBold.Render(e.Title)
		}
		rest := width - 3 - titleW - ui.Width(e.Note)
		out = append(out, SelRow(sel, width, ui.Fit(name, titleW)+ui.Fit(styleDim.Render(e.Hint), max(8, rest-1))+e.Note))
	}
	if end < len(h.list.Rows) {
		out = append(out, styleDim.Render("   "+symDown+" ещё "+itoa(len(h.list.Rows)-end)+" ниже"))
	}
	return out
}
