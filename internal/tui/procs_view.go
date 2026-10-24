package tui

import (
	"fmt"
	"strings"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Вид экрана процессов: шапка колонок, список и карточка выбранного приложения со своей таблицей.

func (p *procsScreen) View(width, height int) string {
	var out []string
	if !p.inTabs {
		left := "   " + p.SearchLine()
		right := styleDim.Render(p.OrderText()) + " "
		out = append(out, left+strings.Repeat(" ", max(1, width-ui.Width(left)-ui.Width(right)))+right)
	}
	if p.warning != "" {
		out = append(out, "   "+styleAmber.Render(symWarn+" "+p.warning))
	}
	out = append(out, "")
	if p.loading && len(p.data.Groups) == 0 {
		return strings.Join(append(out, "   "+styleDim.Render("смотрю, кто что занял")), "\n")
	}
	head := ui.Fit("ПРИЛОЖЕНИЕ", 28) + fmt.Sprintf("%6s%9s%7s", "CPU "+sortMark(true), "ПАМЯТЬ", "ПРОЦ")
	if p.byMem {
		head = ui.Fit("ПРИЛОЖЕНИЕ", 28) + fmt.Sprintf("%9s%8s%7s", "ПАМЯТЬ "+sortMark(true), "CPU", "ПРОЦ")
	}
	if len(p.shown) == 0 {
		out = append(out, "   "+styleHead.Render(head),
			"   "+styleDim.Render("ничего похожего на \""+strings.TrimSpace(p.input.Value())+"\" из "+itoa(len(p.data.Groups))))
		return strings.Join(out, "\n")
	}
	room := height - len(out) - 1
	cw, beside := cardWidth(width)
	card := p.card(cw)
	switch {
	case card != nil && beside:
		list := append([]string{"   " + styleHead.Render(head)}, p.list.View(cardListW, room)...)
		out = append(out, besideList(list, card.Lines(cw, room+1), width)...)
	case card != nil:
		lines := card.Lines(cw, max(6, room/3))
		out = append(out, "   "+styleHead.Render(head))
		out = append(out, p.list.View(width, room-len(lines)-1)...)
		out = append(out, "")
		for _, l := range lines {
			out = append(out, "   "+l)
		}
	default:
		out = append(out, "   "+styleHead.Render(head))
		out = append(out, p.list.View(width, room)...)
	}
	return strings.Join(out, "\n")
}

// card: процессы выбранного приложения своей таблицей, совет и действия.
func (p *procsScreen) card(w int) *Card {
	cur := p.current()
	if cur == nil {
		return nil
	}
	c := &Card{Title: cur.Name, Sub: ui.Count(len(cur.Procs), "процесс", "процесса", "процессов") + " " + symSep + " " + ui.Short(cur.Age)}
	nameW := max(10, w-2-8-6-3-8)
	c.Extra = append(c.Extra, styleHead.Render(fmt.Sprintf("  %8s%6s   %s%8s", "ПАМЯТЬ", "CPU", ui.Fit("ПРОЦЕСС", nameW), "PID")))
	limit := 3
	if p.expanded {
		limit = 12
	}
	for i, r := range cur.Procs {
		if i >= limit {
			c.Extra = append(c.Extra, styleDim.Render(fmt.Sprintf("и ещё %d %s вместе %s, %d%% памяти машины",
				len(cur.Procs)-limit, symSep, ui.Bytes(cur.RSS), memShare(cur.RSS, p.data.MemTotal))))
			break
		}
		name := strings.Join(strings.Fields(r.Name), " ")
		c.Extra = append(c.Extra, fmt.Sprintf("  %8s%5.0f%%   %s%8s", ui.Bytes(r.RSS), r.CPU, ui.Fit(name, nameW), r.PID))
	}
	if adv := groupAdvice(*cur); adv != "" {
		c.Extra = append(c.Extra, "", styleAmber.Render(ui.Clip(symWarn+" "+adv, w)))
	}
	switch {
	case len(cur.Procs) > 3 && !p.expanded:
		c.Enter = "показать все " + itoa(len(cur.Procs)) + " здесь"
	case p.expanded:
		c.Enter = "свернуть"
	}
	if !cur.Protected {
		c.Actions = []CardAction{{"x", "закрыть бережно", ""}, {"X", "принудительно", ""}}
	}
	return c
}

func memShare(rss, total int64) int {
	if total <= 0 {
		return 0
	}
	return int(rss * 100 / total)
}

// groupAdvice: что с этим приложением не так, словами.
func groupAdvice(g ProcGroup) string {
	switch {
	case g.CPU >= 100 && g.IsApp:
		return fmt.Sprintf("%s и %.0f%% CPU - обычно одна тяжёлая вкладка или задача: закрой её внутри, или приложение здесь", ui.Bytes(g.RSS), g.CPU)
	case g.CPU >= 100:
		return fmt.Sprintf("%.0f%% CPU - занято больше ядра; если это не сборка или индексация, стоит закрыть", g.CPU)
	case g.RSS >= 4<<30:
		return ui.Bytes(g.RSS) + " памяти - больше, чем обычно нужно; перезапуск вернёт память"
	}
	for _, r := range g.Procs {
		if r.Note != "" {
			return r.Note
		}
	}
	return ""
}
