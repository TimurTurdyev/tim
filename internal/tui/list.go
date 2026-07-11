package tui

import (
	"fmt"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Row: строка списка - уже сложенный текст и то, по чему искать.
type Row struct {
	Text   string // для фильтра и подсказок
	Render func(selected bool, width int) string
}

// List: список с курсором и прокруткой; без фильтра - он у экрана.
type List struct {
	Rows   []Row
	Cursor int
	top    int
}

func (l *List) Up() { l.Cursor = max(0, l.Cursor-1) }

func (l *List) Down() { l.Cursor = min(max(0, len(l.Rows)-1), l.Cursor+1) }

func (l *List) Home() { l.Cursor = 0 }

func (l *List) End() { l.Cursor = max(0, len(l.Rows)-1) }

func (l *List) Page(n int) {
	l.Cursor = max(0, min(max(0, len(l.Rows)-1), l.Cursor+n))
}

// Key обрабатывает клавиши движения; true - клавиша была про список.
func (l *List) Key(k string, pageSize int) bool {
	switch k {
	case "up", "k", "л", "ctrl+p":
		l.Up()
	case "down", "j", "о", "ctrl+n":
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

// View рисует видимое окно списка высотой h; selected помечается самим рендером строки.
func (l *List) View(width, h int) []string {
	if h <= 0 {
		return nil
	}
	if l.Cursor < l.top {
		l.top = l.Cursor
	}
	if l.Cursor >= l.top+h {
		l.top = l.Cursor - h + 1
	}
	var out []string
	for i := l.top; i < len(l.Rows) && i < l.top+h; i++ {
		out = append(out, l.Rows[i].Render(i == l.Cursor, width))
	}
	if rest := len(l.Rows) - (l.top + h); rest > 0 && len(out) == h {
		out[h-1] = styleDim.Render(fmt.Sprintf("   и ещё %d ниже", rest+1))
	}
	return out
}

// SelRow: типовая строка списка - полоса слева у выбранной и подсветка на всю ширину.
func SelRow(selected bool, width int, body string) string {
	if selected {
		return styleSelBg.Render(ui.Fit(styleAccent.Render(" "+symBar)+" "+body, width))
	}
	return ui.Fit("   "+body, width)
}
