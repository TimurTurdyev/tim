package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Обозреватель диска: папки по размеру, заход внутрь, отметки, уборка в Корзину.

// Entry: строка обозревателя.
type Entry struct {
	Name, Path, Hint string
	Size             int64
	Dir              bool // можно зайти внутрь
}

// BrowseOpts: что показывать и что делать. Kids может считать долго (du) - зовётся в фоне.
type BrowseOpts struct {
	Root   Entry
	Kids   func(path string) []Entry
	Open   func(path string)
	Remove func(list []Entry) (freed int64, errs []string)
	Total  int64 // занято на диске: от него проценты в заголовке
}

type level struct {
	entry   Entry
	kids    []Entry
	list    List
	loading bool
}

type kidsMsg struct {
	path string
	kids []Entry
}

type browse struct {
	opts   BrowseOpts
	stack  []*level
	marked map[string]Entry
}

// NewBrowse создаёт экран обозревателя.
func NewBrowse(o BrowseOpts) Screen {
	b := &browse{opts: o, marked: map[string]Entry{}}
	b.stack = []*level{{entry: o.Root, loading: true}}
	return b
}

func (b *browse) Init() tea.Cmd { return b.load(b.opts.Root.Path) }

func (b *browse) load(path string) tea.Cmd {
	return func() tea.Msg { return kidsMsg{path, b.opts.Kids(path)} }
}

func (b *browse) cur() *level { return b.stack[len(b.stack)-1] }

func (b *browse) Crumbs() []string {
	var c []string
	for _, l := range b.stack {
		c = append(c, l.entry.Name)
	}
	return c
}

func (b *browse) Status() string {
	l := b.cur()
	s := Human(l.entry.Size)
	if b.opts.Total > 0 {
		s += " " + symDot + " " + itoa(int(l.entry.Size*100/b.opts.Total)) + "% занятого"
	}
	return s
}

func (b *browse) Command() string {
	if len(b.stack) > 1 && b.cur().entry.Path != "" {
		return "tim disk -b " + b.cur().entry.Path
	}
	return "tim disk -b"
}

func (b *browse) Keys() []Key {
	return []Key{{symUpDown, "выбор"}, {symLR, "внутрь, назад"}, {"space", "отметить"}, {"x", "убрать"}, {"o", "Finder"}, {"esc", "назад"}}
}

// under: путь внутри уже отмеченной папки.
func (b *browse) under(path string) bool {
	for p := range b.marked {
		if strings.HasPrefix(path, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
	}
	return false
}

func (b *browse) fill(l *level) {
	l.list.Rows = nil
	for _, e := range l.kids {
		e := e
		l.list.Rows = append(l.list.Rows, Row{Text: e.Name, Render: func(sel bool, w int) string {
			pct := 0
			if l.entry.Size > 0 {
				pct = int(e.Size * 100 / l.entry.Size)
			}
			filled := max(0, min(16, int(e.Size*16/max(1, l.entry.Size))))
			bar := styleAccent.Render(strings.Repeat(symBlock, filled)) + styleGhost.Render(strings.Repeat(symShade, 16-filled))
			mark := "  "
			if _, ok := b.marked[e.Path]; ok && e.Path != "" {
				mark = styleBad.Render(symCheck) + " "
			} else if e.Path != "" && b.under(e.Path) {
				mark = styleDim.Render(symCheck) + " "
			}
			name := ui.Fit(e.Name, max(20, min(44, w-62)))
			if e.Dir {
				name = styleBold.Render(name)
			} else {
				name = styleDim.Render(name)
			}
			line := mark + styleText.Render(ui.Fit(Human(e.Size), 9)) + " " + bar + " " + styleDim.Render(ui.Fit(itoa(pct)+"%", 4)) + "  " + name
			if e.Hint != "" {
				line += "  " + styleDim.Render(e.Hint)
			}
			return SelRow(sel, w, line)
		}})
	}
}

func (b *browse) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case kidsMsg:
		for _, l := range b.stack {
			if l.entry.Path == msg.path {
				l.kids, l.loading = msg.kids, false
				b.fill(l)
				if l.list.Cursor >= len(l.kids) {
					l.list.Cursor = max(0, len(l.kids)-1)
				}
			}
		}
		return nil
	case tea.KeyPressMsg:
		l := b.cur()
		k := msg.String()
		if l.list.Key(k, 10) {
			return nil
		}
		switch k {
		case "enter", "right", "l", "д":
			if l.loading || len(l.kids) == 0 {
				return nil
			}
			e := l.kids[l.list.Cursor]
			if !e.Dir {
				return app.Toast("warn", "внутрь не зайти: это файлы или место без папок")
			}
			b.stack = append(b.stack, &level{entry: e, loading: true})
			return b.load(e.Path)
		case "left", "backspace", "р":
			if len(b.stack) > 1 {
				b.stack = b.stack[:len(b.stack)-1]
				return nil
			}
			return app.Pop()
		case "space", " ":
			if l.loading || len(l.kids) == 0 {
				return nil
			}
			e := l.kids[l.list.Cursor]
			switch {
			case e.Path == "":
				return app.Toast("warn", "это не папка - отметить нельзя")
			case b.under(e.Path):
				return app.Toast("warn", "уже внутри отмеченной папки - уйдёт вместе с ней")
			}
			if _, ok := b.marked[e.Path]; ok {
				delete(b.marked, e.Path)
			} else {
				b.marked[e.Path] = e
			}
			l.list.Down()
		case "o", "щ":
			if !l.loading && len(l.kids) > 0 && l.kids[l.list.Cursor].Path != "" && b.opts.Open != nil {
				b.opts.Open(l.kids[l.list.Cursor].Path)
				return app.Toast("ok", "открыто в Finder")
			}
		case "x", "ч":
			return b.confirmRemove(app)
		}
	}
	return nil
}

func (b *browse) confirmRemove(app *App) tea.Cmd {
	var all []Entry
	for _, e := range b.marked {
		all = append(all, e)
	}
	top := Outermost(all)
	if len(top) == 0 {
		return app.Toast("warn", "сначала отметь пробелом, что убрать")
	}
	var lines, paths []string
	var sum int64
	for _, e := range top {
		lines = append(lines, ui.Fit(Human(e.Size), 9)+"  "+e.Path)
		paths = append(paths, e.Path)
		sum += e.Size
	}
	return app.Confirm(Dialog{
		Title:   "Убрать в Корзину?",
		Lines:   lines,
		Command: "trash " + strings.Join(paths, " "),
		Note:    "Из Корзины можно вернуть. Место освободится после её очистки.",
		OK:      "Убрать " + Human(sum),
		Danger:  true,
		Yes: func() tea.Cmd {
			freed, errs := b.opts.Remove(top)
			b.marked = map[string]Entry{}
			cmds := []tea.Cmd{}
			if len(errs) > 0 {
				cmds = append(cmds, app.Toast("bad", strings.Join(errs, "; ")))
			} else {
				cmds = append(cmds, app.Toast("ok", "в Корзине "+Human(freed)+", место освободится после её очистки"))
			}
			// Размеры изменились - перечитать все уровни стопки.
			for _, l := range b.stack {
				l.loading = true
				cmds = append(cmds, b.load(l.entry.Path))
			}
			return tea.Batch(cmds...)
		},
	})
}

func (b *browse) View(width, height int) string {
	l := b.cur()
	var out []string
	out = append(out, "")
	if l.loading {
		out = append(out, "  "+styleEmpty.Render("считаю, что внутри..."))
	}
	rows := l.list.View(width, max(3, height-4))
	out = append(out, rows...)
	for len(out) < height-1 {
		out = append(out, "")
	}
	if len(b.marked) > 0 {
		var all []Entry
		for _, e := range b.marked {
			all = append(all, e)
		}
		top := Outermost(all)
		var sum int64
		var names []string
		for _, e := range top {
			sum += e.Size
			names = append(names, e.Name)
		}
		out[len(out)-1] = "  " + styleBad.Render("отмечено "+itoa(len(top))+" "+symDot+" "+Human(sum)) + styleDim.Render("   "+ui.Clip(strings.Join(names, ", "), width-30))
	}
	return strings.Join(out, "\n")
}

// Outermost: отмеченное без того, что лежит внутри другого отмеченного - иначе оно посчиталось бы
// дважды, а убрать его второй раз нельзя: оно уже уехало вместе с родителем.
func Outermost(list []Entry) []Entry {
	var out []Entry
	for _, e := range list {
		inside := false
		for _, p := range list {
			if p.Path != e.Path && strings.HasPrefix(e.Path, strings.TrimSuffix(p.Path, "/")+"/") {
				inside = true
				break
			}
		}
		if !inside {
			out = append(out, e)
		}
	}
	return out
}
