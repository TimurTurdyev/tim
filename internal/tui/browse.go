package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/level"
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

type folder struct {
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
	stack  []*folder
	marked marks[Entry]
}

// NewBrowse создаёт экран обозревателя.
func NewBrowse(o BrowseOpts) Screen {
	b := &browse{opts: o}
	b.stack = []*folder{{entry: o.Root, loading: true}}
	return b
}

func (b *browse) Init() tea.Cmd { return b.load(b.opts.Root.Path) }

func (b *browse) load(path string) tea.Cmd {
	return func() tea.Msg { return kidsMsg{path, b.opts.Kids(path)} }
}

func (b *browse) cur() *folder { return b.stack[len(b.stack)-1] }

func (b *browse) Crumbs() []string {
	var c []string
	for _, l := range b.stack {
		c = append(c, l.entry.Name)
	}
	return c
}

func (b *browse) Status() string {
	l := b.cur()
	if l.loading {
		return spinFrames[0] + " считаю, что внутри"
	}
	s := ui.Bytes(l.entry.Size)
	if b.opts.Total > 0 {
		s += " " + symSep + " " + itoa(int(l.entry.Size*100/b.opts.Total)) + "% занятого"
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
	return []Key{{"enter", "внутрь"}, {"space", "отметить"}, {"x", "убрать"}, {"o", "Finder"}, {symLeft, "вверх"}}
}

// under: путь внутри уже отмеченной папки.
func (b *browse) under(path string) bool {
	for _, p := range b.marked.keys() {
		if strings.HasPrefix(path, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
	}
	return false
}

func (b *browse) fill(l *folder) {
	l.list.Rows = nil
	for _, e := range l.kids {
		e := e
		l.list.Rows = append(l.list.Rows, Row{Text: e.Name, Render: func(sel bool, w int) string {
			pct := 0
			if l.entry.Size > 0 {
				pct = int(e.Size * 100 / l.entry.Size)
			}
			filled := max(0, min(15, int(e.Size*15/max(1, l.entry.Size))))
			bar := styleText.Render(strings.Repeat("\u2584", filled)) + styleGhost.Render(strings.Repeat("\u2582", 15-filled))
			mark := " "
			if e.Path != "" && b.marked.has(e.Path) {
				mark = styleBad.Render(symCheck)
			} else if e.Path != "" && b.under(e.Path) {
				mark = styleDim.Render(symCheck)
			}
			share := fmt.Sprintf("%3d%%", pct)
			if pct == 0 {
				share = "    "
			}
			name := styleText.Render(ui.Fit(e.Name, max(20, min(44, w-62))))
			switch {
			case sel:
				name = styleBold.Render(ui.Fit(e.Name, max(20, min(44, w-62))))
			case !e.Dir:
				name = styleDim.Render(ui.Fit(e.Name, max(20, min(44, w-62))))
			}
			line := fmt.Sprintf("%s%9s  %s %s  %s", mark, ui.Bytes(e.Size), bar, styleDim.Render(share), name)
			if e.Hint != "" {
				line += "  " + styleDim.Render(e.Hint)
			}
			return SelRow(sel, w, line)
		}})
	}
}

func (b *browse) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case JobDone:
		// Размеры изменились - перечитать все уровни стопки.
		var cmds []tea.Cmd
		for _, l := range b.stack {
			l.loading = true
			cmds = append(cmds, b.load(l.entry.Path))
		}
		return tea.Batch(cmds...)
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
		case "enter", "right":
			if l.loading || len(l.kids) == 0 {
				return nil
			}
			e := l.kids[l.list.Cursor]
			if !e.Dir {
				return app.Say(level.Warn, "внутрь не зайти: это файлы или место без папок")
			}
			b.stack = append(b.stack, &folder{entry: e, loading: true})
			return b.load(e.Path)
		case "left", "backspace":
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
				return app.Say(level.Warn, "это не папка - отметить нельзя")
			case b.under(e.Path):
				return app.Say(level.Warn, "уже внутри отмеченной папки - уйдёт вместе с ней")
			}
			b.marked.toggle(e.Path, e)
			l.list.Down()
		case "o":
			if !l.loading && len(l.kids) > 0 && l.kids[l.list.Cursor].Path != "" && b.opts.Open != nil {
				b.opts.Open(l.kids[l.list.Cursor].Path)
				return app.Say(level.OK, "открыто в Finder")
			}
		case "x":
			return b.confirmRemove(app)
		}
	}
	return nil
}

func (b *browse) confirmRemove(app *App) tea.Cmd {
	top := Outermost(b.marked.list())
	if len(top) == 0 {
		return app.Say(level.Warn, "сначала отметь пробелом, что убрать")
	}
	var lines, paths []string
	var sum int64
	for _, e := range top {
		lines = append(lines, ui.Fit(ui.Bytes(e.Size), 9)+"  "+e.Path)
		paths = append(paths, e.Path)
		sum += e.Size
	}
	return app.Confirm(Dialog{
		Title:   "Убрать в Корзину?",
		Lines:   lines,
		Command: "trash " + strings.Join(paths, " "),
		Note:    "Из Корзины можно вернуть. Место освободится после её очистки.",
		OK:      "Убрать " + ui.Bytes(sum),
		Danger:  true,
		Yes: func() tea.Cmd {
			b.marked.clear()
			what := "убираю в Корзину " + ui.Count(len(top), "место", "места", "мест") + ", " + ui.Bytes(sum)
			return app.Job(what, func() (string, error) {
				freed, errs := b.opts.Remove(top)
				if len(errs) > 0 {
					return "", ui.Errorf("%s", strings.Join(errs, "; "))
				}
				return "Корзина: +" + ui.Bytes(freed) + ", место освободится после её очистки", nil
			})
		},
	})
}

func (b *browse) View(width, height int) string {
	l := b.cur()
	out := []string{"", "   " + styleHead.Render(fmt.Sprintf("%10s  %-15s %4s  %s", "РАЗМЕР "+sortMark(true), "ДОЛЯ", "", "ПАПКА"))}
	if l.loading && len(l.kids) == 0 {
		return strings.Join(append(out, "   "+styleDim.Render("считаю, что внутри")), "\n")
	}
	var marked []string
	if b.marked.len() > 0 {
		top := Outermost(b.marked.list())
		var sum int64
		var names []string
		for _, e := range top {
			sum += e.Size
			names = append(names, e.Name)
		}
		marked = []string{"", "   " + styleBad.Render("отмечено "+itoa(len(top))+" "+symSep+" "+ui.Bytes(sum)) + "   " + styleDim.Render(ui.Clip(strings.Join(names, ", "), width-30))}
	}
	var card []string
	if c := b.card(); c != nil {
		card = append([]string{""}, indent(c.Lines(width-6, 8))...)
	}
	room := height - len(out) - len(marked) - len(card)
	out = append(out, l.list.View(width, max(3, room))...)
	return strings.Join(append(append(out, marked...), card...), "\n")
}

func indent(lines []string) []string {
	for i, l := range lines {
		lines[i] = "   " + l
	}
	return lines
}

// card: выбранная папка - путь, что внутри, что сделает enter.
func (b *browse) card() *Card {
	l := b.cur()
	if l.loading || l.list.Cursor >= len(l.kids) {
		return nil
	}
	e := l.kids[l.list.Cursor]
	c := &Card{Title: e.Name}
	if e.Path != "" {
		c.Sub = ui.Clip(e.Path, 60)
		c.Actions = []CardAction{{"o", "Finder", ""}}
	}
	if e.Hint != "" {
		c.Facts = append(c.Facts, Fact{Label: "заметка", Value: e.Hint})
	}
	if e.Dir {
		c.Enter = "внутрь, подпапки по размеру"
	} else {
		c.Enter = "внутрь не зайти: это файлы и мелкие папки"
	}
	return c
}

// Outermost: отмеченное без того, что лежит внутри другого отмеченного - иначе оно посчиталось бы
// дважды, а убрать его второй раз нельзя: оно уже уехало вместе с родителем.
func Outermost(list []Entry) []Entry {
	// "/" считается меньше любого символа: тогда вложенные идут сразу за своей папкой,
	// а не после соседей вроде "backups-old".
	key := func(e Entry) string { return strings.ReplaceAll(e.Path, "/", "\x00") }
	sorted := slices.Clone(list)
	slices.SortFunc(sorted, func(a, b Entry) int { return strings.Compare(key(a), key(b)) })
	inside := map[string]bool{}
	last := ""
	for _, e := range sorted {
		if last != "" && e.Path != last && strings.HasPrefix(e.Path, strings.TrimSuffix(last, "/")+"/") {
			inside[e.Path] = true
			continue
		}
		last = e.Path
	}
	var out []Entry
	for _, e := range list {
		if !inside[e.Path] {
			out = append(out, e)
		}
	}
	return out
}
