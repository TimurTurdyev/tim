package tui

import (
	"fmt"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Row: строка списка - уже сложенный текст и то, по чему искать.
type Row struct {
	Text   string // для фильтра и подсказок
	Header bool   // заголовок группы: курсор его пропускает
	Render func(selected bool, width int) string
}

// List: список с курсором и прокруткой; без фильтра - он у экрана.
type List struct {
	Rows   []Row
	Cursor int
	top    int // первая видимая строка
}

func (l *List) Up() { l.move(-1) }

func (l *List) Down() { l.move(1) }

func (l *List) Home() {
	l.Cursor = 0
	l.Skip(1)
}

func (l *List) End() {
	l.Cursor = max(0, len(l.Rows)-1)
	l.Skip(-1)
}

func (l *List) Page(n int) {
	l.Cursor = max(0, min(max(0, len(l.Rows)-1), l.Cursor+n))
	dir := 1
	if n < 0 {
		dir = -1
	}
	l.Skip(dir)
}

func (l *List) move(dir int) {
	next := l.Cursor + dir
	for next >= 0 && next < len(l.Rows) && l.Rows[next].Header {
		next += dir
	}
	if next >= 0 && next < len(l.Rows) {
		l.Cursor = next
	}
}

// Skip ставит курсор на ближайшую строку не-заголовок в сторону dir, у края - в обратную.
func (l *List) Skip(dir int) {
	for range 2 {
		for l.Cursor >= 0 && l.Cursor < len(l.Rows) && l.Rows[l.Cursor].Header {
			l.Cursor += dir
		}
		if l.Cursor >= 0 && l.Cursor < len(l.Rows) {
			return
		}
		l.Cursor = max(0, min(len(l.Rows)-1, l.Cursor))
		dir = -dir
	}
}

// Key обрабатывает клавиши движения; true - клавиша была про список.
func (l *List) Key(k string, pageSize int) bool {
	switch k {
	case "up", "k", "ctrl+p":
		l.Up()
	case "down", "j", "ctrl+n":
		l.Down()
	case "pgup":
		l.Page(-pageSize)
	case "pgdown":
		l.Page(pageSize)
	case "home", "g":
		l.Home()
	case "end", "G":
		l.End()
	default:
		return false
	}
	return true
}

// View рисует видимое окно списка высотой h: если что-то уехало, первая и последняя строки
// говорят, сколько выше и ниже. selected помечается самим рендером строки.
func (l *List) View(width, h int) []string {
	n := len(l.Rows)
	if h <= 0 || n == 0 {
		return nil
	}
	start, end := l.window(h)
	var out []string
	if start > 0 {
		out = append(out, styleDim.Render(fmt.Sprintf("   %s ещё %d выше", symUp, start)))
	}
	for i := start; i < end; i++ {
		out = append(out, l.Rows[i].Render(i == l.Cursor, width))
	}
	if end < n {
		out = append(out, styleDim.Render(fmt.Sprintf("   %s ещё %d ниже", symDown, n-end)))
	}
	return out
}

// window: какие строки видны, чтобы курсор был на экране, а счётчики выше и ниже влезли в h.
func (l *List) window(h int) (start, end int) {
	n := len(l.Rows)
	if n <= h {
		l.top = 0
		return 0, n
	}
	shown := func(s int) (int, int) {
		room := h
		if s > 0 {
			room--
		}
		e := min(n, s+room)
		if e < n {
			e = max(s+1, e-1)
		}
		return s, e
	}
	l.top = max(0, min(l.top, n-1))
	for range 4 {
		s, e := shown(l.top)
		switch {
		case l.Cursor < s:
			l.top = l.Cursor
		case l.Cursor >= e:
			l.top += l.Cursor - e + 1
		default:
			return s, e
		}
	}
	return shown(l.top)
}

// SelRow: типовая строка списка - полоса слева у выбранной и подсветка на всю ширину. На
// подсветке серого нет: вторые колонки выбранной строки пишутся основным цветом.
func SelRow(selected bool, width int, body string) string {
	if selected {
		return onShade(ui.Fit(styleAccent.Render(" "+symBar)+" "+body, width))
	}
	return ui.Fit("   "+body, width)
}
