package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Общие детали экранов: полоса, лампочка, панель в рамке, список с курсором.

var (
	styleDim    = lipgloss.NewStyle().Foreground(colDim)
	styleText   = lipgloss.NewStyle().Foreground(colText)
	styleBold   = lipgloss.NewStyle().Foreground(colText).Bold(true)
	styleAccent = lipgloss.NewStyle().Foreground(colAccent)
	styleAmber  = lipgloss.NewStyle().Foreground(colWarn)
	styleBad    = lipgloss.NewStyle().Foreground(colBad)
	styleGood   = lipgloss.NewStyle().Foreground(colOK)
	styleGhost  = lipgloss.NewStyle().Foreground(colGhost)
	styleSelBg  = lipgloss.NewStyle().Background(colRow)
	stylePaneT  = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleTabOn  = lipgloss.NewStyle().Background(colAccent).Foreground(colOnAcc).Bold(true)
	styleTabOff = lipgloss.NewStyle().Background(colBarBg).Foreground(colDim)
	styleHead   = lipgloss.NewStyle().Foreground(colDim)
)

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

// Lamp: значок состояния со словом - читается и без цвета.
func Lamp(kind, text string) string {
	switch kind {
	case "ok":
		return styleGood.Render(symLamp + " " + text)
	case "warn":
		return styleAmber.Render(symWarn + " " + text)
	case "bad":
		return styleBad.Render(symCross + " " + text)
	}
	return styleDim.Render(symOff + " " + text)
}

// Pad добивает строку с учётом цветовых кодов; длиннее - обрезает.
func Pad(s string, w int) string {
	n := lipgloss.Width(s)
	if n > w {
		return Clip(s, w)
	}
	return s + strings.Repeat(" ", w-n)
}

// Clip обрезает строку до ширины, последний символ - тильда.
func Clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	// Строки с кодами цвета режем грубо по рунам, они короткие.
	out := ""
	for _, c := range s {
		if lipgloss.Width(out+string(c)) >= w {
			break
		}
		out += string(c)
	}
	return out + "~"
}

// Pane: панель в рамке; заголовок в верхней кромке, sub - серым рядом. Строки обрезаются по ширине.
func Pane(title, sub string, rows []string, width int) []string {
	inner := width - 4
	head := styleDim.Render("\u256d\u2500 ") + stylePaneT.Render(title)
	if sub != "" {
		head += styleDim.Render(" \u2500 " + sub)
	}
	n := lipgloss.Width(head)
	head += styleDim.Render(" " + strings.Repeat("\u2500", max(0, width-n-2)) + "\u256e")
	out := []string{head}
	for _, r := range rows {
		if strings.HasPrefix(r, "\x00sep:") {
			lab := strings.TrimPrefix(r, "\x00sep:")
			out = append(out, styleDim.Render("\u251c\u2500 "+lab+" "+strings.Repeat("\u2500", max(0, width-len([]rune(lab))-5))+"\u2524"))
			continue
		}
		out = append(out, styleDim.Render("\u2502 ")+Pad(r, inner)+styleDim.Render(" \u2502"))
	}
	out = append(out, styleDim.Render("\u2570"+strings.Repeat("\u2500", width-2)+"\u256f"))
	return out
}

// Sep: перегородка внутри панели с подписью.
func Sep(label string) string { return "\x00sep:" + label }

// Tile: маленькая панель состояния для главного экрана.
func Tile(title string, rows []string, width int) []string {
	return Pane(title, "", rows, width)
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
		out[i] = Pad(l, leftW) + " " + Pad(r, width-leftW-1)
	}
	return out
}

// Human: байты по-человечески.
func Human(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

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

func (l *List) Up()   { l.Cursor = max(0, l.Cursor-1) }
func (l *List) Down() { l.Cursor = min(max(0, len(l.Rows)-1), l.Cursor+1) }
func (l *List) Home() { l.Cursor = 0 }
func (l *List) End()  { l.Cursor = max(0, len(l.Rows)-1) }
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
		return styleSelBg.Render(Pad(styleAccent.Render(" "+symBar)+" "+body, width))
	}
	return Pad("   "+body, width)
}
