package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Хвосты удалённых программ: вкладки Заметные и Мелкие, отметки, карточка под списком, уборка с
// подтверждением и в фоне.

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
	Hidden   bool // мелкий: во вкладке Мелкие
	Items    []LeftItem
	Commands []string // что выполнится при уборке
}

// LeftOpts: источники экрана хвостов.
type LeftOpts struct {
	Load   func() []LeftGroup
	Remove func(g LeftGroup) error
	Open   func(path string) // показать в Finder
}

type leftovers struct {
	opts    LeftOpts
	groups  []LeftGroup
	list    List
	marked  map[string]bool
	hidden  bool // открыта вкладка Мелкие
	loading bool
	started time.Time
}

type leftMsg []LeftGroup

// NewLeftovers создаёт экран хвостов.
func NewLeftovers(opts LeftOpts) Screen {
	return &leftovers{opts: opts, marked: map[string]bool{}}
}

func (l *leftovers) Init() tea.Cmd {
	l.loading, l.started = true, time.Now()
	return func() tea.Msg { return leftMsg(l.opts.Load()) }
}

func (l *leftovers) Crumbs() []string {
	return []string{"Диск", "Хвосты удалённых программ"}
}

// counts: заметных и мелких.
func (l *leftovers) counts() (big, small int, smallSize int64) {
	for _, g := range l.groups {
		if g.Hidden {
			small++
			smallSize += g.Size
		} else {
			big++
		}
	}
	return big, small, smallSize
}

func (l *leftovers) Status() string {
	if l.loading {
		return spinFrames[0] + " ищу хвосты"
	}
	big, small, _ := l.counts()
	if big == 0 {
		return "крупных нет " + symSep + " " + itoa(small) + " мелких"
	}
	return itoa(big) + " заметных " + symSep + " " + itoa(small) + " мелких"
}

func (l *leftovers) Command() string { return "tim disk --leftovers" }

func (l *leftovers) Keys() []Key {
	if len(l.shown()) == 0 {
		return []Key{{"tab", "вкладка"}}
	}
	return []Key{{"enter", "Finder"}, {"space", "отметить"}, {"v", "все"}, {"x", "убрать"}, {"tab", "вкладка"}}
}

func (l *leftovers) shown() []LeftGroup {
	var out []LeftGroup
	for _, g := range l.groups {
		if g.Hidden == l.hidden {
			out = append(out, g)
		}
	}
	return out
}

func (l *leftovers) refill() {
	l.list.Rows = nil
	for _, g := range l.shown() {
		g := g
		l.list.Rows = append(l.list.Rows, Row{Text: g.Name, Render: func(sel bool, w int) string {
			mark := " "
			if l.marked[g.ID] {
				mark = styleBad.Render(symCheck)
			}
			name := styleText.Render(ui.Fit(g.Name, 24))
			if sel {
				name = styleBold.Render(ui.Fit(g.Name, 24))
			}
			note := styleDim.Render(g.Note)
			if !g.Sure && !g.Hidden {
				note = styleAmber.Render(g.Note)
			}
			return SelRow(sel, w, fmt.Sprintf("%s%9s  %s%s  %s", mark, ui.Bytes(g.Size), name, styleDim.Render(ui.Fit(g.Owner, 28)), note))
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
	case JobDone:
		return l.Init()
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
		case "v":
			all := true
			for _, g := range l.shown() {
				if !l.marked[g.ID] {
					all = false
				}
			}
			for _, g := range l.shown() {
				l.marked[g.ID] = !all
			}
		case "tab", "shift+tab":
			l.hidden = !l.hidden
			l.list.Cursor = 0
			l.refill()
		case "enter":
			if g := l.current(); g != nil && len(g.Items) > 0 && l.opts.Open != nil {
				l.opts.Open(g.Items[0].Path)
				return app.Say(level.OK, g.Name+": открыто в Finder")
			}
		case "x":
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
		return app.Say(level.Warn, "сначала отметь пробелом, что убрать")
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
			l.marked = map[string]bool{}
			what := "убираю хвосты: " + ui.Count(len(picked), "программа", "программы", "программ") + ", " + ui.Bytes(sum)
			return app.Job(what, func() (string, error) {
				var errs []string
				var freed int64
				for _, g := range picked {
					if err := l.opts.Remove(g); err != nil {
						errs = append(errs, g.Name+": "+err.Error())
						continue
					}
					freed += g.Size
				}
				if len(errs) > 0 {
					return "", ui.Errorf("%s", strings.Join(errs, "; "))
				}
				return "Корзина: +" + ui.Bytes(freed) + ", место освободится после её очистки", nil
			})
		},
	})
}

func (l *leftovers) View(width, height int) string {
	big, small, smallSize := l.counts()
	tabs := func() string {
		on, off := styleTabOn, styleTabOff
		if l.hidden {
			on, off = off, on
		}
		return "   " + on.Render(" Заметные "+itoa(big)+" ") + " " + off.Render(" Мелкие "+itoa(small)+" ")
	}
	if l.loading {
		out := Notice("Ищу, что осталось от удалённых программ", progressLine(-1, "смотрю Library, автозапуск и кэши", l.started),
			"находки не трогаются: убирать - только отмеченное и после подтверждения", width)
		return strings.Join(out, "\n")
	}
	if len(l.shown()) == 0 && !l.hidden {
		out := Notice(styleGood.Render(symLamp)+" Хвостов удалённых программ нет",
			styleDim.Render(itoa(small)+" мелких записей на "+ui.Bytes(smallSize)+": настройки и кэши, их не стоит трогать вслепую"),
			"мелкие лежат во вкладке Мелкие, уборка там такая же: отметить и убрать в Корзину", width)
		return strings.Join(append(out, "", tabs()), "\n")
	}
	out := []string{"", tabs(), "", "   " + styleHead.Render(fmt.Sprintf("%10s  %-24s%-28s  %s", "РАЗМЕР "+sortMark(true), "ПРОГРАММА", "ГДЕ", ""))}
	var marked []string
	n, sum := 0, int64(0)
	for _, g := range l.shown() {
		if l.marked[g.ID] {
			n++
			sum += g.Size
		}
	}
	if n > 0 {
		marked = []string{"", "   " + styleBad.Render("отмечено "+itoa(n)+" "+symSep+" "+ui.Bytes(sum))}
	}
	var card []string
	if g := l.current(); g != nil {
		c := &Card{Title: g.Name, Sub: g.Owner + " " + symSep + " " + g.Note, Enter: "показать в Finder"}
		for i, it := range g.Items {
			if i >= 5 {
				c.Extra = append(c.Extra, styleDim.Render("и ещё "+itoa(len(g.Items)-5)))
				break
			}
			line := fmt.Sprintf("%9s  %s", ui.Bytes(it.Size), it.Path)
			if it.Note != "" {
				line += "  " + styleAmber.Render(it.Note)
			}
			c.Extra = append(c.Extra, line)
		}
		card = append([]string{""}, indent(c.Lines(width-6, 10))...)
	}
	room := height - len(out) - len(marked) - len(card)
	out = append(out, l.list.View(width, max(3, room))...)
	return strings.Join(append(append(out, marked...), card...), "\n")
}
