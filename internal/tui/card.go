package tui

import (
	"slices"
	"strings"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Карточка выбранной строки - без рамки: линия, имя и подзаголовок, путь, факты с подписью, что
// сделает enter, линия, действия капами. Только то, чего нет в строке списка. Рамка остаётся окнам
// поверх экрана.

// Ширина карточки справа: W-62, но не уже 48 и не шире 64. Справа она стоит, когда окно от 110
// колонок, иначе - под списком.
const (
	cardMinW    = 48
	cardMaxW    = 64
	cardBeside  = 110
	cardListW   = 56 // список слева от карточки
	cardGap     = 3
	factLabelW  = 12 // подпись 10 и 2 пробела, длинное значение переносится с этого отступа
	cardActionW = 15 // слот капа действия
)

// Fact: строка карточки - подпись и значение; пустая подпись - продолжение значения выше.
type Fact struct{ Label, Value string }

// CardAction: действие над строкой - кап в карточке; Note - последствие серым ("закроет tim").
type CardAction struct{ Key, Label, Note string }

// Card: карточка выбранного.
type Card struct {
	Title, Sub string // имя жирным и подзаголовок серым справа в той же строке
	Path       string
	About      []string // проза под путём: что это за место и чем оно опасно, переносится по ширине
	Facts      []Fact
	Extra      []string // блок после фактов, уже с цветом: например последние строки журнала
	Enter      string   // что сделает enter, честно
	Actions    []CardAction
	Loading    bool // факты ещё считаются
}

// cardWidth: ширина карточки справа и стоит ли она справа.
func cardWidth(width int) (int, bool) {
	if width < cardBeside {
		return width - 6, false
	}
	return max(cardMinW, min(cardMaxW, width-62)), true
}

// Lines: карточка шириной w не выше maxH строк. Не влезает - сначала сворачиваются факты, потом путь;
// имя, строка enter и капы остаются всегда.
func (c Card) Lines(w, maxH int) []string {
	line := styleLine.Render(strings.Repeat("\u2500", w))
	head := styleBold.Render(c.Title)
	if c.Sub != "" {
		head = ui.Clip(head, w-ui.Width(c.Sub)-2)
		head += strings.Repeat(" ", max(1, w-ui.Width(head)-ui.Width(c.Sub))) + styleDim.Render(c.Sub)
	}
	top := []string{line, head}
	path := []string{}
	if c.Path != "" {
		path = append(path, styleDim.Render(ui.Clip(c.Path, w)))
	}
	for _, a := range c.About {
		path = append(path, ui.Wrap(a, w)...)
	}
	var facts []string
	if c.Loading {
		facts = append(facts, styleDim.Render("смотрю"))
	}
	for _, f := range c.Facts {
		facts = append(facts, c.fact(f, w)...)
	}
	facts = append(facts, c.Extra...)
	var tail []string
	if c.Enter != "" {
		tail = append(tail, styleDim.Render(ui.Clip(symArrow+" enter - "+c.Enter, w)))
	}
	acts := c.actionLines(w)
	if len(acts) > 0 {
		tail = append(tail, line)
		tail = append(tail, acts...)
	}
	fixed := len(top) + len(tail)
	if len(facts) > 0 {
		fixed += 2 // пустые строки вокруг фактов
	}
	if fixed+len(path)+len(facts) > maxH {
		facts = clipRows(facts, maxH-fixed-len(path))
		if len(facts) == 0 {
			fixed -= 2
		}
		if fixed+len(path)+len(facts) > maxH {
			path = nil
		}
	}
	out := slices.Concat(top, path)
	if len(facts) > 0 {
		out = append(out, "")
		out = append(out, facts...)
		out = append(out, "")
	}
	return append(out, tail...)
}

// fact: подпись Teal 10+2, значение; длинное значение - на своих строках с отступом 12, не тильдой.
func (c Card) fact(f Fact, w int) []string {
	pad := strings.Repeat(" ", factLabelW)
	label := pad
	switch {
	case strings.Contains(f.Label, "\x1b"):
		label = ui.Fit(f.Label, factLabelW) // подпись уже с цветом: лампа беды
	case f.Label != "":
		label = ui.Fit(styleAccent.Render(f.Label), factLabelW)
	}
	room := w - factLabelW
	if ui.Width(f.Value) <= room {
		return []string{label + f.Value}
	}
	var out []string
	for i, part := range ui.Wrap(f.Value, room) {
		if i == 0 {
			out = append(out, label+part)
			continue
		}
		out = append(out, pad+part)
	}
	return out
}

// actionLines: капы по слотам в 15 колонок; у действия с последствием - два слота.
func (c Card) actionLines(w int) []string {
	var out []string
	line, used := "", 0
	for _, a := range c.Actions {
		cell := styleKeyCap.Render(" "+a.Key+" ") + " " + styleText.Render(a.Label)
		if a.Note != "" {
			cell += styleDim.Render(" " + symSep + " " + a.Note)
		}
		// Короткий кап - в слот 15 колонок, длинный - своей шириной и три пробела.
		width := cardActionW
		if ui.Width(cell)+1 > cardActionW {
			width = ui.Width(cell) + 3
		}
		if used+width > w+1 && used > 0 {
			out = append(out, strings.TrimRight(line, " "))
			line, used = "", 0
		}
		line += ui.Pad(cell, width)
		used += width
	}
	if line != "" {
		out = append(out, strings.TrimRight(line, " "))
	}
	return out
}

// KeyCap: клавиша на подложке - для строк карточки, которые собирают разделы.
func KeyCap(k string) string { return styleKeyCap.Render(" " + k + " ") }

// besideList: список слева и карточка справа строка к строке; карточка с колонки 60.
func besideList(left []string, card []string, width int) []string {
	return besideListW(left, card, cardListW, width)
}

// besideListW: то же со своей шириной списка - на широком окне у списка есть ещё колонки.
func besideListW(left []string, card []string, listW, width int) []string {
	n := max(len(left), len(card))
	out := make([]string, n)
	for i := range out {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(card) {
			r = card[i]
		}
		if r == "" {
			out[i] = l
			continue
		}
		out[i] = ui.Fit(l, listW) + strings.Repeat(" ", cardGap) + ui.Clip(r, width-listW-cardGap)
	}
	return out
}
