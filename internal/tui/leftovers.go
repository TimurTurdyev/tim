package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Хвосты удалённых программ: список с галочками, панель с подробностями, уборка с подтверждением.

// LeftItem: файл или папка хвоста.
type LeftItem struct {
	Path, Note string
	Size       int64
}

// LeftGroup: всё, что осталось от одной программы.
type LeftGroup struct {
	ID       string // ключ для отметок
	Name     string // Whisky
	Owner    string // com.isaacmarovitz
	Note     string // "программа удалена", "похоже, проверь", "твой проект"
	Size     int64
	Sure     bool
	Hidden   bool // показывается только по h
	Items    []LeftItem
	Commands []string // что выполнится при уборке
}

// LeftOpts: источники экрана хвостов.
type LeftOpts struct {
	Load   func() []LeftGroup
	Remove func(g LeftGroup) error
}

type leftovers struct {
	opts    LeftOpts
	groups  []LeftGroup
	list    List
	marked  map[string]bool
	hidden  bool
	loading bool
}

type leftMsg []LeftGroup

// NewLeftovers создаёт экран хвостов.
func NewLeftovers(opts LeftOpts) Screen {
	return &leftovers{opts: opts, marked: map[string]bool{}}
}

func (l *leftovers) Init() tea.Cmd {
	l.loading = true
	return func() tea.Msg { return leftMsg(l.opts.Load()) }
}

func (l *leftovers) Crumbs() []string {
	return []string{"Диск", "Хвосты удалённых программ"}
}

func (l *leftovers) Status() string {
	if l.loading {
		return "ищу..."
	}
	var sum int64
	for _, g := range l.shown() {
		sum += g.Size
	}
	return "найдено " + ui.Bytes(sum)
}

func (l *leftovers) Command() string { return "tim disk --leftovers" }

func (l *leftovers) Keys() []Key {
	return []Key{{symUpDown, "выбор"}, {"space", "отметить"}, {"a", "все"}, {"x", "убрать"}, {"h", "скрытые"}, {"esc", "назад"}}
}

func (l *leftovers) shown() []LeftGroup {
	var out []LeftGroup
	for _, g := range l.groups {
		if g.Hidden && !l.hidden {
			continue
		}
		out = append(out, g)
	}
	return out
}

func (l *leftovers) refill() {
	l.list.Rows = nil
	for _, g := range l.shown() {
		g := g
		l.list.Rows = append(l.list.Rows, Row{Text: g.Name, Render: func(sel bool, w int) string {
			box := styleDim.Render("[ ]")
			if l.marked[g.ID] {
				box = styleBad.Render("[" + symCheck + "]")
			}
			name := ui.Fit(g.Name, 22)
			switch {
			case sel:
				name = styleBold.Render(name)
			case g.Hidden:
				name = styleDim.Render(name)
			default:
				name = styleText.Render(name)
			}
			note := styleDim.Render(g.Note)
			if !g.Sure && !g.Hidden {
				note = styleAmber.Render(g.Note)
			}
			return SelRow(sel, w, box+" "+styleText.Render(ui.Fit(ui.Bytes(g.Size), 9))+"  "+name+note)
		}})
	}
	if l.list.Cursor >= len(l.list.Rows) {
		l.list.Cursor = max(0, len(l.list.Rows)-1)
	}
}

func (l *leftovers) current() *LeftGroup {
	s := l.shown()
	if l.list.Cursor < len(s) {
		return &s[l.list.Cursor]
	}
	return nil
}

func (l *leftovers) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case leftMsg:
		l.groups = []LeftGroup(msg)
		l.loading = false
		// Бесспорное отмечено заранее, сомнительное и скрытое - нет.
		for _, g := range l.groups {
			if g.Sure && !g.Hidden {
				l.marked[g.ID] = true
			}
		}
		l.refill()
		return nil
	case tea.KeyPressMsg:
		k := msg.String()
		if l.list.Key(k, 10) {
			return nil
		}
		switch k {
		case "space", " ":
			if g := l.current(); g != nil {
				l.marked[g.ID] = !l.marked[g.ID]
				l.list.Down()
			}
		case "a", "ф":
			all := true
			for _, g := range l.shown() {
				if !l.marked[g.ID] {
					all = false
				}
			}
			for _, g := range l.shown() {
				l.marked[g.ID] = !all
			}
		case "h", "р":
			l.hidden = !l.hidden
			l.refill()
		case "x", "ч":
			return l.confirmRemove(app)
		}
	}
	return nil
}

func (l *leftovers) confirmRemove(app *App) tea.Cmd {
	var picked []LeftGroup
	var lines []string
	var sum int64
	for _, g := range l.shown() {
		if l.marked[g.ID] {
			picked = append(picked, g)
			lines = append(lines, ui.Fit(ui.Bytes(g.Size), 9)+"  "+g.Name+"  "+styleDim.Render(g.Owner))
			sum += g.Size
		}
	}
	if len(picked) == 0 {
		return app.Toast(level.Warn, "сначала отметь пробелом, что убрать")
	}
	var cmds []string
	for _, g := range picked {
		cmds = append(cmds, g.Commands...)
	}
	cmd := strings.Join(cmds, "; ")
	if len(cmd) > 160 {
		cmd = cmd[:157] + "..."
	}
	return app.Confirm(Dialog{
		Title:   "Убрать хвосты?",
		Lines:   lines,
		Command: cmd,
		Note:    "Файлы уходят в Корзину, агенты автозапуска выгружаются, процессы останавливаются.",
		OK:      "Убрать " + ui.Bytes(sum),
		Danger:  true,
		Yes: func() tea.Cmd {
			var errs []string
			var freed int64
			for _, g := range picked {
				if err := l.opts.Remove(g); err != nil {
					errs = append(errs, g.Name+": "+err.Error())
					continue
				}
				freed += g.Size
			}
			l.marked = map[string]bool{}
			toast := app.Toast(level.OK, "в Корзине "+ui.Bytes(freed)+", место освободится после её очистки")
			if len(errs) > 0 {
				toast = app.Toast(level.Bad, strings.Join(errs, "; "))
			}
			return tea.Batch(toast, l.Init())
		},
	})
}

func (l *leftovers) View(width, height int) string {
	var out []string
	out = append(out, "")
	if l.loading {
		out = append(out, "  "+styleEmpty.Render("смотрю, что осталось от удалённых программ..."))
		return strings.Join(out, "\n")
	}
	if len(l.groups) == 0 {
		out = append(out, "  "+styleDim.Render("хвостов удалённых программ не нашёл"))
		return strings.Join(out, "\n")
	}
	if len(l.shown()) == 0 {
		var sum int64
		for _, g := range l.groups {
			sum += g.Size
		}
		out = append(out, "  "+styleDim.Render("крупных хвостов нет; h - показать "+itoa(len(l.groups))+" мелких на "+ui.Bytes(sum)))
		return strings.Join(out, "\n")
	}
	var pane []string
	if g := l.current(); g != nil {
		var rows []string
		for i, it := range g.Items {
			if i >= 5 {
				rows = append(rows, styleDim.Render("ещё "+itoa(len(g.Items)-5)))
				break
			}
			line := styleDim.Render(ui.Fit(ui.Bytes(it.Size), 9)) + "  " + it.Path
			if it.Note != "" {
				line += "  " + styleAmber.Render(it.Note)
			}
			rows = append(rows, line)
		}
		pane = Pane(g.Name, g.Owner+" "+symDot+" "+g.Note, rows, width-4)
	}
	listH := max(3, height-len(out)-len(pane)-3)
	out = append(out, l.list.View(width, listH)...)
	for len(out) < height-len(pane)-2 {
		out = append(out, "")
	}
	n, sum := 0, int64(0)
	for _, g := range l.shown() {
		if l.marked[g.ID] {
			n++
			sum += g.Size
		}
	}
	if n > 0 {
		out = append(out, "  "+styleBad.Render("отмечено "+itoa(n)+" "+symDot+" "+ui.Bytes(sum))+styleDim.Render("   x - убрать"))
	} else {
		out = append(out, "")
	}
	for _, p := range pane {
		out = append(out, "  "+p)
	}
	return strings.Join(out, "\n")
}
