package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Вид обзора диска: полоса занятости, вкладки разделов, таблица мест и карточка под ней.

func (d *disk) View(width, height int) string {
	if d.loading && len(d.data.Tabs) == 0 {
		done, total, where := d.counted()
		frac := -1.0
		if total > 0 {
			frac = float64(done) / float64(total)
		}
		count := itoa(done) + " из " + itoa(total) + " папок"
		if where != "" {
			count += " в " + where
		}
		out := Notice("Считаю, куда ушло место", progressLine(frac, count, d.started),
			"первый замер около минуты, потом 15 минут берётся готовый; после esc досчитается в фоне", width)
		out = append(out, "", "   "+styleTabOff.Render(" Можно освободить ···   Программы ···   Твои файлы ···   Система ··· "))
		return strings.Join(out, "\n")
	}
	ago := "замер только что"
	if time.Since(d.data.MeasuredAt) >= time.Minute {
		ago = "замер " + ui.Short(time.Since(d.data.MeasuredAt))
	}
	out := []string{"   " + ui.Bar(d.data.UsedPct, 40) + "  " + styleDim.Render(ago+" "+symSep+" r - заново"),
		"   " + d.tabs(width-4), ""}
	out = append(out, "   "+styleHead.Render(fmt.Sprintf("%10s   %-8s  %s", "РАЗМЕР "+sortMark(true), "ДОЛЯ", ui.Fit("МЕСТО", min(34, max(20, width-60))+1)+"ЧЕМ УБРАТЬ")))
	var foot []string
	if d.marked.len() > 0 {
		var sum int64
		var names []string
		for _, r := range d.marked.list() {
			sum += r.Size
			names = append(names, r.Name)
		}
		foot = append(foot, "", "   "+styleBad.Render("отмечено "+itoa(d.marked.len())+" "+symSep+" "+ui.Bytes(sum))+"   "+styleDim.Render(ui.Clip(strings.Join(names, ", "), width-30)))
	}
	for _, n := range d.data.Notes {
		for _, l := range ui.Wrap(n, width-6) {
			foot = append(foot, "   "+styleDim.Render(l))
		}
	}
	room := height - len(out) - len(foot)
	card := d.card()
	cw := width - 6
	cardH := 0
	if card != nil {
		cardH = min(len(card.Lines(cw, room)), max(6, room/3))
	}
	out = append(out, d.list.View(width, max(3, room-cardH-1))...)
	if card != nil {
		out = append(out, "")
		for _, l := range card.Lines(cw, cardH) {
			out = append(out, "   "+l)
		}
	}
	return strings.Join(append(out, foot...), "\n")
}

// card: что это за место, что внутри и что сделает enter.
func (d *disk) card() *Card {
	cur := d.current()
	if cur == nil {
		return nil
	}
	c := &Card{Title: cur.Name, Sub: cur.shown()}
	for i, it := range cur.Items {
		if i >= 6 {
			c.Extra = append(c.Extra, styleDim.Render("и ещё "+itoa(len(cur.Items)-6)))
			break
		}
		note := styleDim.Render(it.Note)
		if it.Stale {
			note = styleAmber.Render(it.Note)
		}
		c.Extra = append(c.Extra, fmt.Sprintf("%9s  %s%s", ui.Bytes(it.Size), ui.Fit(it.Name, 32), note))
	}
	if cur.Warn != "" {
		c.Facts = append(c.Facts, Fact{Label: styleAmber.Render(symWarn + " внимание"), Value: cur.Warn})
	}
	switch {
	case cur.Cmd != "":
		c.Enter = "выполнить " + cur.Cmd + ", спросит подтверждение"
		c.About = append(c.About, styleDim.Render(ui.Bytes(cur.Size)+" "+symSep+" отметить пробелом нельзя - это команда, а не папка"))
	case cur.Removable:
		c.Enter = "внутрь, по папкам"
		c.About = append(c.About, styleDim.Render(ui.Bytes(cur.Size)+" "+symSep+" можно целиком в Корзину"))
	default:
		c.Enter = "внутрь, по папкам"
		if cur.Hint != "" {
			c.Facts = append(c.Facts, Fact{Label: "как убрать", Value: cur.Hint})
		}
	}
	if cur.Path != "" {
		c.Actions = []CardAction{{"o", "Finder", ""}}
	}
	return c
}

// tabs: разделы с размерами; не влезают в width - размер остаётся только у открытого.
func (d *disk) tabs(width int) string {
	render := func(short bool) string {
		var tabs []string
		for i, t := range d.data.Tabs {
			label := " " + t.Title + " "
			if !short || i == d.tab {
				label += ui.Bytes(t.Total) + " "
			}
			if i == d.tab {
				tabs = append(tabs, styleTabOn.Render(label))
			} else {
				tabs = append(tabs, styleTabOff.Render(label))
			}
		}
		return strings.Join(tabs, " ")
	}
	if full := render(false); ui.Width(full) <= width {
		return full
	}
	return render(true)
}
