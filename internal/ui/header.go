package ui

import (
	"strings"

	"github.com/TimurTurdyev/tim/internal/brand"
)

// Лок-ап: значок слева, справа три строки на рядах 1-3 значка. Один рисунок для сводки, главного
// экрана, --version и заметок - как шапка, которую узнают с одного взгляда.

// headerGap: от значка до текста.
const headerGap = 3

// sep: разделитель сегментов в строках лок-апа и карточек.
const sep = " \u00b7 "

// HeaderOpts: как рисовать лок-ап.
type HeaderOpts struct {
	Look   brand.Look
	Indent int  // колонок слева до значка: 1 в тексте, 2 на экране
	Width  int  // во что строки должны влезть; 0 - не резать
	Large  bool // крупный маскот вместо значка - только мастер первого запуска
	Sprite bool // рисовать значок; без него текст стоит там же, если Keep
	Keep   bool // место значка остаётся пустым (экран, наложение), иначе текст с края
}

// Header: строки лок-апа. lines - строки из сегментов; где ширины не хватает, падают хвостовые
// сегменты, тильды не бывает.
func Header(lines [][]string, o HeaderOpts) []string {
	art := []string(nil)
	if o.Sprite {
		if on, depth := Colors(); on {
			if o.Large {
				art = brand.MascotLarge(o.Look, depth)
			} else {
				art = brand.MascotCompact(o.Look, depth)
			}
		}
	}
	artW := brand.MascotCompactWidth
	if o.Large {
		artW = brand.MascotLargeWidth
	}
	if len(art) == 0 && !o.Keep {
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = TrimSegments(l, o.Width)
		}
		return out
	}
	pad := strings.Repeat(" ", o.Indent)
	textAt := o.Indent + artW + headerGap
	rows := max(len(art), len(lines)+1, 5)
	// Текст на рядах 1-3 значка; у крупного - посередине его высоты.
	first := 1
	if o.Large {
		first = (rows - len(lines)) / 2
	}
	out := make([]string, rows)
	for i := range out {
		left := strings.Repeat(" ", artW)
		if i < len(art) {
			left = art[i]
		}
		line := pad + left
		if j := i - first; j >= 0 && j < len(lines) {
			width := 0
			if o.Width > 0 {
				width = o.Width - textAt
			}
			line += strings.Repeat(" ", headerGap) + TrimSegments(lines[j], width)
		}
		out[i] = strings.TrimRight(line, " ")
	}
	return out
}

// TrimSegments: сегменты через точку; не влезает в width - хвостовые уходят, пока не влезет. Первый
// сегмент остаётся всегда и при нужде режется. width 0 - без ограничения.
func TrimSegments(segs []string, width int) string {
	for n := len(segs); n > 0; n-- {
		line := strings.Join(segs[:n], Dim(sep))
		if width <= 0 || Width(line) <= width {
			return line
		}
	}
	if len(segs) == 0 {
		return ""
	}
	return Clip(segs[0], width)
}
